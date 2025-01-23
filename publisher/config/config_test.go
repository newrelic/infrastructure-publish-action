package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
func Test_requiredValues(t *testing.T) {
	_, err := LoadConfig()
	assert.ErrorIs(t, err, ErrMissingConfig)
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
				"APP_NAME": "foo",
				"TAG":      "vFooBar",
			},
			want: Config{
				AppName:           "foo",
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
				"APP_NAME":          "foo",
				"APP_VERSION":       "Baz",
				"APTLY_FOLDER":      "FooFolder",
				"LOCK_GROUP":        "FooGroup",
				"ACCESS_POINT_HOST": "FooAPH",
				"LOCK_RETRIES":      "false",
			},
			want: Config{
				AppName:           "foo",
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
			config, err := LoadConfig()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, config, "Case failed:", tt.name, tt.env)
		})
	}
}
