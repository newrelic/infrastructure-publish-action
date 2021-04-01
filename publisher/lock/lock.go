// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import "errors"

var (
	ErrLockBusy = errors.New("lock is busy")
)

type BucketLock interface {
	// Lock tries acquiring lock or fails rigth away.
	Lock() error
	// Release tries releasing an owned lock or fails.
	Release() error
}

type Mode string

const (
	modeDefault = ""
	modeDisabled = "disabled"
	modeRetryOnBusy = "retry_on_busy"
	modeFailOnBusy = "fail_on_busy"
)

func (m Mode) IsValid() bool {
	switch m {
	case modeDefault:
		return true
	case modeDisabled:
		return true
	case modeRetryOnBusy:
		return true
	case modeFailOnBusy:
		return true
	}
	return false
}

func (m Mode) IsDisabled() bool {
	return m == modeDefault || m == modeDisabled
}

func (m Mode) IsRetryOnBusy() bool {
	return m == modeRetryOnBusy
}

func (m Mode) IsFailOnBusy() bool {
	return m == modeFailOnBusy
}

type noop struct{}

// Noop returns a NO-OP lock, to be used when releasing stuff that won't need locking.
func NewNoop() BucketLock {
	return &noop{}
}

func (l *noop) Lock() error    { return nil }
func (l *noop) Release() error { return nil }
