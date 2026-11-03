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

func Test_loadConfig_lockBucketRegion(t *testing.T) {
	tests := []struct {
		name           string
		env            map[string]string
		wantRegion     string
		wantLockRegion string
	}{
		{
			name:           "defaults to the publish bucket region",
			env:            map[string]string{"AWS_REGION": "us-east-2"},
			wantRegion:     "us-east-2",
			wantLockRegion: "us-east-2",
		},
		{
			name:           "explicit empty lock region falls back to the publish bucket region",
			env:            map[string]string{"AWS_REGION": "us-east-2", "AWS_S3_LOCK_BUCKET_REGION": ""},
			wantRegion:     "us-east-2",
			wantLockRegion: "us-east-2",
		},
		{
			name:           "lock bucket in a different region",
			env:            map[string]string{"AWS_REGION": "us-east-2", "AWS_S3_LOCK_BUCKET_REGION": "us-east-1"},
			wantRegion:     "us-east-2",
			wantLockRegion: "us-east-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_NAME", "foo")
			t.Setenv("TAG", "vFooBar")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			config, err := LoadConfig()
			assert.NoError(t, err)
			assert.Equal(t, tt.wantRegion, config.AwsRegion)
			assert.Equal(t, tt.wantLockRegion, config.AwsLockBucketRegion)
		})
	}
}
