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

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}

	releaseInfo := ReleaseInfo{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURL: "url3",
	}
	mark, err := markerS3.Start(releaseInfo)
	require.NoError(t, err)
	require.Equal(t, releaseInfo, mark.ReleaseInfo)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_StartErrorReadingMarkers(t *testing.T) {
	releaseInfo := ReleaseInfo{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURL: "url3",
	}
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
	_, err := markerS3.Start(releaseInfo)
	assert.ErrorIs(t, err, someError)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_StartErrorWritingMarkers(t *testing.T) {
	releaseInfo := ReleaseInfo{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURL: "url3",
	}
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
	_, err := markerS3.Start(releaseInfo)
	assert.ErrorIs(t, err, someError)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	releaseInfo := ReleaseInfo{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURL: "url3",
	}
	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		ReleaseInfo: releaseInfo,
		Start:       CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	require.NoError(t, err)
	require.Equal(t, releaseInfo, mark.ReleaseInfo)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	releaseInfo := ReleaseInfo{
		AppName:   "my-app",
		Tag:       "v1.2",
		RunID:     "run3",
		RepoName:  "repo3",
		Schema:    "schema3",
		SchemaURL: "url3",
	}
	mark := Mark{
		ReleaseInfo: releaseInfo,
		Start:       CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, someError)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		ReleaseInfo: ReleaseInfo{
			AppName:   "my-app",
			Tag:       "v1.2",
			RunID:     "run3",
			RepoName:  "repo3",
			Schema:    "schema3",
			SchemaURL: "url3",
		},
		Start: CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNoStartedMarkersFound)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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
		ReleaseInfo: ReleaseInfo{
			AppName:   "my-app",
			Tag:       "v1.2",
			RunID:     "run3",
			RepoName:  "repo3",
			Schema:    "schema3",
			SchemaURL: "url3",
		},
		Start: CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, someError)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		ReleaseInfo: ReleaseInfo{
			AppName:   "my-app",
			Tag:       "v1.2",
			RunID:     "run3",
			RepoName:  "repo3",
			Schema:    "schema3",
			SchemaURL: "url3",
		},
		Start: CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrLastMarkerEnded)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		ReleaseInfo: ReleaseInfo{
			AppName:   "another-app",
			Tag:       "v1.2",
			RunID:     "run3",
			RepoName:  "repo3",
			Schema:    "schema3",
			SchemaURL: "url3",
		},
		Start: CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNoStartedMarkerFoundForApp)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	startTime := time.Date(2023, 1, 2, 00, 00, 00, 0, time.UTC)
	mark := Mark{
		ReleaseInfo: ReleaseInfo{
			AppName:   "my-app",
			Tag:       "v1.2",
			RunID:     "run3",
			RepoName:  "repo3",
			Schema:    "schema3",
			SchemaURL: "url3",
		},
		Start: CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNoStartedMarkerFoundForApp)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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

	startTime := time.Time{}
	mark := Mark{
		ReleaseInfo: ReleaseInfo{
			AppName:   "my-app",
			Tag:       "v1.2",
			RunID:     "run3",
			RepoName:  "repo3",
			Schema:    "schema3",
			SchemaURL: "url3",
		},
		Start: CustomTime{startTime},
	}

	markerS3 := &markerAWS{
		client:       s3ClientMock,
		conf:         s3Config,
		timeProvider: timeProviderMock,
		logfn:        nolog,
	}
	err := markerS3.End(mark)
	assert.ErrorIs(t, err, ErrNotStartedMark)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

// batch markers as left by a batch run: an older unended marker (a killed run), then the batch's
// own markers, all started in the same second
const batchExistingMarkers = `
	[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "nri-apache", "tag": "v1.18.2", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "newrelic/nri-apache", "schema": "ohi", "schema_url": ""},
		{"app_name": "nri-apache-fips", "tag": "v1.18.2", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "newrelic/nri-apache", "schema": "ohi-fips", "schema_url": ""},
		{"app_name": "nri-mysql", "tag": "v1.25.0", "run_id": "run2", "start": "2023-01-02T00:00:01Z", "end": "0001-01-01T00:00:00Z", "repo_name": "newrelic/nri-mysql", "schema": "ohi", "schema_url": ""}
	]`

func batchMarks() []Mark {
	mark := func(app, tag, repo, schema string, start time.Time) Mark {
		return Mark{ReleaseInfo: ReleaseInfo{AppName: app, Tag: tag, RunID: "run2", RepoName: repo, Schema: schema}, Start: CustomTime{start}}
	}
	return []Mark{
		mark("nri-apache", "v1.18.2", "newrelic/nri-apache", "ohi", time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)),
		mark("nri-apache-fips", "v1.18.2", "newrelic/nri-apache", "ohi-fips", time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)),
		mark("nri-mysql", "v1.25.0", "newrelic/nri-mysql", "ohi", time.Date(2023, 1, 2, 0, 0, 1, 0, time.UTC)),
	}
}

func newBatchMarkerTest(existing string) (*markerAWS, *S3ClientMock, *TimeProviderMock, S3Config) {
	s3ClientMock := &S3ClientMock{}
	timeProviderMock := &TimeProviderMock{}
	s3Config := S3Config{Bucket: "bucket", RoleARN: "role", Region: "region", Directory: "directory"}
	if existing != "" {
		s3ClientMock.ShouldGetObject(
			&s3.GetObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName))},
			&s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader([]byte(existing)))})
	}
	return &markerAWS{client: s3ClientMock, conf: s3Config, timeProvider: timeProviderMock, logfn: nolog}, s3ClientMock, timeProviderMock, s3Config
}

func Test_EndBatch(t *testing.T) {
	markerS3, s3ClientMock, timeProviderMock, s3Config := newBatchMarkerTest(batchExistingMarkers)

	endTime := time.Date(2025, 3, 4, 11, 12, 13, 0, time.UTC)
	timeProviderMock.ShouldProvideNow(endTime)

	// every batch marker ended with one write; the older unended marker is left alone
	expectedMarkers := mustPrettify(`[
		{"app_name": "app1", "tag": "v1.0", "run_id": "run1", "start": "2023-01-01T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "repo1", "schema": "schema1", "schema_url": "url1"},
		{"app_name": "nri-apache", "tag": "v1.18.2", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "2025-03-04T11:12:13Z", "repo_name": "newrelic/nri-apache", "schema": "ohi", "schema_url": ""},
		{"app_name": "nri-apache-fips", "tag": "v1.18.2", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "2025-03-04T11:12:13Z", "repo_name": "newrelic/nri-apache", "schema": "ohi-fips", "schema_url": ""},
		{"app_name": "nri-mysql", "tag": "v1.25.0", "run_id": "run2", "start": "2023-01-02T00:00:01Z", "end": "2025-03-04T11:12:13Z", "repo_name": "newrelic/nri-mysql", "schema": "ohi", "schema_url": ""}
	]`)
	s3ClientMock.ShouldPutObject(
		&s3.PutObjectInput{Bucket: &s3Config.Bucket, Key: aws.String(fmt.Sprintf("%s/%s", s3Config.Directory, markerName)), Body: aws.ReadSeekCloser(bytes.NewReader([]byte(expectedMarkers)))},
		&s3.PutObjectOutput{},
	)

	err := markerS3.EndBatch(batchMarks())
	require.NoError(t, err)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_EndBatch_EndOneByOneOnlyEndsLast(t *testing.T) {
	// the bug EndBatch fixes: End on the first app of a batch fails, its marker is not the last one
	markerS3, s3ClientMock, timeProviderMock, _ := newBatchMarkerTest(batchExistingMarkers)

	err := markerS3.End(batchMarks()[0])
	assert.ErrorIs(t, err, ErrNoStartedMarkerFoundForApp)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_EndBatch_FailsIfMarksAreNotTheLastMarkers(t *testing.T) {
	markerS3, s3ClientMock, timeProviderMock, _ := newBatchMarkerTest(batchExistingMarkers)

	// someone else's marker after the batch's ones
	err := markerS3.EndBatch(batchMarks()[:2])
	assert.ErrorIs(t, err, ErrNoStartedMarkerFoundForApp)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_EndBatch_FailsIfAMarkerIsEnded(t *testing.T) {
	ended := `
	[
		{"app_name": "nri-apache", "tag": "v1.18.2", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "0001-01-01T00:00:00Z", "repo_name": "newrelic/nri-apache", "schema": "ohi", "schema_url": ""},
		{"app_name": "nri-apache-fips", "tag": "v1.18.2", "run_id": "run2", "start": "2023-01-02T00:00:00Z", "end": "2023-01-02T01:00:00Z", "repo_name": "newrelic/nri-apache", "schema": "ohi-fips", "schema_url": ""},
		{"app_name": "nri-mysql", "tag": "v1.25.0", "run_id": "run2", "start": "2023-01-02T00:00:01Z", "end": "0001-01-01T00:00:00Z", "repo_name": "newrelic/nri-mysql", "schema": "ohi", "schema_url": ""}
	]`
	markerS3, s3ClientMock, timeProviderMock, _ := newBatchMarkerTest(ended)

	err := markerS3.EndBatch(batchMarks())
	assert.ErrorIs(t, err, ErrLastMarkerEnded)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_EndBatch_FailsIfFewerMarkersThanMarks(t *testing.T) {
	markerS3, s3ClientMock, timeProviderMock, _ := newBatchMarkerTest(`[]`)

	err := markerS3.EndBatch(batchMarks())
	assert.ErrorIs(t, err, ErrNoStartedMarkersFound)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_EndBatch_FailsIfAMarkIsNotStarted(t *testing.T) {
	markerS3, s3ClientMock, timeProviderMock, _ := newBatchMarkerTest("")

	marks := batchMarks()
	marks[1].Start = CustomTime{}
	err := markerS3.EndBatch(marks)
	assert.ErrorIs(t, err, ErrNotStartedMark)
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
}

func Test_EndBatch_NoMarks(t *testing.T) {
	markerS3, s3ClientMock, timeProviderMock, _ := newBatchMarkerTest("")

	require.NoError(t, markerS3.EndBatch(nil))
	mock.AssertExpectationsForObjects(t, s3ClientMock, timeProviderMock)
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
