// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// S3 based lock.
type S3 struct {
	client   *s3.S3
	owner    string
	bucket   string
	tags     string
	filePath string
}

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
	}, nil
}

func (l *S3) Lock() error {
	if l.isLockBusy() {
		return LockBusyErr
	}

	input := &s3.PutObjectInput{
		Body:    aws.ReadSeekCloser(strings.NewReader(l.owner)),
		Bucket:  aws.String(l.bucket),
		Key:     aws.String(l.filePath),
		Tagging: aws.String(l.tags),
	}

	_, err := l.client.PutObject(input)
	if err != nil {
		return err
	}

	if l.isLockBusy() {
		return LockBusyErr
	}

	return nil
}

func (l *S3) Release() error {
	if l.isLockBusy() {
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

	if l.isLockBusy() {
		return LockBusyErr
	}

	return nil
}

// isLockBusy verifies there is a lock not owned by this client.
func (l *S3) isLockBusy() bool {
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
				return false
			default:
			}
		}
		return true
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return true
	}

	// same owner means lock was already acquired by the client
	return string(body) != l.owner
}
