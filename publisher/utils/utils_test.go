package utils

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"io"
	"io/ioutil"
	"log"
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

// A simple mock service to assert on retry functionality
type Service struct {
	mock.Mock
}

func (s *Service) Do() error {
	args := s.Called()
	return args.Error(0)
}

func Test_RetrySuccessWithErrors(t *testing.T) {
	service := &Service{}
	var err = errors.New("some error")
	service.On("Do", mock.Anything).Times(2).Return(err)
	service.On("Do", mock.Anything).Times(1).Return(nil)

	anotherService := &Service{}
	anotherService.On("Do", mock.Anything).Times(2).Return(nil)

	// We will execute service 3 times. First + 2 retries
	err = Retry(service.Do, 5, time.Millisecond, func() { _ = anotherService.Do() })

	require.NoError(t, err)
	mock.AssertExpectationsForObjects(t, service, anotherService)
}

func Test_RetryNoError(t *testing.T) {
	service := &Service{}
	service.On("Do", mock.Anything).Times(1).Return(nil)

	anotherService := &Service{}

	// We will execute service 1 time. No retries
	err := Retry(service.Do, 5, time.Millisecond, func() { _ = anotherService.Do() })

	require.NoError(t, err)
	mock.AssertExpectationsForObjects(t, service, anotherService)
}

func Test_RetryError(t *testing.T) {
	service := &Service{}
	var err = errors.New("some error")
	service.On("Do", mock.Anything).Times(5).Return(err)

	anotherService := &Service{}
	anotherService.On("Do", mock.Anything).Times(5).Return(nil)

	// We will execute service all the retries and error will be returned
	actualErr := Retry(service.Do, 5, time.Millisecond, func() { _ = anotherService.Do() })

	assert.ErrorIs(t, actualErr, err)
	mock.AssertExpectationsForObjects(t, service, anotherService)
}
