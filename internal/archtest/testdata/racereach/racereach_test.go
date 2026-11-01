// Package racereach holds one data race, which scripts/race-reach.sh requires
// `go test -race` to report, and nothing else: that is how it shows `make race`
// runs the race detector at all, as a race-free tree would pass without it.
// It is test code alone, so the layering audit, which reads production files,
// meets no package here, and coverage, which instruments no test file, cannot
// hide it as atomic coverage hid the same race in production code (GitHub
// #1271).
package racereach

import (
	"sync"
	"testing"
	"time"
)

// shared is written by write and read by read with nothing ordering them.
var shared int

// mark is a block both goroutines run.
func mark(n int) int { return n + 1 }

func write() {
	shared = 1
	_ = mark(0)
}

func read() int {
	_ = mark(1)
	return shared
}

// TestPlantedRace runs write and read on two goroutines, read after a pause,
// so the race detector reports the pair.
func TestPlantedRace(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); write() }()
	go func() { defer wg.Done(); time.Sleep(50 * time.Millisecond); _ = read() }()
	wg.Wait()
}
