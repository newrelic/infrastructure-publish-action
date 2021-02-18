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
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	defaultTTL = 1 * time.Hour
)

// S3 based lock.
type S3 struct {
	client   *s3.S3
	owner    string
	bucket   string
	tags     string
	filePath string
	ttl      time.Duration
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

// NewS3 creates a lock instance ready to be used validating required AWS credentials.
func NewS3(bucket, filepath, owner, region string) (*S3, error) {
	// AWS resource tags
	owningTeam := "CAOS"
	product := "integrations"
	project := "infrastructure-publish-action"
	env := "us-development"

	s, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	// Create a S3 client with additional configuration
	svc := s3.New(s, aws.NewConfig().WithRegion(region))

	return &S3{
		client:   svc,
		owner:    owner,
		bucket:   bucket,
		filePath: filepath,
		tags:     fmt.Sprintf("department=product&product=%s&project=%s&owning_team=%s&environment=%s", product, project, owningTeam, env),
		ttl:      defaultTTL,
	}, nil
}

// Lock S3 has no compare-and-swap so this is a
func (l *S3) Lock() error {
	if l.isBusyDeletingExpired() {
		return LockBusyErr
	}

	data := lockData{
		Owner: l.owner,
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
		return LockBusyErr
	}

	return nil
}

// Release frees owned lock.
func (l *S3) Release() error {
	if l.isBusyDeletingExpired() {
		return LockBusyErr
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
		return LockBusyErr
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
