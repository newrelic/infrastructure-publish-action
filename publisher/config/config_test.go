package config

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func Test_parseAccessPointHost(t *testing.T) {
	tests := []struct {
		name              string
		accessPointHost   string
		expectedUrl       string
		expectedMirrorUrl string
	}{
		{"empty value fallback to prod", "", accessPointProduction, mirrorProduction},
		{"production placeholder", "production", accessPointProduction, mirrorProduction},
		{"staging placeholder", "staging", accessPointStaging, accessPointStaging},
		{"testing placeholder", "testing", accessPointTesting, accessPointTesting},
		{"fixed url", "https://www.some-bucket-url.com", "https://www.some-bucket-url.com", "https://www.some-bucket-url.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcRepo, mirrorHost := parseAccessPointHost(tt.accessPointHost)
			assert.Equal(t, tt.expectedUrl, srcRepo)
			assert.Equal(t, tt.expectedMirrorUrl, mirrorHost)
		})
	}
}

func Test_loadConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "defaults are applied",
			env: map[string]string{
				"TAG": "vFooBar",
			},
			want: Config{
				Tag:               "vFooBar",
				Version:           "FooBar",
				AccessPointHost:   accessPointProduction,
				MirrorHost:        mirrorProduction,
				AptlyFolder:       defaultAptlyFolder,
				LockGroup:         defaultLockgroup,
				UseDefLockRetries: true,
			},
		},
		{
			name: "custom values",
			env: map[string]string{
				"TAG":               "vFooBar",
				"APP_VERSION":       "Baz",
				"APTLY_FOLDER":      "FooFolder",
				"LOCK_GROUP":        "FooGroup",
				"ACCESS_POINT_HOST": "FooAPH",
				"LOCK_RETRIES":      "false",
			},
			want: Config{
				Tag:               "vFooBar",
				Version:           "Baz",
				AccessPointHost:   "FooAPH",
				MirrorHost:        "FooAPH",
				AptlyFolder:       "FooFolder",
				LockGroup:         "FooGroup",
				UseDefLockRetries: false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				os.Setenv(k, v)
			}
			assert.Equal(t, tt.want, LoadConfig(), "Case failed:", tt.name, tt.env)
		})
	}
}
