package release

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
