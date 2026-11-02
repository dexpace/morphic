// Package racereach holds one data race, which scripts/race-reach.sh requires
// `go test -race` to report, and nothing else: that is how it shows `make race`
// runs the race detector at all, as a race-free tree would pass without it.
// The accesses are in a non-test file, which coverage instruments: a race run
// with coverage merged in hides them, so the script refuses that run, as
// GitHub #1271 measured. A later toolchain may stop hiding them, which only
// weakens that half of the check.
package racereach

// shared is written by write and read by read with nothing ordering them.
var shared int

// mark is a block both goroutines run.
func mark(n int) int { return n + 1 }

// write stores shared, unordered with read.
func write() {
	shared = 1
	_ = mark(0)
}

// read loads shared, unordered with write.
func read() int {
	_ = mark(1)
	return shared
}
