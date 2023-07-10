// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"log"
	"net/http"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
)

var (
	l = log.New(log.Writer(), "", 0)
)

func main() {
	conf, err := config.LoadConfig()
	if err != nil {
		l.Fatal(err)
	}

	var bucketLock lock.BucketLock
	if conf.DisableLock {
		bucketLock = lock.NewNoop()
	} else {
		cfg := lock.NewS3Config(
			conf.AwsLockBucket,
			conf.AwsTags,
			conf.LockGroup,
			conf.LockOwner(),
			conf.LockRetries,
			lock.DefaultRetryBackoff,
			lock.DefaultTTL,
		)

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

	err = upload.UploadArtifacts(conf, uploadSchemas, bucketLock)
	if err != nil {
		l.Fatal(err)
	}
	l.Println("🎉 upload phase complete")
}
