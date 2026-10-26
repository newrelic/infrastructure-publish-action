// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/lock"
	"github.com/newrelic/infrastructure-publish-action/publisher/release"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
)

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
