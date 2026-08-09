// Package irtest provides golden-snapshot helpers for IR documents.
package irtest

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/ir"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing")

// Update reports whether -update was passed to go test.
func Update() bool { return *update }

// encodeGolden serializes doc as deterministic indented JSON with a trailing
// newline. Both WriteGolden and compareGolden encode through this one
// function, so the golden writer and the golden comparer cannot disagree
// about what "the golden encoding" is. Both screen out a nil doc first, so one
// never reaches here.
func encodeGolden(doc *ir.Document) ([]byte, error) {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// WriteGolden serializes doc deterministically and writes it to path, creating
// parent directories as needed. An empty path and a nil document are both
// caller errors and are refused before anything is written.
//
// A nil document is worth refusing rather than encoding: it marshals to the
// four bytes "null", which every later run with a nil document matches, so a
// golden written from one asserts nothing while looking like a snapshot.
func WriteGolden(path string, doc *ir.Document) error {
	if path == "" {
		return errors.New("irtest: write golden: empty path")
	}
	if doc == nil {
		return errors.New("irtest: write golden: nil document")
	}
	raw, err := encodeGolden(doc)
	if err != nil {
		return fmt.Errorf("irtest: marshal golden %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("irtest: mkdir for golden %s: %w", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("irtest: write golden %s: %w", path, err)
	}
	return nil
}

// testingT is the subset of *testing.T that compareGolden relies on. It exists
// so the abort/failure branches can be exercised with a recording stub instead
// of aborting a real test.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

// CompareGolden compares doc against the golden file at goldenPath, or
// rewrites it when -update is set. Failures include a full diff. An empty
// goldenPath and a nil document each fail the test immediately: this is a test
// helper, so refusing loudly is the only channel it has.
func CompareGolden(t *testing.T, goldenPath string, doc *ir.Document) {
	t.Helper()
	compareGolden(t, goldenPath, doc)
}

// compareGolden holds the comparison logic against the minimal testingT surface.
// The preconditions precede the -update branch, so a bad call is refused the
// same way whether the golden is being compared or rewritten.
func compareGolden(t testingT, goldenPath string, doc *ir.Document) {
	t.Helper()
	if goldenPath == "" {
		t.Fatalf("irtest: compare golden: empty golden path")
	}
	if doc == nil {
		t.Fatalf("irtest: compare golden: nil document")
	}
	if Update() {
		if err := WriteGolden(goldenPath, doc); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", goldenPath, err)
	}
	raw, err := encodeGolden(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	if diff := cmp.Diff(string(want), string(raw)); diff != "" {
		t.Errorf("golden mismatch for %s (-golden +got):\n%s", goldenPath, diff)
	}
}
