package leakcheck

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeProfile is a profile whose Count and WriteTo behavior are both fixed by
// the test, which is what lets verdict's branches be driven without an actual
// leaked goroutine.
type fakeProfile struct {
	count    int
	written  string
	writeErr error
}

func (f *fakeProfile) Count() int { return f.count }

func (f *fakeProfile) WriteTo(w io.Writer, _ int) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	_, err := io.WriteString(w, f.written)
	return err
}

// lookupFake returns a lookup function that hands back p regardless of the
// name asked for.
func lookupFake(p profile) func(string) profile {
	return func(string) profile { return p }
}

func TestVerdict_NilProfileFailsLoudlyRegardlessOfCode(t *testing.T) {
	t.Parallel()
	tests := map[string]int{"a passing run": 0, "an already-failing run": 3}
	for name, code := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			got := verdict(code, lookupFake(nil), &out)
			assert.NotZero(t, got, "an absent profile must fail loudly, never report green")
			assert.Contains(t, out.String(), "not registered")
		})
	}
}

func TestVerdict_WriteToErrorFailsLoudly(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	wantErr := errors.New("boom")
	got := verdict(0, lookupFake(&fakeProfile{writeErr: wantErr}), &out)
	assert.NotZero(t, got, "a profile that cannot be written must fail loudly")
	assert.Contains(t, out.String(), "boom")
}

func TestVerdict_ZeroLeaksReturnsCodeUnchangedAndPrintsNothing(t *testing.T) {
	t.Parallel()
	tests := map[string]int{"a passing run": 0, "an already-failing run": 5}
	for name, code := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			got := verdict(code, lookupFake(&fakeProfile{count: 0}), &out)
			assert.Equal(t, code, got)
			assert.Empty(t, out.String(), "a clean run must print nothing")
		})
	}
}

func TestVerdict_LeaksArePrintedAndForceNonZero(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code int
		want int
	}{
		{name: "a passing run", code: 0, want: forcedExitCode},
		{name: "an already-failing run", code: 9, want: 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			p := &fakeProfile{count: 3, written: "STACKMARKER\n"}
			got := verdict(tc.code, lookupFake(p), &out)
			assert.Equal(t, tc.want, got)
			assert.Contains(t, out.String(), "3", "the summary must name the count")
			assert.Contains(t, out.String(), "STACKMARKER", "the stacks WriteTo wrote must be printed")
		})
	}
}

func TestForcedCode_ReturnsCodeUnlessZero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, forcedExitCode, forcedCode(0))
	assert.Equal(t, 7, forcedCode(7))
}

func TestPprofLookup_RegisteredProfileIsNonNil(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, pprofLookup(goroutineLeakProfile))
}

// TestPprofLookup_UnregisteredNameIsGenuinelyNilInterface guards the typed-nil
// trap pprofLookup's doc comment describes.
func TestPprofLookup_UnregisteredNameIsGenuinelyNilInterface(t *testing.T) {
	t.Parallel()
	got := pprofLookup("leakcheck-test-name-nothing-registers")
	// A plain == nil, not assert.Nil: testify's Nil reflects through the
	// interface and would report this as nil even if pprofLookup regressed to
	// "return pprof.Lookup(name)" — a *pprof.Profile(nil) wrapped in a non-nil
	// profile interface, which is exactly the trap this test exists to catch.
	assert.True(t, got == nil, "an unregistered name must yield a nil interface, not a nil *pprof.Profile in one")
}

// TestVerdict_DirectCallCoversTheExportedWrapper calls Verdict itself, rather
// than only through Main, so this statement's coverage is recorded during
// m.Run() — before Main's own call to Verdict runs, which happens after
// m.Run() has already written the coverage profile.
func TestVerdict_DirectCallCoversTheExportedWrapper(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	got := Verdict(0, &out)
	assert.Equal(t, 0, got, "this test process itself has not leaked a goroutine")
}
