// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"testing"

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
	l, err := NewS3(bucket, roleARN, region, "TestNewS3", "owner", 0)
	require.NoError(t, err)

	assert.NotEmpty(t, l)
}

func TestS3_Lock(t *testing.T) {
	l, err := NewS3(bucket, roleARN, region, "TestS3_Lock", "owner", 0)
	require.NoError(t, err)

	assert.NoError(t, l.Lock())
	defer l.Release()
}

func TestS3_Lock_onLocked(t *testing.T) {
	l1, err := NewS3(bucket, roleARN, region, "TestS3_Lock_onLocked", "owner-1", 0)
	require.NoError(t, err)

	l2, err := NewS3(bucket, roleARN, region, "TestS3_Lock_onLocked", "owner-2", 0)
	require.NoError(t, err)

	assert.NoError(t, l1.Lock())
	assert.Equal(t, ErrLockBusy, l2.Lock())

	defer l1.Release()
	defer l2.Release()
}

func TestS3_Release(t *testing.T) {
	l, err := NewS3(bucket, roleARN, region, "TestS3_Release", "owner", 0)
	require.NoError(t, err)

	assert.NoError(t, l.Lock())
	assert.NoError(t, l.Release())
}
