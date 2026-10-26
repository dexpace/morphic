package ctxtest

import (
	"context"
	"sync"
)

// Countdown is a context whose Err answers nil for its first Left calls and
// context.Canceled from then on, counting every call in Calls. It puts a
// cancel at a chosen check of the context, deterministically, where a timer
// would race the work. It embeds the context it decorates for the rest, and is
// used by pointer.
type Countdown struct {
	context.Context

	mu          sync.Mutex
	Left, Calls int
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
	return context.Canceled
}
