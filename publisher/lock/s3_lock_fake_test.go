// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"bytes"
	"io/ioutil"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeS3 is an in-memory bucket implementing s3API, so locks can be tested without AWS.
// Share one instance between locks to simulate them using the same bucket.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}}
}

func (f *fakeS3) GetObject(in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[aws.StringValue(in.Key)]
	if !ok {
		return nil, awserr.New(s3.ErrCodeNoSuchKey, "not found", nil)
	}
	return &s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(b))}, nil
}

func (f *fakeS3) PutObject(in *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	b, err := ioutil.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[aws.StringValue(in.Key)] = b
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(in *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, aws.StringValue(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func newFakeLock(client s3API, lockGroup, owner string) *S3 {
	c := NewS3Config("bucket", "", "", "", lockGroup, owner, 0, DefaultRetryBackoff, DefaultTTL)
	return newS3WithClient(client, c, func(string, ...interface{}) {})
}

func TestFakeS3_Lock(t *testing.T) {
	l := newFakeLock(newFakeS3(), "group", "owner")

	assert.NoError(t, l.Lock())
	assert.NoError(t, l.Release())
}

func TestFakeS3_Lock_sameGroupIsBusy(t *testing.T) {
	bucket := newFakeS3()
	l1 := newFakeLock(bucket, "group", "owner-1")
	l2 := newFakeLock(bucket, "group", "owner-2")

	require.NoError(t, l1.Lock())
	assert.Equal(t, ErrLockBusy, l2.Lock())
}

func TestFakeS3_Lock_differentGroupsDoNotBlock(t *testing.T) {
	bucket := newFakeS3()
	l1 := newFakeLock(bucket, "group-1", "owner-1")
	l2 := newFakeLock(bucket, "group-2", "owner-2")

	assert.NoError(t, l1.Lock())
	assert.NoError(t, l2.Lock())
	assert.Len(t, bucket.objects, 2)
}

func TestFakeS3_Release_onlyFreesOwnGroup(t *testing.T) {
	bucket := newFakeS3()
	l1 := newFakeLock(bucket, "group-1", "owner-1")
	l2 := newFakeLock(bucket, "group-2", "owner-2")
	require.NoError(t, l1.Lock())
	require.NoError(t, l2.Lock())

	require.NoError(t, l1.Release())

	assert.NotContains(t, bucket.objects, "group-1")
	assert.Contains(t, bucket.objects, "group-2")
}

func TestFakeS3_Lock_expiredLockIsTaken(t *testing.T) {
	bucket := newFakeS3()
	c := NewS3Config("bucket", "", "", "", "group", "owner-2", 0, DefaultRetryBackoff, time.Nanosecond)
	l1 := newFakeLock(bucket, "group", "owner-1")
	l2 := newS3WithClient(bucket, c, func(string, ...interface{}) {})
	require.NoError(t, l1.Lock())
	time.Sleep(time.Millisecond)

	assert.NoError(t, l2.Lock())
}
