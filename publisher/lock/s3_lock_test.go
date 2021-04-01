// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AWS testing config
const (
	roleARN = "arn:aws:iam::017663287629:role/caos_testing"
	bucket  = "onhost-ci-lock-testing"
	region  = "us-east-1"
)

func TestNewS3(t *testing.T) {
	l, err := NewS3(newTestConf(t.Name(), "owner"))
	require.NoError(t, err)

	assert.NotEmpty(t, l)
}

func TestS3_Lock(t *testing.T) {
	l, err := NewS3(newTestConf(t.Name(), "owner"))
	require.NoError(t, err)

	assert.NoError(t, l.Lock())
	defer l.Release()
}

func TestS3_Lock_onLocked(t *testing.T) {
	l1, err := NewS3(newTestConf(t.Name(), "owner-1"))
	require.NoError(t, err)

	l2, err := NewS3(newTestConf(t.Name(), "owner-2"))
	require.NoError(t, err)

	assert.NoError(t, l1.Lock())
	assert.Equal(t, ErrLockBusy, l2.Lock())

	defer l1.Release()
	defer l2.Release()
}

func TestS3_Release(t *testing.T) {
	l, err := NewS3(newTestConf(t.Name(), "owner"))
	require.NoError(t, err)

	assert.NoError(t, l.Lock())
	assert.NoError(t, l.Release())
}

// Complex dist-sys time race here.
// We should decouple components to better test this, but we are rushing so take a seat.
func TestS3_retry(t *testing.T) {
	// GIVEN a 1st lock grabber
	l1, err := NewS3(newTestConf(t.Name(), "owner-1"))
	require.NoError(t, err)
	// AND a 2nd one being a retry grabber
	c2 := newTestConf(t.Name(), "owner-2")
	c2.MaxRetries = 1
	c2.RetryBackoff = 500 * time.Millisecond // big boat indeed, ops are addressing an external API
	l2, err := NewS3(c2)
	require.NoError(t, err)

	// WHEN 1st grabs the lock
	assert.NoError(t, l1.Lock())

	// AND 2nd tries to grab the same
	l2Return := make(chan error, 1)
	go func() {
		l2Return <- l2.Lock()
	}()

	// AND 1st releases before 2nd retry-backoff expires
	go func() {
		<-time.After(c2.RetryBackoff / 2)
		l1.Release()
	}()

	// THEN as 2nd backoff-retry expires it grabs the lock
	select {
	case err = <-l2Return:
		assert.NoError(t, err, "second lock behaves as no retry")
	case <-time.After(c2.RetryBackoff * 2):
		t.Errorf("lock took longer than expected")
	}

	defer l1.Release()
	defer l2.Release()
}

func newTestConf(lockgroup, owner string) S3Config {
	return NewS3Config(bucket, roleARN, region, lockgroup, owner, 0, DefaultRetryBackoff, DefaultTTL)
}
