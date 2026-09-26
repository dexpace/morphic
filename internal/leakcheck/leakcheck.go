package leakcheck

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
)

// goroutineLeakProfile is the name Go 1.27's runtime/pprof registers its
// leak-detecting profile under.
const goroutineLeakProfile = "goroutineleak"

// debugLevel is the WriteTo debug argument. 1 symbolizes each stack frame to a
// function name and source line, which is what lets a failure name the test
// that started the leaked goroutine; 0 would emit the binary pprof format
// instead.
const debugLevel = 1

// forcedExitCode replaces a zero exit code when the leak check itself is what
// failed: returning 0 in that case would report the run clean when it was not.
const forcedExitCode = 1

// Runner is the subset of *testing.M that Main needs. *testing.M satisfies it;
// the interface is what lets Main's own tests drive it with a fake.
type Runner interface {
	// Run runs the test binary's tests and returns the process exit code
	// testing.M itself would return.
	Run() int
}

// Main runs m and fails the process if doing so leaves a goroutine behind. A
// package whose tests start a goroutine calls it from a TestMain in place of
// running m.Run() directly:
//
//	func TestMain(m *testing.M) { leakcheck.Main(m) }
//
// Main's body is exactly one statement so that its coverage counter is
// recorded the instant it is entered — before m.Run() runs to completion and
// writes the coverage profile, which is otherwise a trap: anything a test
// binary executes only after m.Run() returns is invisible to that profile, and
// the gate this repo runs treats one uncovered statement as a failure.
func Main(m Runner) {
	os.Exit(Verdict(m.Run(), os.Stderr))
}

// Verdict turns code — m.Run()'s own exit code — into the leak-checked exit
// code: unchanged when the run left no goroutine behind, non-zero otherwise. A
// leak's stacks and a one-line summary are written to w.
func Verdict(code int, w io.Writer) int {
	return verdict(code, pprofLookup, w)
}

// profile is the subset of *pprof.Profile that verdict reads. A small
// consumer-defined interface stands in for *pprof.Profile itself because
// nothing outside runtime/pprof can construct one carrying a chosen sample
// count, which is what verdict's own tests need in order to drive every
// branch without an actual leaked goroutine.
type profile interface {
	// Count reports how many stacks the profile currently holds.
	Count() int
	// WriteTo writes the profile to w. For the goroutineleak profile
	// specifically, the first call in the process also runs the
	// leak-detecting GC cycle that makes a subsequent Count accurate.
	WriteTo(w io.Writer, debug int) error
}

// pprofLookup adapts pprof.Lookup to the profile interface, returning a
// genuinely nil profile for a name nothing has registered.
//
// Returning pprof.Lookup's result directly would instead hand back a non-nil
// profile interface wrapping a nil *pprof.Profile — a classic Go trap — and
// verdict's own "p == nil" check cannot see through that: it would go on to
// call WriteTo on a nil receiver instead of failing loudly.
func pprofLookup(name string) profile {
	p := pprof.Lookup(name)
	if p == nil {
		return nil
	}
	return p
}

// verdict decides the leak-checked exit code. lookup and w are parameters,
// rather than pprof.Lookup and os.Stderr read directly, so the unit tests can
// drive every branch — profile absent, a write failure, zero leaks, and one or
// more leaks — without an actual leaked goroutine.
func verdict(code int, lookup func(string) profile, w io.Writer) int {
	p := lookup(goroutineLeakProfile)
	if p == nil {
		_, _ = fmt.Fprintf(w, "leakcheck: %q profile not registered (needs Go 1.27+); "+
			"failing because the leak check itself could not run\n", goroutineLeakProfile)
		return forcedCode(code)
	}

	// WriteTo runs the leak-detecting GC cycle as a side effect, which is what
	// makes the Count below accurate. It is buffered rather than written
	// straight to w so that a clean run — the common case — prints nothing.
	var stacks bytes.Buffer
	if err := p.WriteTo(&stacks, debugLevel); err != nil {
		_, _ = fmt.Fprintf(w, "leakcheck: writing %q profile: %v; "+
			"failing because the leak check itself could not run\n", goroutineLeakProfile, err)
		return forcedCode(code)
	}

	if n := p.Count(); n > 0 {
		_, _ = fmt.Fprintf(w, "leakcheck: %d leaked goroutine(s)\n", n)
		_, _ = w.Write(stacks.Bytes())
		return forcedCode(code)
	}
	return code
}

// forcedCode returns code unchanged when it already reports failure, and
// forcedExitCode otherwise. The leak check must never turn a real failure back
// into a pass by returning 0, and must never report a leak as one either.
func forcedCode(code int) int {
	if code != 0 {
		return code
	}
	return forcedExitCode
}
