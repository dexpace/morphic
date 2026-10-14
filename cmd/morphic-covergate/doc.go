// Command morphic-covergate judges a coverage profile against the gate's rule
// that every statement runs. make coverage runs the suite and hands it the
// profile.
//
// It counts statements rather than reading go test's "coverage: N%" lines,
// which are rounded: a package at 99.96% prints "100.0%". Blocks are merged by
// position first, because go test can write one package's blocks twice when it
// rejects a cached result (#369), and a raw count would measure the test cache.
//
// Exit code is 0 when every statement ran, 1 when one did not or the profile
// cannot be judged, and 2 on a usage error.
package main
