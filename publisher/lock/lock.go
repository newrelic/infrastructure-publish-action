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
