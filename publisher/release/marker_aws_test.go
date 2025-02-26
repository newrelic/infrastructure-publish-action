package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

var nolog = func(format string, args ...interface{}) {}

func Test_Start(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	// It should read existing markers
	existingMarkers := `
		[
			{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
			{"app_name": "app2", "tag": "v1.1", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "2023-01-02T01:00:00Z", "repo_name": "repo2", "schema": "schema2", "schema_url": "url2"}
		]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the new marker
	startTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(startTime)

	expectedMarkers := mustPrettify(`[
		{"app_name":"app1","tag":"v1.0","run_id":"run1","start":"2023-01-01T00:00:00Z","end":"2023-01-01T01:00:00Z","repo_name":"repo1","schema":"schema1","schema_url":"url1"},
		{"app_name":"app2","tag":"v1.1","run_id":"run2","start":"2023-01-02T00:00:00Z","end":"2023-01-02T01:00:00Z","repo_name":"repo2","schema":"schema2","schema_url":"url2"},
		{"app_name":"my-app","tag":"v1.2","run_id":"run3","start":"2025-03-04T11:12:13Z","end":"0001-01-01T00:00:00Z","repo_name":"repo3","schema":"schema3","schema_url":"url3"}
	]`)

	putBody := aws.ReadSeekCloser(bytes.NewReader([]byte(expectedMarkers)))
	s3ClientMock.ShouldPutObject(
		&s3.PutObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName)), Body: putBody},
		&s3.PutObjectOutput{},
	)

	appName := "my-app"
	tag := "v1.2"
	runID := "run3"
	repoName := "repo3"
	schema := "schema3"
	schemaURL := "url3"
	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	marker, err := markerS3.Start(appName, tag, runID, repoName, schema, schemaURL)
	require.NoError(t, err)
	require.Equal(t, appName, marker.AppName)
	require.Equal(t, tag, marker.Tag)
	require.Equal(t, runID, marker.RunID)
	require.Equal(t, repoName, marker.RepoName)
	require.Equal(t, schema, marker.Schema)
	require.Equal(t, schemaURL, marker.SchemaURl)
}

func Test_StartErrorReadingMarkers(t *testing.T) {
	appName := "my-app"
	tag := "v1.2"
	runID := "run3"
	repoName := "repo3"
	schema := "schema3"
	schemaURL := "url3"
	timeProviderMock := &TimeProviderMock{}
	s3ClientMock := &S3ClientMock{}

	s3Config := S3Config{
		Bucket:  "bucket",
		RoleARN: "role",
		Region:  "region",
	}

	var someError = errors.New("error reading markers")
	s3ClientMock.ShouldReturnErrorOnGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		someError)

	markerS3 := &markerAWS{timeProvider: timeProviderMock, conf: s3Config, client: s3ClientMock, logfn: nolog}
	_, err := markerS3.Start(appName, tag, runID, repoName, schema, schemaURL)
	assert.ErrorIs(t, err, someError)
}

func Test_StartErrorWritingMarkers(t *testing.T) {
	appName := "my-app"
	tag := "v1.2"
	runID := "run3"
	repoName := "repo3"
	schema := "schema3"
	schemaURL := "url3"
	timeProviderMock := &TimeProviderMock{}
	s3ClientMock := &S3ClientMock{}

	s3Config := S3Config{
		Bucket:  "bucket",
		RoleARN: "role",
		Region:  "region",
	}

	// It should read existing markers
	existingMarkers := `
		[
			{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
			{"app_name": "app2", "tag": "v1.1", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "2023-01-02T01:00:00Z", "repo_name": "repo2", "schema": "schema2", "schema_url": "url2"}
		]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the new marker
	startTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(startTime)

	expectedMarkers := mustPrettify(`[
		{"app_name":"app1","tag":"v1.0","run_id":"run1","start":"2023-01-01T00:00:00Z","end":"2023-01-01T01:00:00Z","repo_name":"repo1","schema":"schema1","schema_url":"url1"},
		{"app_name":"app2","tag":"v1.1","run_id":"run2","start":"2023-01-02T00:00:00Z","end":"2023-01-02T01:00:00Z","repo_name":"repo2","schema":"schema2","schema_url":"url2"},
		{"app_name":"my-app","tag":"v1.2","run_id":"run3","start":"2025-03-04T11:12:13Z","end":"0001-01-01T00:00:00Z","repo_name":"repo3","schema":"schema3","schema_url":"url3"}
	]`)

	putBody := aws.ReadSeekCloser(bytes.NewReader([]byte(expectedMarkers)))
	var someError = errors.New("error writing markers")
	s3ClientMock.ShouldReturnErrorOnPutObject(
		&s3.PutObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName)), Body: putBody},
		someError)

	markerS3 := &markerAWS{timeProvider: timeProviderMock, conf: s3Config, client: s3ClientMock, logfn: nolog}
	_, err := markerS3.Start(appName, tag, runID, repoName, schema, schemaURL)
	assert.ErrorIs(t, err, someError)
}
func Test_End(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:  "bucket",
		RoleARN: "role",
		Region:  "region",
	}

	// It should read existing markers
	existingMarkers := `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "my-app", "tag": "v1.2", "run_id": "run3", "start": "2023-01-02T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo3", "schema": "schema3", "schema_url": "url3"}
	]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	expectedMarkers := mustPrettify(`[
		{"app_name":"app1","tag":"v1.0","run_id":"run1","start":"2023-01-01T00:00:00Z","end":"2023-01-01T01:00:00Z","repo_name":"repo1","schema":"schema1","schema_url":"url1"},
		{"app_name":"my-app","tag":"v1.2","run_id":"run3","start":"2023-01-02T00:00:00Z","end":"2025-03-04T11:12:13Z","repo_name":"repo3","schema":"schema3","schema_url":"url3"}
	]`)

	putBody := aws.ReadSeekCloser(bytes.NewReader([]byte(expectedMarkers)))
	s3ClientMock.ShouldPutObject(
		&s3.PutObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName)), Body: putBody},
		&s3.PutObjectOutput{},
	)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "schema_url",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	require.NoError(t, err)
}

func Test_End_ErrorOnWriting(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:  "bucket",
		RoleARN: "role",
		Region:  "region",
	}

	// It should read existing markers
	existingMarkers := `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "my-app", "tag": "v1.2", "run_id": "run3", "start": "2023-01-02T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo3", "schema": "schema3", "schema_url": "url3"}
	]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	expectedMarkers := mustPrettify(`[
		{"app_name":"app1","tag":"v1.0","run_id":"run1","start":"2023-01-01T00:00:00Z","end":"2023-01-01T01:00:00Z","repo_name":"repo1","schema":"schema1","schema_url":"url1"},
		{"app_name":"my-app","tag":"v1.2","run_id":"run3","start":"2023-01-02T00:00:00Z","end":"2025-03-04T11:12:13Z","repo_name":"repo3","schema":"schema3","schema_url":"url3"}
	]`)

	putBody := aws.ReadSeekCloser(bytes.NewReader([]byte(expectedMarkers)))
	var someError = errors.New("error writing markers")
	s3ClientMock.ShouldReturnErrorOnPutObject(
		&s3.PutObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName)), Body: putBody},
		someError)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "schema_url",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, someError)
}

func Test_End_ErrorIfNoMarkerFound(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	// It should read existing markers
	existingMarkers := `[]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "schema_url",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNoStartedMarkersFound)
}
func Test_End_ErrorOnReadingMarkers(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	var someError = errors.New("error reading markers")
	s3ClientMock.ShouldReturnErrorOnGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		someError)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "schema_url",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, someError)
}

func Test_EndFailsIfLatestMarkerIsEnded(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	// It should read existing markers
	existingMarkers := `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "my-app", "tag": "v1.2", "run_id": "run3", "start": "2023-01-02T00:00:00Z", "end": "2023-01-02T01:00:00Z", "repo_name": "repo3", "schema": "schema3", "schema_url": "url3"}
	]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "schema_url",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrLastMarkerEnded)
}

func Test_EndFailsIfMarkerForAppNotFound(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	// It should read existing markers
	existingMarkers := `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "my-app", "tag": "v1.2", "run_id": "run3", "start": "2023-01-02T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo3", "schema": "schema3", "schema_url": "url3"}
	]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "another-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "url3",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNoStartedMarkerFoundForApp)
}

func Test_EndFailsIfMarkerStartTimeIsDifferent(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	// It should read existing markers
	existingMarkers := `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "my-app", "tag": "v1.2", "run_id": "run3", "start": "2023-01-02T11:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo3", "schema": "schema3", "schema_url": "url3"}
	]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "url3",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNoStartedMarkerFoundForApp)
}

func Test_EndFailsIfMarkerStartIsZero(t *testing.T) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}

	s3Config := S3Config{
		Bucket:    "bucket",
		RoleARN:   "role",
		Region:    "region",
		Directory: "directory",
	}

	// It should read existing markers
	existingMarkers := `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "my-app", "tag": "v1.2", "run_id": "run3", "start": "2023-01-02T11:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo3", "schema": "schema3", "schema_url": "url3"}
	]`

	reader := bytes.NewReader([]byte(existingMarkers))
	readCloser := io.NopCloser(reader)

	s3ClientMock.ShouldGetObject(
		&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
		&s3.GetObjectOutput{Body: readCloser})

	// It should get current time for the end of the started marker
	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	startTime := time.Time{}
	mark := Mark{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURl: "url3",
		Start:     CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNotStartedMark)
}

///////////////////////////////////////////////////////////////
// S3 client Mock
///////////////////////////////////////////////////////////////

type S3ClientMock struct {
	mock.Mock
}

func (m *S3ClientMock) PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	args := m.Called(input)

	return args.Get(0).(*s3.PutObjectOutput), args.Error(1)
}

func (m *S3ClientMock) GetObject(input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	args := m.Called(input)

	return args.Get(0).(*s3.GetObjectOutput), args.Error(1)
}

func (m *S3ClientMock) ShouldPutObject(input *s3.PutObjectInput, output *s3.PutObjectOutput) {
	m.
		On("PutObject", input).
		Once().
		Return(output, nil)
}

func (m *S3ClientMock) ShouldReturnErrorOnPutObject(input *s3.PutObjectInput, err error) {
	m.
		On("PutObject", input).
		Once().
		Return(&s3.PutObjectOutput{}, err)
}

func (m *S3ClientMock) ShouldGetObject(input *s3.GetObjectInput, output *s3.GetObjectOutput) {
	m.
		On("GetObject", input).
		Once().
		Return(output, nil)
}

func (m *S3ClientMock) ShouldReturnErrorOnGetObject(input *s3.GetObjectInput, err error) {
	m.
		On("GetObject", input).
		Once().
		Return(&s3.GetObjectOutput{}, err)
}

///////////////////////////////////////////////////////////////
// Time provider Mock
///////////////////////////////////////////////////////////////

type TimeProviderMock struct {
	mock.Mock
}

func (m *TimeProviderMock) Now() time.Time {
	args := m.Called()

	return args.Get(0).(time.Time)
}

func (m *TimeProviderMock) ShouldProvideNow(now time.Time) {
	m.
		On("Now").
		Once().
		Return(now)
}

func mustPrettify(jsonStr string) string {
	var jsonObj []Mark
	err := json.Unmarshal([]byte(jsonStr), &jsonObj)
	if err != nil {
		panic(err)
	}

	prettyJSON, err := json.MarshalIndent(jsonObj, "", "  ")
	if err != nil {
		panic(err)
	}

	return string(prettyJSON)
}
