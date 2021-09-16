package utils

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test_streamAsLog(t *testing.T) {
	type args struct {
		content string
		prefix  string
	}
	tests := []struct {
		name string
		args args
	}{
		{"empty", args{"", ""}},
		{"empty with prefix", args{"", "some-prefix"}},
		{"content", args{"foo", ""}},
		{"content with prefix", args{"foo", "a-prefix"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			l := log.New(&output, "", 0)

			wg := sync.WaitGroup{}
			wg.Add(1)
			StreamAsLog(&wg, l, reader(tt.args.content), tt.args.prefix)

			assert.Equal(t, expectedLog(tt.args.prefix, tt.args.content), output.String())
		})
	}
}

func Test_execLogOutput_streamExecOutputEnabled(t *testing.T) {
	streamExecOutput = true

	tests := []struct {
		name        string
		cmdName     string
		cmdArgs     []string
		expectedLog string
		wantErr     bool
	}{
		{"empty", "", []string{}, "", true},
		{"echo stdout", "echo", []string{"foo"}, "stdout: foo", false},
		// pipes are being escaped, but function is shared btw stdout and stderr, so testing stdout should be enough
		//{"echo stderr", "echo", []string{"bar", ">>", "/dev/stderr"}, "stderr: bar", false},
	}
	var err error
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			l := log.New(&output, "", 0)

			err = ExecLogOutput(l, tt.cmdName, time.Hour * 1, tt.cmdArgs...)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				gotLog := output.String()
				assert.True(t, strings.Contains(gotLog, tt.expectedLog), ">> Logged lines:\n%s\n>> Don't contain: %s", gotLog, tt.expectedLog)
			}
		})
	}
}


func expectedLog(prefix, content string) string {
	if content == "" {
		return content
	}

	if prefix != "" {
		prefix += ": "
	}
	return prefix + content + "\n"
}

func reader(content string) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(content)))
}
