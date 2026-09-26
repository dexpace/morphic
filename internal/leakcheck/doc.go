// Package leakcheck fails a test binary whose tests leave a goroutine behind.
//
// A package whose tests start a goroutine gives it a TestMain that calls Main
// in place of running m.Run() directly:
//
//	func TestMain(m *testing.M) { leakcheck.Main(m) }
//
// Main checks Go 1.27's goroutineleak profile (runtime/pprof) rather than
// pulling in a third-party dependency. The profile reports only a goroutine
// blocked on a primitive no running goroutine can reach; it does not report
// one still running, or one blocked on a primitive a package-level variable
// still holds. It is test infrastructure outside the compile pipeline, like
// internal/harness.
package leakcheck
