package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
	"github.com/newrelic/infrastructure-publish-action/publisher/download"
	"github.com/newrelic/infrastructure-publish-action/publisher/upload"
)

// downloader downloads the assets of one app, as the single-app publisher does.
type downloader interface {
	DownloadArtifacts(conf config.Config, schema config.UploadArtifactSchemas) error
}

func downloaderFor(client download.HTTPClient) downloader {
	return download.NewDownloader(client)
}

// prepareBatch resolves the schema and downloads the assets of every app. An app that fails any of those
// steps is returned in skipped and left out of the jobs, so one missing asset does not block the batch.
func prepareBatch(conf config.Config, opts config.BatchOptions, specs []config.AppSpec, d downloader) (jobs []upload.AppUploadJob, skipped []skippedApp) {
	for _, spec := range specs {
		job, err := prepareApp(conf, opts, spec, d)
		if err != nil {
			skipped = append(skipped, skippedApp{appRef: appRef{AppName: spec.AppName, RepoName: spec.RepoName, Tag: spec.Tag}, Reason: err.Error()})
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, skipped
}

func prepareApp(conf config.Config, opts config.BatchOptions, spec config.AppSpec, d downloader) (upload.AppUploadJob, error) {
	appConf := spec.ToConfig(conf)

	schemaFile, err := config.ResolveSchemaFile(spec, opts.SchemasFolder, os.TempDir(), http.DefaultClient)
	if err != nil {
		return upload.AppUploadJob{}, err
	}
	uploadSchemas, err := config.ParseUploadSchemasFile(schemaFile)
	if err != nil {
		return upload.AppUploadJob{}, fmt.Errorf("schema: %w", err)
	}
	if err = config.ValidateSchemas(appConf.AppName, uploadSchemas); err != nil {
		return upload.AppUploadJob{}, err
	}

	if conf.LocalPackagesPath == "" {
		if err = d.DownloadArtifacts(appConf, uploadSchemas); err != nil {
			return upload.AppUploadJob{}, fmt.Errorf("download: %w", err)
		}
		l.Printf("🎉 download phase complete for %s %s", spec.AppName, spec.Tag)
	} else {
		appConf.ArtifactsSrcFolder = conf.LocalPackagesPath
	}

	return upload.AppUploadJob{Conf: appConf, Schemas: uploadSchemas}, nil
}
