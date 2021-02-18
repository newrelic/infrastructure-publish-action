package lock

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	bucket = "caos-testing"
	region = "us-east-1"
)

func TestNewS3(t *testing.T) {
	l, err := NewS3(bucket, "TestNewS3", "owner", region)
	assert.NoError(t, err)

	assert.NotEmpty(t, l)
}

func TestS3_Lock(t *testing.T) {
	l, err := NewS3(bucket, "TestS3_Lock", "owner", region)
	assert.NoError(t, err)

	assert.NoError(t, l.Lock())
	defer l.Release()
}

func TestS3_Lock_onLocked(t *testing.T) {
	l1, err := NewS3(bucket, "TestS3_Lock_onLocked", "owner-1", region)
	assert.NoError(t, err)

	l2, err := NewS3(bucket, "TestS3_Lock_onLocked", "owner-2", region)
	assert.NoError(t, err)

	assert.NoError(t, l1.Lock())
	assert.Equal(t, LockBusyErr, l2.Lock())

	defer l1.Release()
	defer l2.Release()
}

func TestS3_Release(t *testing.T) {
	l, err := NewS3(bucket, "TestS3_Release", "owner", region)
	assert.NoError(t, err)

	assert.NoError(t, l.Lock())
	assert.NoError(t, l.Release())
}
