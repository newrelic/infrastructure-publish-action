// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command batch publishes several apps in one run: it reads a JSON manifest of apps, downloads
// every app's assets and publishes all of them with one lock, building each apt/yum/zypp
// repository once. The single-app publisher (../../publisher.go) is not touched by it.
package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/release"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
)

const (
	defaultLockRetries = 30

	// AWS lock resource tags, same as the single-app publisher
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
	l = log.New(log.Writer(), "", 0)
)

func main() {
	conf, opts, err := config.LoadBatch()
	if err != nil {
		l.Fatal("loading config: " + err.Error())
	}

	releaseMarker, err := newBatchMarker(conf)
	if err != nil {
		l.Fatal("creating release marker: " + err.Error())
	}

	bucketLock, err := newBucketLock(conf)
	if err != nil {
		l.Fatal(err)
	}

	result, runErr := runBatch(conf, opts, bucketLock, releaseMarker)
	if err = result.write(opts.ResultPath); err != nil {
		l.Printf("ERROR: cannot write batch result: %v", err)
	}
	if runErr != nil {
		l.Fatal(runErr)
	}
	l.Println("🎉 batch upload phase complete")
}

// runBatch prepares every app of the manifest and publishes the ones that are ready in one locked upload.
// An app that cannot be prepared (schema or download failure) is skipped and reported, it does not stop
// the others. Nothing is written to S3 before every download is done.
func runBatch(conf config.Config, opts config.BatchOptions, bucketLock lock.BucketLock, releaseMarker release.BatchMarker) (result batchResult, err error) {
	specs, err := config.ParseAppsManifest(opts.ManifestPath)
	if err != nil {
		return result, err
	}
	l.Printf("batch of %d apps", len(specs))

	jobs, skipped := prepareBatch(conf, opts, specs, newDownloader())
	result.Skipped = skipped
	for _, s := range skipped {
		l.Printf("⚠ skipping %s %s: %s", s.AppName, s.Tag, s.Reason)
	}

	if err = upload.UploadArtifactsBatch(jobs, bucketLock, releaseMarker); err != nil {
		result.Status = statusFailed
		result.Error = err.Error()
		return result, err
	}

	result.Status = statusPublished
	for _, job := range jobs {
		result.Published = append(result.Published, appRef{AppName: job.Conf.AppName, RepoName: job.Conf.RepoName, Tag: job.Conf.Tag})
	}
	return result, nil
}

func newBatchMarker(conf config.Config) (release.BatchMarker, error) {
	return release.NewBatchMarkerAWS(release.S3Config{
		Bucket:    conf.AwsBucket,
		RoleARN:   conf.AwsRoleARN,
		Region:    conf.AwsRegion,
		Directory: markerDirectory(conf.DestPrefix),
	}, l.Printf)
}

// markerDirectory is where releases.json lives: in the root of the repository the batch publishes to,
// i.e. the whole dest prefix. infrastructure_agent/ gives infrastructure_agent/releases.json, as for a
// single app, and a nested prefix such as testing/v2/infrastructure_agent_v2/ keeps it inside that prefix.
func markerDirectory(destPrefix string) string {
	return strings.Trim(destPrefix, "/")
}

// newBucketLock builds the lock the same way the single-app publisher does.
func newBucketLock(conf config.Config) (lock.BucketLock, error) {
	if conf.DisableLock {
		return lock.NewNoop(), nil
	}
	for name, value := range map[string]string{
		"aws_region":              conf.AwsRegion,
		"aws_s3_lock_bucket_name": conf.AwsLockBucket,
		"aws_role_arn":            conf.AwsRoleARN,
		"run_id":                  conf.RunID,
	} {
		if value == "" {
			return nil, fmt.Errorf("missing '%s' value", name)
		}
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
	bucketLock, err := lock.NewS3(cfg, l.Printf)
	// fail fast when lacking required AWS credentials
	if err != nil {
		return nil, fmt.Errorf("cannot create lock on s3: %w", err)
	}
	return bucketLock, nil
}

func newDownloader() downloader {
	return downloaderFor(http.DefaultClient)
}
