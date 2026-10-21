// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/release"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
	"log"
	"net/http"
	"os"
	"strings"
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
	conf, err := config.LoadConfig()
	if err != nil {
		l.Fatal("loading config: " + err.Error())
	}

	batch := conf.AppsManifestPath != ""
	if batch {
		// identifies the batch in the lock owner (app name, tag and run id)
		conf.AppName = "batch"
		conf.Tag = "manifest"
	}

	releaseMarker, err := newReleaseMarker(conf)
	if err != nil {
		l.Fatal("creating release marker: " + err.Error())
	}

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

	if batch {
		if err = runBatch(conf, bucketLock, releaseMarker); err != nil {
			l.Fatal(err)
		}
		l.Println("🎉 batch upload phase complete")
		return
	}

	uploadSchemas, err := config.ParseUploadSchemasFile(conf.UploadSchemaFilePath)
	if err != nil {
		l.Fatal(err)
	}
	// validate schemas
	if err = config.ValidateSchemas(conf.AppName, uploadSchemas); err != nil {
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

	err = upload.UploadArtifacts(conf, uploadSchemas, bucketLock, releaseMarker)
	if err != nil {
		l.Fatal(err)
	}
	l.Println("🎉 upload phase complete")
}

// runBatch downloads every app in the manifest the same way a single-app run does,
// then publishes all of them in one locked upload.
func runBatch(conf config.Config, bucketLock lock.BucketLock, releaseMarker release.Marker) error {
	specs, err := config.ParseAppsManifest(conf.AppsManifestPath)
	if err != nil {
		return err
	}
	l.Printf("batch of %d apps", len(specs))

	d := download.NewDownloader(http.DefaultClient)
	var jobs []upload.AppUploadJob
	for _, spec := range specs {
		appConf := spec.ToConfig(conf)

		schemaFile, err := config.ResolveSchemaFile(spec, conf.SchemasFolder, os.TempDir(), http.DefaultClient)
		if err != nil {
			return err
		}
		uploadSchemas, err := config.ParseUploadSchemasFile(schemaFile)
		if err != nil {
			return fmt.Errorf("%s: %w", spec.AppName, err)
		}
		if err = config.ValidateSchemas(appConf.AppName, uploadSchemas); err != nil {
			return err
		}

		if conf.LocalPackagesPath == "" {
			if err = d.DownloadArtifacts(appConf, uploadSchemas); err != nil {
				return fmt.Errorf("%s: %w", spec.AppName, err)
			}
			l.Printf("🎉 download phase complete for %s %s", spec.AppName, spec.Tag)
		} else {
			appConf.ArtifactsSrcFolder = conf.LocalPackagesPath
		}

		jobs = append(jobs, upload.AppUploadJob{Conf: appConf, Schemas: uploadSchemas})
	}

	return upload.UploadArtifactsBatch(jobs, bucketLock, releaseMarker)
}

func newReleaseMarker(conf config.Config) (release.Marker, error) {
	// We'll leave the release marker file in the root of the repository
	// i.e.
	// repo = /infrastructure_agent/linux/apt/
	// release marker = /infrastructure_agent/releases.json
	repoRootDir := strings.Split(strings.TrimPrefix(conf.DestPrefix, "/"), "/")[0]
	markerS3Conf := release.S3Config{
		Bucket:    conf.AwsBucket,
		RoleARN:   conf.AwsRoleARN,
		Region:    conf.AwsRegion,
		Directory: repoRootDir,
	}

	return release.NewMarkerAWS(markerS3Conf, l.Printf)
}
