// Package race holds the one data race `make race` requires the detector to
// report, which shows the run has -race and nothing that hides a race. Write
// and Read share a block, mark, between their accesses: under
// -covermode=atomic that block's counter orders them, and the race goes
// unreported (#1271). The accesses sit outside the test file, as coverage
// instruments only those.
package race

var shared int

func mark(n int) int { return n + 1 }

// Write sets shared, then runs the block Read runs too.
func Write() { shared = 1; _ = mark(0) }

// Read runs the block Write runs, then reads shared.
func Read() int { _ = mark(1); return shared }
