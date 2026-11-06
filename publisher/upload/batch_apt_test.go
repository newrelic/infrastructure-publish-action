package upload

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/newrelic/infrastructure-publish-action/publisher/config"
)

func Test_batchAptSrcRepo_followsTheDestPrefix(t *testing.T) {
	upload := config.Upload{Dest: "{dest_prefix}linux/apt/", SrcRepo: "{access_point_host}/infrastructure_agent/linux/apt"}

	tests := map[string]struct {
		mirrorHost string
		destPrefix string
		want       string
	}{
		"default prefix": {
			"http://mirror", "infrastructure_agent/", "http://mirror/infrastructure_agent/linux/apt",
		},
		"v2 prefix": {
			"http://mirror", "infrastructure_agent_v2/", "http://mirror/infrastructure_agent_v2/linux/apt",
		},
		"nested test prefix and trailing slash on the host": {
			"http://mirror/", "nested/prefix/infrastructure_agent_v2/",
			"http://mirror/nested/prefix/infrastructure_agent_v2/linux/apt",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conf := config.Config{MirrorHost: tt.mirrorHost, DestPrefix: tt.destPrefix}
			assert.Equal(t, tt.want, batchAptSrcRepo(conf, upload, "noble"))
		})
	}
}

func Test_groupApt_failsWhenAppsUseDifferentNestedPrefixes(t *testing.T) {
	jobs := ohiJobs(t, "nri-apache", "nri-mysql")
	jobs[0].Conf.DestPrefix = "testing/a/infrastructure_agent_v2/"
	jobs[1].Conf.DestPrefix = "testing/b/infrastructure_agent_v2/"

	_, err := groupApt(jobs)
	assert.Error(t, err)
}

func Test_aptDistExists(t *testing.T) {
	var status int
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(status)
	}))
	defer server.Close()

	tests := map[int]struct {
		exists  bool
		wantErr bool
	}{
		http.StatusOK:         {true, false},
		http.StatusNotFound:   {false, false},
		http.StatusForbidden:  {false, true}, // could be a private or misread repo: never treat as "empty"
		http.StatusBadGateway: {false, true},
	}
	for code, want := range tests {
		status = code
		exists, err := aptDistExists(server.URL+"/prefix/linux/apt", "noble")
		require.Equal(t, want.wantErr, err != nil, "status %d", code)
		assert.Equal(t, want.exists, exists, "status %d", code)
		assert.Equal(t, "/prefix/linux/apt/dists/noble/Release", gotPath)
	}
}

func Test_aptDistExists_networkErrorIsAnError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	exists, err := aptDistExists(url, "noble")
	assert.Error(t, err)
	assert.False(t, exists)
}
