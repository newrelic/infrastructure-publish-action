package lock

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMode_Valid(t *testing.T) {
	tests := []struct {
		name string
		m    Mode
		want bool
	}{
		{"default", "", true},
		{"invalid", "wtf", false},
		{"retry on busy", "retry_on_busy", true},
		{"fail on busy", "fail_on_busy", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.m.IsValid())
		})
	}
}
