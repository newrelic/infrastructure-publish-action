package config

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/newrelic/infrastructure-publish-action/publisher/utils"
	"github.com/spf13/viper"
	"go.uber.org/multierr"
)

const (
	defaultAptlyFolder = "/root/.aptly"
	defaultLockgroup   = "lockgroup"
	defaultLockRetries = 30

	//Access points
	accessPointStaging               = "http://nr-downloads-ohai-staging.s3-website-us-east-1.amazonaws.com"
	accessPointTesting               = "http://nr-downloads-ohai-testing.s3-website-us-east-1.amazonaws.com"
	accessPointProduction            = "https://download.newrelic.com"
	mirrorProduction                 = "https://nr-downloads-main.s3.amazonaws.com"
	placeholderAccessPointStaging    = "staging"
	placeholderAccessPointTesting    = "testing"
	placeholderAccessPointProduction = "production"

	// env vars
	repoNameFlag             = "repo_name"
	appNameFlag              = "app_name"
	appVersionFlag           = "app_version"
	tagFlag                  = "tag"
	accessPointHostFlag      = "access_point_host"
	runIDFlag                = "run_id"
	artifactsDestFolderFlag  = "artifacts_dest_folder"
	artifactsSrcFolderFlag   = "artifacts_src_folder"
	aptlyFolderFlag          = "aptly_folder"
	uploadSchemaFilePathFlag = "upload_schema_file_path"
	destPrefixFlag           = "dest_prefix"
	aptSkipMirrorFlag        = "apt_skip_mirror"
	awsTagsFlag              = "aws_tags"

	disableGpgSigningFlag = "disable_gpg_signing"
	gpgPassphraseFlag     = "gpg_passphrase"
	gpgKeyRingFlag        = "gpg_key_ring"

	awsS3BucketNameFlag     = "aws_s3_bucket_name"
	awsS3LockBucketNameFlag = "aws_s3_lock_bucket_name"
	disableLockFlag         = "disable_lock"
	lockRetriesFlag         = "lock_retries"
	lockGroupFlag           = "lock_group"
	localPackagesPathFlag   = "local_packages_path"

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
	LocalPackagesPath    string
	AptSkipMirror        bool

	// GPG Signing
	DisableGpgSigning bool
	GpgPassphrase     string
	GpgKeyRing        string

	// locking properties (candidate for factoring)
	DisableLock   bool
	AwsLockBucket string
	AwsTags       string
	LockGroup     string
	LockRetries   uint
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

func LoadConfig() (Config, error) {
	var errs error

	// TODO: make all the config required
	viper.BindEnv(repoNameFlag)
	viper.BindEnv(appNameFlag)
	viper.BindEnv(appVersionFlag)
	viper.BindEnv(tagFlag)
	viper.BindEnv(accessPointHostFlag)
	viper.BindEnv(runIDFlag)
	viper.BindEnv(artifactsDestFolderFlag)
	viper.BindEnv(artifactsSrcFolderFlag)
	viper.BindEnv(aptlyFolderFlag)
	viper.BindEnv(uploadSchemaFilePathFlag)
	viper.BindEnv(destPrefixFlag)
	viper.BindEnv(localPackagesPathFlag)
	viper.BindEnv(aptSkipMirrorFlag)

	viper.BindEnv(disableGpgSigningFlag)
	viper.BindEnv(gpgPassphraseFlag)
	viper.BindEnv(gpgKeyRingFlag)

	viper.BindEnv(disableLockFlag)
	viper.BindEnv(awsS3LockBucketNameFlag)
	viper.BindEnv(awsS3BucketNameFlag)
	viper.BindEnv(awsTagsFlag)
	viper.BindEnv(lockRetriesFlag)
	viper.BindEnv(lockGroupFlag)

	aptlyF := viper.GetString(aptlyFolderFlag)
	if aptlyF == "" {
		aptlyF = defaultAptlyFolder
	}

	lockGroup := viper.GetString(lockGroupFlag)
	if lockGroup == "" {
		lockGroup = defaultLockgroup
	}

	version := viper.GetString(appVersionFlag)
	if version == "" {
		version = strings.Replace(viper.GetString(tagFlag), "v", "", -1)
	}

	accessPointHost, mirrorHost := parseAccessPointHost(viper.GetString(accessPointHostFlag))

	awsTags := viper.GetString(awsTagsFlag)
	if awsTags == "" {
		awsTags = defaultTags
	}

	lockRetries := viper.GetUint(lockRetriesFlag)
	if !viper.IsSet(lockRetriesFlag) {
		lockRetries = defaultLockRetries
	}

	c := Config{
		DestPrefix:           viper.GetString(destPrefixFlag),
		RepoName:             viper.GetString(repoNameFlag),
		AppName:              viper.GetString(appNameFlag),
		Tag:                  viper.GetString(tagFlag),
		MirrorHost:           mirrorHost,
		AccessPointHost:      accessPointHost,
		RunID:                viper.GetString(runIDFlag),
		Version:              version,
		ArtifactsDestFolder:  viper.GetString(artifactsDestFolderFlag),
		ArtifactsSrcFolder:   viper.GetString(artifactsSrcFolderFlag),
		AptlyFolder:          aptlyF,
		UploadSchemaFilePath: viper.GetString(uploadSchemaFilePathFlag),

		DisableGpgSigning: viper.GetBool(disableGpgSigningFlag),
		GpgPassphrase:     viper.GetString(gpgPassphraseFlag),
		GpgKeyRing:        viper.GetString(gpgKeyRingFlag),

		DisableLock:       viper.GetBool(disableLockFlag),
		LockGroup:         lockGroup,
		AwsLockBucket:     viper.GetString(awsS3LockBucketNameFlag),
		AwsTags:           awsTags,
		LockRetries:       lockRetries,
		LocalPackagesPath: viper.GetString(localPackagesPathFlag),

		AptSkipMirror: viper.GetBool(aptSkipMirrorFlag),
	}

	if c.DisableLock {
		if !viper.IsSet(awsS3LockBucketNameFlag) {
			multierr.Append(errs, fmt.Errorf("missing 'aws_s3_lock_bucket_name' value"))
		}
		if !viper.IsSet(runIDFlag) {
			multierr.Append(errs, fmt.Errorf("missing 'run_id' value"))
		}
	}

	if c.DisableGpgSigning {
		if viper.IsSet(gpgPassphraseFlag) {
			multierr.Append(errs, fmt.Errorf("'gpg_passphrase' should not be set with GPG signing disabled"))
		}
		if viper.IsSet(gpgKeyRingFlag) {
			multierr.Append(errs, fmt.Errorf("'gpg_key_ring' should not be set with GPG signing disabled"))
		}
	} else {
		gpgKey, err := base64.RawStdEncoding.DecodeString(c.GpgPassphrase)
		if err != nil {
			multierr.Append(errs, err)
		}

		tmp := os.TempDir()
		defer os.RemoveAll(tmp)
		if err := os.WriteFile(tmp+"/gpg.key", gpgKey, fs.FileMode(0x700)); err != nil {
			multierr.Append(errs, err)
		}

		gpgArgs := []string{
			"--batch",
			"--import",
			"--no-default-keyring",
			"--keyring",
			c.GpgKeyRing,
			tmp + "/gpg.key",
		}
		if err := utils.ExecLogOutput(utils.Logger, "gpg", time.Minute, gpgArgs...); err != nil {
			multierr.Append(errs, err)
		}
	}

	if errs != nil {
		return Config{}, nil
	}
	return c, nil
}
