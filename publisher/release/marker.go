package release

import (
	"encoding/json"
	"time"
)

type ReleaseInfo struct {
	AppName   string `json:"app_name"`
	Tag       string `json:"tag"`
	RunID     string `json:"run_id"`
	RepoName  string `json:"repo_name"`
	Schema    string `json:"schema"`
	SchemaURL string `json:"schema_url"`
}

// Mark represents a release mark. It will contain the name of the release (appName, tag...)
// and the start and end of a release
// When the release has been started, the end will be zero
type Mark struct {
	ReleaseInfo
	Start CustomTime `json:"start"`
	End   CustomTime `json:"end"`
}

// Marker abstracts the persistence of the start and end of a release
type Marker interface {
	Start(releaseInfo ReleaseInfo) (Mark, error)
	End(mark Mark) error
}

// CustomTime is a wrapper around time.Time that
// allows to marshal and unmarshal time.Time in a custom format
type CustomTime struct {
	time.Time
}

const ctLayout = time.RFC3339

func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse(ctLayout, s)
	if err != nil {
		return err
	}
	ct.Time = t
	return nil
}

func (ct *CustomTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ct.Time.Format(ctLayout))
}

func (ct *CustomTime) Equals(t CustomTime) bool {
	return ct.Truncate(time.Second).Equal(t.Truncate(time.Second))
}
