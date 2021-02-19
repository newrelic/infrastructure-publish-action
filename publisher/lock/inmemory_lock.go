// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"sync/atomic"
	"time"
)

// InMemory lock for testing puposes.
type InMemory struct {
	locked uint32
}

func NewInMemory() *InMemory {
	return &InMemory{
		locked: 0,
	}
}

func (l *InMemory) Lock() error {
	adquired := atomic.CompareAndSwapUint32(&l.locked, 0, 1)
	if !adquired {
		return LockBusyErr
	}

	return nil
}

func (l *InMemory) Release() error {
	atomic.SwapUint32(&l.locked, 0)

	// fake some latency
	time.Sleep(100 * time.Millisecond)

	return nil
}

