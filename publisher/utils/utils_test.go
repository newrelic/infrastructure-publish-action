package utils

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
			streamAsLog(&wg, l, reader(tt.args.content), tt.args.prefix)

			assert.Equal(t, expectedLog(tt.args.prefix, tt.args.content), output.String())
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

func Test_ExecWithRetries_Ok(t *testing.T) {
	var output, outputRetry bytes.Buffer
	l := log.New(&output, "", 0)
	lRetry := log.New(&outputRetry, "", 0)

	err := ExecLogOutput(l, "ls", time.Millisecond*50, "/")
	assert.Nil(t, err)
	retryCallback := func(l *log.Logger, commandTimeout time.Duration) {
		l.Print("remounting")
	}
	err = ExecWithRetries(3, retryCallback, lRetry, "ls", time.Millisecond*50, "/")
	assert.Nil(t, err)

	assert.Equal(t, output.String(), outputRetry.String())
}

func Test_ExecWithRetries_Fail(t *testing.T) {
	var output, outputRetry bytes.Buffer
	l := log.New(&output, "", 0)
	lRetry := log.New(&outputRetry, "", 0)
	retries := 3

	err := ExecLogOutput(l, "ls", time.Millisecond*50, "/non_existing_path")
	assert.Error(t, err, "exit status 1")

	retryCallback := func(l *log.Logger, commandTimeout time.Duration) {
		l.Print("remounting")
	}
	err = ExecWithRetries(retries, retryCallback, lRetry, "ls", time.Millisecond*50, "/non_existing_path")
	assert.Error(t, err, "exit status 1")

	var expectedOutput string
	for i := 0; i < retries; i++ {
		expectedOutput += output.String()
		expectedOutput += "remounting\n"
		expectedOutput += fmt.Sprintf("[attempt %v] error executing command ls /non_existing_path\n", i)
	}
	assert.Equal(t, expectedOutput, outputRetry.String())
}

func Test_JoinPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"empty", []string{}, ""},
		{"first ends with slash", []string{"/a/b/c/", "a.txt"}, "/a/b/c/a.txt"},
		{"first and last ends with slash", []string{"/a/b/c/", "/d/"}, "/a/b/c/d/"},
		{"last ends with slash", []string{"/a/b/c", "d/"}, "/a/b/c/d/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, JoinPaths(tt.input...), tt.expected)
		})
	}
}
