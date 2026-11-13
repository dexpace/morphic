package racereach

import (
	"sync"
	"testing"
	"time"
)

// TestPlantedRace runs write and read on two goroutines, read after a pause,
// so the race detector reports the pair.
func TestPlantedRace(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); write() }()
	go func() { defer wg.Done(); time.Sleep(50 * time.Millisecond); _ = read() }()
	wg.Wait()
}
