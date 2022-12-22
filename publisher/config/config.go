package config

import (
	"fmt"
	"github.com/spf13/viper"
	"strings"
	"time"
)

const (
	defaultAptlyFolder = "/root/.aptly"
	defaultLockgroup   = "lockgroup"

	//Access points
	accessPointStaging               = "http://nr-downloads-ohai-staging.s3-website-us-east-1.amazonaws.com"
	accessPointTesting               = "http://nr-downloads-ohai-testing.s3-website-us-east-1.amazonaws.com"
	accessPointProduction            = "https://download.newrelic.com"
	mirrorProduction                 = "https://nr-downloads-main.s3.amazonaws.com"
	placeholderAccessPointStaging    = "staging"
	placeholderAccessPointTesting    = "testing"
	placeholderAccessPointProduction = "production"
)

type Config struct {
	DestPrefix           string
	RepoName             string
	AppName              string
	Tag                  string
	MirrorHost           string
	AccessPointHost      string
	RunID                string
	Version              string
	ArtifactsDestFolder  string // s3 mounted folder
	ArtifactsSrcFolder   string
	AptlyFolder          string
	UploadSchemaFilePath string
	GpgPassphrase        string
	GpgKeyRing           string
	AwsRegion            string
	AwsRoleARN           string
	// locking properties (candidate for factoring)
	AwsLockBucket     string
	AwsTags           string
	LockGroup         string
	DisableLock       bool
	LockRetries       uint
	UseDefLockRetries bool
	LocalPackagesPath string
	AptSkipMirror     bool

	FastlyApiKey      string
	FastlyPurgeTag    string
	FastlyAwsBucket   string
	FastlyAwsRegion   string
	FastlyAwsAttempts int
	FastlyTimeoutS3   time.Duration
	FastlyTimeoutCDN  time.Duration
}

func (c *Config) LockOwner() string {
	return fmt.Sprintf("%s_%s_%s", c.AppName, c.Tag, c.RunID)
}

// parseAccessPointHost accessPointHost will be parsed to detect production, staging or testing placeholders
// and substitute them with their specific real values. Empty will fallback to production and any other value
// will be considered a different access point and will be return as it is
func parseAccessPointHost(accessPointHost string) (string, string) {
	switch accessPointHost {
	case "":
		return accessPointProduction, mirrorProduction
	case placeholderAccessPointProduction:
		return accessPointProduction, mirrorProduction
	case placeholderAccessPointStaging:
		return accessPointStaging, accessPointStaging
	case placeholderAccessPointTesting:
		return accessPointTesting, accessPointTesting
	default:
		return accessPointHost, accessPointHost
	}
}

func LoadConfig() Config {
	// TODO: make all the config required
	viper.BindEnv("repo_name")
	viper.BindEnv("app_name")
	viper.BindEnv("app_version")
	viper.BindEnv("tag")
	viper.BindEnv("access_point_host")
	viper.BindEnv("run_id")
	viper.BindEnv("artifacts_dest_folder")
	viper.BindEnv("artifacts_src_folder")
	viper.BindEnv("aptly_folder")
	viper.BindEnv("upload_schema_file_path")
	viper.BindEnv("dest_prefix")
	viper.BindEnv("gpg_passphrase")
	viper.BindEnv("gpg_key_ring")
	viper.BindEnv("aws_s3_bucket_name")
	viper.BindEnv("aws_s3_lock_bucket_name")
	viper.BindEnv("aws_role_arn")
	viper.BindEnv("aws_region")
	viper.BindEnv("disable_lock")
	viper.BindEnv("lock_retries")
	viper.BindEnv("lock_group")
	viper.BindEnv("local_packages_path")
	viper.BindEnv("apt_skip_mirror")

	viper.BindEnv("fastly_key")
	viper.BindEnv("fastly_purge_tag")
	viper.BindEnv("fastly_aws_bucket")
	viper.BindEnv("fastly_aws_region")
	viper.BindEnv("fastly_aws_attempts")
	viper.BindEnv("fastly_timeout_s3")
	viper.BindEnv("fastly_timeout_cdn")

	aptlyF := viper.GetString("aptly_folder")
	if aptlyF == "" {
		aptlyF = defaultAptlyFolder
	}

	lockGroup := viper.GetString("lock_group")
	if lockGroup == "" {
		lockGroup = defaultLockgroup
	}

	version := viper.GetString("app_version")
	if version == "" {
		version = strings.Replace(viper.GetString("tag"), "v", "", -1)
	}

	accessPointHost, mirrorHost := parseAccessPointHost(viper.GetString("access_point_host"))

	return Config{
		DestPrefix:           viper.GetString("dest_prefix"),
		RepoName:             viper.GetString("repo_name"),
		AppName:              viper.GetString("app_name"),
		Tag:                  viper.GetString("tag"),
		MirrorHost:           mirrorHost,
		AccessPointHost:      accessPointHost,
		RunID:                viper.GetString("run_id"),
		Version:              version,
		ArtifactsDestFolder:  viper.GetString("artifacts_dest_folder"),
		ArtifactsSrcFolder:   viper.GetString("artifacts_src_folder"),
		AptlyFolder:          aptlyF,
		UploadSchemaFilePath: viper.GetString("upload_schema_file_path"),
		GpgPassphrase:        viper.GetString("gpg_passphrase"),
		GpgKeyRing:           viper.GetString("gpg_key_ring"),
		LockGroup:            lockGroup,
		AwsLockBucket:        viper.GetString("aws_s3_lock_bucket_name"),
		AwsRoleARN:           viper.GetString("aws_role_arn"),
		AwsRegion:            viper.GetString("aws_region"),
		AwsTags:              viper.GetString("aws_tags"),
		DisableLock:          viper.GetBool("disable_lock"),
		LockRetries:          viper.GetUint("lock_retries"),
		LocalPackagesPath:    viper.GetString("local_packages_path"),
		UseDefLockRetries:    !viper.IsSet("lock_retries"),     // when non set: use default value
		AptSkipMirror:        viper.GetBool("apt_skip_mirror"), // when non set: use default value

		FastlyApiKey:      viper.GetString("fastly_api_key"),
		FastlyPurgeTag:    viper.GetString("fastly_purge_tag"),
		FastlyAwsBucket:   viper.GetString("fastly_aws_bucket"),
		FastlyAwsRegion:   viper.GetString("fastly_aws_region"),
		FastlyAwsAttempts: viper.GetInt("fastly_aws_attempts"),
		FastlyTimeoutS3:   viper.GetDuration("fastly_timeout_s3") * time.Second,
		FastlyTimeoutCDN:  viper.GetDuration("fastly_timeout_cdn") * time.Second,
	}
}
