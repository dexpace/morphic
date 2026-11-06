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

// mark is a block both write and read run, so under coverage they share its
// counter. That shared update is what hides the race from a covered run;
// without it the race is reported anyway, and scripts/race-reach.sh would pass
// a run with coverage merged in. It hides the race only while write calls mark
// after its store and read calls it before its load: moving either call to the
// other side of its access brings the race back.
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
