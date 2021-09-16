package config

import (
	"github.com/stretchr/testify/assert"
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
