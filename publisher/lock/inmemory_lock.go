// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import (
	"sync"
	"time"
)

// InMemory lock for testing puposes.
type InMemory struct {
	locked bool
	mutex sync.Mutex
}

func NewInMemory() *InMemory {
	return &InMemory{
		mutex: sync.Mutex{},
	}
}

func (l *InMemory) Lock() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.locked {
		return LockBusyErr
	}

	l.locked = true
	return nil
}

func (l *InMemory) Release() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	// fake some latency
	time.Sleep(10 * time.Millisecond)

	l.locked = false
	return nil
}

