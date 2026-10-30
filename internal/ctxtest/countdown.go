// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ctxtest

import (
	"context"
	"sync"
)

// Countdown is a context whose Err answers nil for its first Left calls and
// context.Canceled from then on, counting every call in Calls. It puts a
// cancel at a chosen check of the context, deterministically, where a timer
// would race the work. It embeds the context it decorates for the rest, Done
// among it, so only work that asks Err sees the cancel. It is used by pointer.
type Countdown struct {
	context.Context
	// OnRefuse, when set, is called once, as Err first answers
	// context.Canceled, so a test can note what the work had done by then. It
	// runs under the context's lock, so it must not ask the context.
	OnRefuse func()

	mu          sync.Mutex
	Left, Calls int
	refused     bool
}

// Err implements context.Context.
func (c *Countdown) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Calls++
	if c.Left > 0 {
		c.Left--
		return nil
	}
	if !c.refused && c.OnRefuse != nil {
		c.OnRefuse()
	}
	c.refused = true
	return context.Canceled
}

// Refused reports whether Err has answered context.Canceled yet. It is safe
// to call while the work is still asking.
func (c *Countdown) Refused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refused
}
