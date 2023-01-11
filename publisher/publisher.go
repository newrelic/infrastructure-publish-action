// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/fastly"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
)

const (
	defaultLockRetries = 30

	// AWS lock resource tags
	defaultTagOwningTeam = "CAOS"
	defaultTagProduct    = "integrations"
	defaultTagProject    = "infrastructure-publish-action"
	defaultTagEnv        = "us-development"
)

var (
	defaultTags = fmt.Sprintf("department=product&product=%s&project=%s&owning_team=%s&environment=%s",
		defaultTagProduct,
		defaultTagProject,
		defaultTagOwningTeam,
		defaultTagEnv,
	)
)

var (
	l = log.New(log.Writer(), "", 0)
)

func main() {
	conf := config.LoadConfig()

	var bucketLock lock.BucketLock
	if conf.DisableLock {
		bucketLock = lock.NewNoop()
	} else {
		if conf.AwsRegion == "" {
			l.Fatal("missing 'aws_region' value")
		}
		if conf.AwsLockBucket == "" {
			l.Fatal("missing 'aws_s3_lock_bucket_name' value")
		}
		if conf.AwsRoleARN == "" {
			l.Fatal("missing 'aws_role_arn' value")
		}
		if conf.RunID == "" {
			l.Fatal("missing 'run_id' value")
		}

		if conf.AwsTags == "" {
			conf.AwsTags = defaultTags
		}

		if conf.UseDefLockRetries {
			conf.LockRetries = defaultLockRetries
		}
		cfg := lock.NewS3Config(
			conf.AwsLockBucket,
			conf.AwsRoleARN,
			conf.AwsRegion,
			conf.AwsTags,
			conf.LockGroup,
			conf.LockOwner(),
			conf.LockRetries,
			lock.DefaultRetryBackoff,
			lock.DefaultTTL,
		)
		var err error
		bucketLock, err = lock.NewS3(cfg, l.Printf)
		// fail fast when lacking required AWS credentials
		if err != nil {
			l.Fatal("cannot create lock on s3: " + err.Error())
		}
	}

	uploadSchemas, err := config.ParseUploadSchemasFile(conf.UploadSchemaFilePath)
	if err != nil {
		l.Fatal(err)
	}

	if conf.LocalPackagesPath == "" {
		d := download.NewDownloader(http.DefaultClient)
		err = d.DownloadArtifacts(conf, uploadSchemas)
		if err != nil {
			l.Fatal(err)
		}
		l.Println("🎉 download phase complete")
	} else {
		conf.ArtifactsSrcFolder = conf.LocalPackagesPath
	}

	err = UploadAndClean(conf, uploadSchemas, bucketLock)
	if err != nil {
		l.Fatal(err)
	}
}

// UploadAndClean uploads file to the bucket and cleans Fastly cache
// S3 bucket lock required
func UploadAndClean(conf config.Config, uploadSchemas config.UploadArtifactSchemas, bucketLock lock.BucketLock) (err error) {

	// lock the bucket
	if err = bucketLock.Lock(); err != nil {
		return
	}
	defer func() {
		errRelease := bucketLock.Release()
		if err == nil {
			err = errRelease
		} else if errRelease != nil {
			err = fmt.Errorf("got 2 errors: uploading: \"%v\", releasing lock: \"%v\"", err, errRelease)
		}
		return
	}()

	// upload assets to S3
	err = upload.UploadArtifacts(conf, uploadSchemas)
	if err != nil {
		return err
	}
	l.Println("🎉 upload phase complete")

	// clean fastly cache
	if conf.FastlyConfig.ApiKey != "" {
		err = fastly.PurgeCache(conf.FastlyConfig, l)

		if err != nil {
			l.Printf("❌ Fastly cache cleaning failed, retry manually.\n%s\n", err.Error())
		} else {
			l.Println("🧹 Fastly cache cleaned")
		}
	}

	return
}
