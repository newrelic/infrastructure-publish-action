package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"time"
)

type Logf func(format string, args ...interface{})

const markerName = "releases.json"

var ErrLastMarkerEnded = errors.New("last marker is already ended")
var ErrNoStartedMarkersFound = errors.New("no started markers found")
var ErrNoStartedMarkerFoundForApp = errors.New("no started marker found for app")
var ErrCannotWriteMarkerFile = errors.New("cannot write marker file")
var ErrNotStartedMark = errors.New("not started mark")

// S3Config markerAWS lock config DTO.
type S3Config struct {
	Directory string
	Bucket    string
	RoleARN   string
	Region    string
}

// S3Client aws client interface for testing
type S3Client interface {
	PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error)
	GetObject(input *s3.GetObjectInput) (*s3.GetObjectOutput, error)
}

type TimeProvider interface {
	Now() time.Time
}

type RealTimeProvider struct{}

func (RealTimeProvider) Now() time.Time {
	return time.Now()
}

type markerAWS struct {
	client       S3Client
	conf         S3Config
	timeProvider TimeProvider
	logfn        Logf
}

// NewMarkerAWS creates a new marker using AWS S3
// it returns an interface on purpose, so this way
// we can have markerAWS unexported and force the
// usage of the constructor
func NewMarkerAWS(s3Config S3Config, logfn Logf) (Marker, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	creds := stscreds.NewCredentials(sess, s3Config.RoleARN, func(p *stscreds.AssumeRoleProvider) {})
	awsCfg := aws.Config{
		Credentials: creds,
		Region:      aws.String(s3Config.Region),
	}

	return &markerAWS{
		client:       s3.New(sess, &awsCfg),
		conf:         s3Config,
		timeProvider: RealTimeProvider{},
		logfn:        logfn,
	}, nil
}

// Start will:
// load all the markers from the file
// append a new started marker
// write the markers back to the file
func (s *markerAWS) Start(releaseInfo ReleaseInfo) (Mark, error) {
	s.logfn("[marker] starting %s", releaseInfo.AppName)
	markers, err := s.readMarkers()
	if err != nil {
		if !isNoSuchKeyError(err) {
			return Mark{}, err
		}
	}

	mark := Mark{
		ReleaseInfo: releaseInfo,
		Start:       CustomTime{s.now()},
	}

	markers = append(markers, mark)
	if err = s.writeMarkers(markers); err != nil {
		return mark, err
	}

	return mark, nil
}

// End will:
// load all the markers from the file
// find the last started marker
// append the end time to the last started marker
// write the markers back to the file
func (s *markerAWS) End(mark Mark) error {
	s.logfn("[marker] ending %s", mark.AppName)
	if mark.Start.IsZero() {
		return ErrNotStartedMark
	}

	markers, err := s.readMarkers()
	if err != nil {
		return err
	}
	//ensure the latest marker is the one being ended
	if len(markers) == 0 {
		return ErrNoStartedMarkersFound
	}

	lastMarker := markers[len(markers)-1]
	if !lastMarker.End.IsZero() {
		return ErrLastMarkerEnded
	}

	if lastMarker.AppName != mark.AppName || !lastMarker.Start.Equals(mark.Start) {
		return fmt.Errorf("%w started:%s appName:%s", ErrNoStartedMarkerFoundForApp, mark.Start, mark.AppName)
	}

	lastMarker.End = CustomTime{s.now()}
	markers[len(markers)-1] = lastMarker

	err = s.writeMarkers(markers)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotWriteMarkerFile, err)
	}

	return nil
}

func (s *markerAWS) writeMarkers(markers []Mark) error {
	markersBytes, err := json.MarshalIndent(markers, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode marker file: %w", err)
	}

	s.logfn("[marker] writing bucket:%s key:%s", s.conf.Bucket, s.markerPath())
	_, err = s.client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.conf.Bucket),
		Key:    aws.String(s.markerPath()),
		Body:   aws.ReadSeekCloser(bytes.NewReader(markersBytes)),
	})
	if err != nil {
		return fmt.Errorf("cannot write marker file: %w", err)
	}

	return nil
}

func (s *markerAWS) readMarkers() ([]Mark, error) {
	objOutput, err := s.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.conf.Bucket),
		Key:    aws.String(s.markerPath()),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot read marker file: %w", err)
	}

	var markers []Mark
	err = json.NewDecoder(objOutput.Body).Decode(&markers)
	if err != nil {
		return nil, fmt.Errorf("cannot decode marker file: %w", err)
	}

	return markers, nil
}

func (s *markerAWS) markerPath() string {
	return s.conf.Directory + "/" + markerName
}

func (s *markerAWS) now() time.Time {
	return s.timeProvider.Now().UTC()
}

// from github.com/aws/aws-sdk-go/aws/awserr/error.go
func isNoSuchKeyError(err error) bool {
	var awsErr awserr.Error
	if errors.As(err, &awsErr) {
		return awsErr.Code() == "NoSuchKey"
	}
	return false
}
