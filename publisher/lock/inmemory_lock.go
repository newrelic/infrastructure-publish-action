// Copyright 2021 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package lock

import "time"

// InMemory lock for testing puposes.
type InMemory struct {
	locked bool
}

func NewInMemory() *InMemory {
	return &InMemory{}
}

func (l *InMemory) Lock() error {
	if l.locked {
		return LockBusyErr
	}

	l.locked = true
	return nil
}

func (l *InMemory) Release() error {
	// fake some latency
	time.Sleep(10 * time.Millisecond)

	l.locked = false
	return nil
}

