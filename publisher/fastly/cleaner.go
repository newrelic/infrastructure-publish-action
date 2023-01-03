package fastly

import (
	"context"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/fastly/go-fastly/v7/fastly"
)

// Usage:
// go run fastly-purge.go -v
//
// Similar shell counterpart:
// for i in {1..5}; do
//	echo \$i;
//	aws s3api head-object --bucket nr-downloads-main --key infrastructure_agent/linux/yum/el/7/x86_64/repodata/primary.sqlite.bz2
//		|/bin/grep ReplicationStatus
//		|/bin/grep COMPLETED
//		&& /usr/bin/curl -i -X POST -H \"Fastly-Key:\${FASTLY_KEY}\" https://api.fastly.com/service/2RMeBJ1ZTGnNJYvrWMgQhk/purge_all
//		&& break ;
//	/bin/sleep 60s;
//	if [ \$i -ge 5 ]; then
//		/usr/bin/curl -i -X POST -H \"Fastly-Key:\${FASTLY_KEY}\" https://api.fastly.com/service/2RMeBJ1ZTGnNJYvrWMgQhk/purge_all;
//	fi;
// done

type result struct {
	output s3.GetObjectOutput
	err    error
}

const (
	// https://developer.fastly.com/reference/api/purging/
	infraServiceID             = "2RMeBJ1ZTGnNJYvrWMgQhk"
	replicationStatusCompleted = "COMPLETED" // in s3.ReplicationStatusComplete is set to COMPLETE, which is wrong
	aptDistributionsPath       = "infrastructure_agent/linux/apt/dists/"
	aptDistributionPackageFile = "main/binary-amd64/Packages.bz2"
	rhDistributionsPath        = "infrastructure_agent/linux/yum/"
	zypperDistributionsPath    = "infrastructure_agent/linux/zypp/"
)

type Config struct {
	FastlyApiKey      string
	FastlyPurgeTag    string
	FastlyAwsBucket   string
	FastlyAwsRegion   string
	FastlyAwsAttempts int
	FastlyTimeoutS3   time.Duration
	FastlyTimeoutCDN  time.Duration
}

func PurgeCache(c Config, logger *log.Logger) error {
	logger.Println("Fastly: check for replica status...")
	ctx := context.Background()

	sess := session.Must(session.NewSession())
	cl := s3.New(sess, aws.NewConfig().WithRegion(c.FastlyAwsRegion))

	keys, err := getDefaultKeys(cl, c.FastlyAwsBucket)
	if err != nil {
		return fmt.Errorf("cannot get default keys, error: %v", err)
	}

	for _, key := range keys {
		if key != "" {
			if err := waitForKeyReplication(ctx, c.FastlyAwsBucket, key, cl, c.FastlyTimeoutS3, c.FastlyAwsAttempts); err != nil {
				return fmt.Errorf("unsucessful replication, error: %v", err)
			}
		}
	}

	logger.Println("Fastly: replica is ✅")
	logger.Println("Fastly: purging cache...")
	if err := purgeCDN(ctx, c.FastlyApiKey, c.FastlyPurgeTag, c.FastlyTimeoutCDN); err != nil {
		return fmt.Errorf("cannot purge CDN, error: %v", err)
	}

	logger.Println("Fastly: cache purged ✅")
	return nil
}

// waitForKeyReplication returns nil if key was successfully replicated or is not set for replication
func waitForKeyReplication(ctx context.Context, bucket, key string, cl *s3.S3, timeoutS3 time.Duration, attempts int) error {
	triesLeft := attempts

	inputGetObj := s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}

	replicated := false
	for {
		if replicated || triesLeft <= 0 {
			break
		}
		triesLeft--

		ctxT := ctx
		var cancelFn func()
		if timeoutS3 > 0 {
			ctxT, cancelFn = context.WithTimeout(ctx, timeoutS3)
		}
		if cancelFn != nil {
			defer cancelFn()
		}

		resC := make(chan result)
		go func(*s3.S3) {
			o, err := cl.GetObjectWithContext(ctxT, &inputGetObj)
			if err != nil {
				resC <- result{err: err}
			}
			resC <- result{output: *o}
		}(cl)

		select {
		case <-ctx.Done():
			return fmt.Errorf("execution terminated, msg: %v", ctx.Err())

		case res := <-resC:
			if res.err != nil {
				return fmt.Errorf("cannot get s3 object, key: %s, error: %v", key, res.err)
			}

			// TODO logDebug("key: %s, attempt: %d, object: %+v", key, attempts-triesLeft, res.output)
			// https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication-status.html
			// aws s3api head-object --bucket foo --key "bar/..." |grep ReplicationStatus
			if res.output.ReplicationStatus == nil || *res.output.ReplicationStatus == replicationStatusCompleted {
				replicated = true
			}
		}
	}

	if triesLeft <= 0 {
		return fmt.Errorf("maximum attempts for key: %v", key)
	}

	return nil
}

func purgeCDN(ctx context.Context, fastlyKey, purgeTag string, timeoutCDN time.Duration) error {
	client, err := fastly.NewClient(fastlyKey)
	if err != nil {
		return err
	}

	if timeoutCDN > 0 {
		client.HTTPClient.Timeout = timeoutCDN
	}

	var result *fastly.Purge

	if purgeTag == "" || purgeTag == "purge_all" {
		result, err = client.PurgeAll(&fastly.PurgeAllInput{ServiceID: infraServiceID})
	} else {
		result, err = client.PurgeKey(&fastly.PurgeKeyInput{ServiceID: infraServiceID, Key: purgeTag})
	}

	if err != nil || result.Status != "ok" {
		return fmt.Errorf("unexpected Fastly purge error: %w status: %s", err, result.Status)
	}

	return nil
}

func getDefaultKeys(cl *s3.S3, bucket string) ([]string, error) {
	aptKeys, err := aptDistributionsPackageFilesKeys(cl, bucket)
	if err != nil {
		return nil, err
	}

	rhKeys, err := rpmDistributionsMetadataFilesKeys(cl, bucket, rhDistributionsPath)
	if err != nil {
		return nil, err
	}

	zypperKeys, err := rpmDistributionsMetadataFilesKeys(cl, bucket, zypperDistributionsPath)
	if err != nil {
		return nil, err
	}

	return append(aptKeys, append(rhKeys, zypperKeys...)...), nil
}

func listFoldersInPath(cl *s3.S3, bucket, s3path string) ([]string, error) {
	input := &s3.ListObjectsV2Input{
		Bucket:    &bucket,
		Prefix:    aws.String(s3path),
		Delimiter: aws.String("/"),
	}

	out, err := cl.ListObjectsV2(input)
	if err != nil {
		return []string{}, err
	}

	var res []string
	for _, content := range out.CommonPrefixes {
		res = append(res, *content.Prefix)
	}

	return res, nil
}

func aptDistributionsPackageFilesKeys(cl *s3.S3, bucket string) ([]string, error) {
	aptDistrosPaths, err := listFoldersInPath(cl, aptDistributionsPath, bucket)
	if err != nil {
		return nil, err
	}

	var res []string
	for _, aptDistroPath := range aptDistrosPaths {
		res = append(res, path.Join(aptDistroPath, aptDistributionPackageFile))
	}

	return res, nil
}
func rpmDistributionsMetadataFilesKeys(cl *s3.S3, bucket, distributionPath string) ([]string, error) {
	rpmDistrosPaths, err := listFoldersInPath(cl, bucket, distributionPath)
	if err != nil {
		return nil, err
	}

	var res []string
	for _, rpmDistroPath := range rpmDistrosPaths {
		rpmDistrosVersions, err := listFoldersInPath(cl, bucket, rpmDistroPath)
		if err != nil {
			return nil, err
		}

		for _, rpmDistroVersion := range rpmDistrosVersions {
			rpmDistrosArchs, err := listFoldersInPath(cl, bucket, rpmDistroVersion)
			if err != nil {
				return nil, err
			}

			for _, rpmDistrosArch := range rpmDistrosArchs {
				res = append(res, fmt.Sprintf("%srepodata/repomd.xml", rpmDistrosArch))
			}
		}
	}

	return res, nil
}
