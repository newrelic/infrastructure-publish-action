// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import "errors"

var (
	LockBusyErr = errors.New("bucket is locked")
)

type BucketLock interface {
	Lock() error
	Release() error
}

