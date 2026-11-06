package main

import (
	"encoding/json"
	"os"
)

const (
	statusPublished = "published"
	statusFailed    = "failed"
)

type appRef struct {
	AppName  string `json:"app_name"`
	RepoName string `json:"repo_name"`
	Tag      string `json:"tag"`
}

type skippedApp struct {
	appRef
	Reason string `json:"reason"`
}

// batchResult tells the workflow what happened to every app of the manifest: published apps are tested
// and notified, skipped ones stay pending for the next run.
type batchResult struct {
	Status    string       `json:"status"`
	Error     string       `json:"error,omitempty"`
	Published []appRef     `json:"published"`
	Skipped   []skippedApp `json:"skipped"`
}

func (r batchResult) write(filePath string) error {
	if r.Published == nil {
		r.Published = []appRef{}
	}
	if r.Skipped == nil {
		r.Skipped = []skippedApp{}
	}
	if r.Status == "" {
		r.Status = statusFailed
	}
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, append(content, '\n'), 0644)
}
