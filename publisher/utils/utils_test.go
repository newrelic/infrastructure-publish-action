package utils

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
	"log"
	"sync"
	"testing"
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
