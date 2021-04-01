// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// We should parametrise these:
var (
	// resource tags
	tagOwningTeam = "CAOS"
	tagProduct    = "integrations"
	tagProject    = "infrastructure-publish-action"
	tagEnv        = "us-development"
)

// S3Config S3 lock config DTO.
type S3Config struct {
	Bucket       string
	RoleARN      string
	Region       string
	Filepath     string
	Owner        string
	MaxRetries   uint
	RetryBackoff time.Duration
	TTL          time.Duration
}

// S3 based lock.
type S3 struct {
	client       *s3.S3
	owner        string
	bucket       string
	tags         string
	filePath     string
	maxRetries   uint
	retryBackoff time.Duration
	ttl          time.Duration
}

// lockData represents contents of the JSON lock-file at S3.
type lockData struct {
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
}

// same owner means lock was already acquired by the client
func (l *lockData) belongsTo(owner string) bool {
	return l.Owner == owner
}

func (l *lockData) isExpired(ttl time.Duration, t time.Time) bool {
	return l.CreatedAt.Add(ttl).Before(t)
}

func NewS3Config(bucketName, roleARN, awsRegion, lockGroup, owner string, maxRetries uint, retryBackoff, ttl time.Duration) S3Config {
	return S3Config{
		Bucket:       bucketName,
		RoleARN:      roleARN,
		Region:       awsRegion,
		Filepath:     lockGroup,
		Owner:        owner,
		TTL:          ttl,
		RetryBackoff: retryBackoff,
		MaxRetries:   maxRetries,
	}
}

// NewS3 creates a lock instance ready to be used validating required AWS credentials.
func NewS3(c S3Config) (*S3, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	creds := stscreds.NewCredentials(sess, c.RoleARN, func(p *stscreds.AssumeRoleProvider) {})
	awsCfg := aws.Config{
		Credentials: creds,
		Region:      aws.String(c.Region),
	}

	return &S3{
		client:       s3.New(sess, &awsCfg),
		owner:        c.Owner,
		bucket:       c.Bucket,
		filePath:     c.Filepath,
		tags:         fmt.Sprintf("department=product&product=%s&project=%s&owning_team=%s&environment=%s", tagProduct, tagProject, tagOwningTeam, tagEnv),
		maxRetries:   c.MaxRetries,
		retryBackoff: c.RetryBackoff,
		ttl:          c.TTL,
	}, nil
}

// Lock S3 has no compare-and-swap so this is no bulletproof solution, but should be good enough.
func (l *S3) Lock() error {
	var tries uint
	retry:
	if (l.maxRetries - tries) > 0 {
		tries++
		log.Printf("%s attempt %d", l.owner, tries) //  TODO inject logger
		if l.isBusyDeletingExpired() {
			log.Printf("%s failed, waiting %s", l.owner, l.retryBackoff.String()) //  TODO inject logger
			time.Sleep(l.retryBackoff)
			goto retry
		}
	}

	if l.isBusyDeletingExpired() {
		return ErrLockBusy
	}

	data := lockData{
		Owner:     l.owner,
		CreatedAt: time.Now(),
	}
	dataB, err := json.Marshal(data)
	if err != nil {
		return err
	}

	input := &s3.PutObjectInput{
		Body:    aws.ReadSeekCloser(bytes.NewReader(dataB)),
		Bucket:  aws.String(l.bucket),
		Key:     aws.String(l.filePath),
		Tagging: aws.String(l.tags),
	}

	_, err = l.client.PutObject(input)
	if err != nil {
		return err
	}

	if l.isBusyDeletingExpired() {
		return ErrLockBusy
	}

	return nil
}

// Release frees owned lock.
func (l *S3) Release() error {
	if l.isBusyDeletingExpired() {
		return ErrLockBusy
	}

	delObjIn := &s3.DeleteObjectInput{
		Bucket: aws.String(l.bucket),
		Key:    aws.String(l.filePath),
	}

	_, err := l.client.DeleteObject(delObjIn)
	if err != nil {
		return err
	}

	if l.isBusyDeletingExpired() {
		return ErrLockBusy
	}

	return nil
}

// isBusyDeletingExpired verifies there is a not expired lock, not owned by this client.
// It also deletes expired ones for the shake of management simplicacation.
func (l *S3) isBusyDeletingExpired() (busy bool) {
	busy = true

	readObjIn := &s3.GetObjectInput{
		Bucket: aws.String(l.bucket),
		Key:    aws.String(l.filePath),
	}

	resp, err := l.client.GetObject(readObjIn)
	// no lock file error (404) means lock is idle
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchKey:
				busy = false
				return
			default:
			}
		}
		logErr(err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logErr(err)
		return
	}
	var data lockData
	err = json.Unmarshal(body, &data)
	if err != nil {
		logErr(err)
		return
	}

	if data.isExpired(l.ttl, time.Now()) {
		busy = false
		return
	}

	busy = !data.belongsTo(l.owner)

	return
}

func logErr(err error) {
	log.Println(err)
}
