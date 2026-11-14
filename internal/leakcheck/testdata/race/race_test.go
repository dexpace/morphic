package race

import (
	"sync"
	"testing"
	"time"
)

// TestRace writes and reads shared on two goroutines with nothing ordering
// them. The read waits, so the write is in the detector's history when it runs.
func TestRace(t *testing.T) {
	var wg sync.WaitGroup
	wg.Go(func() { Write() })
	wg.Go(func() { time.Sleep(50 * time.Millisecond); _ = Read() })
	wg.Wait()
}
