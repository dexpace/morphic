// Package irtest provides golden-snapshot helpers for IR documents.
package irtest

import (
	"encoding/json"
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
// about what "the golden encoding" is.
func encodeGolden(doc *ir.Document) ([]byte, error) {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// WriteGolden serializes doc deterministically and writes it to path, creating
// parent directories as needed.
func WriteGolden(path string, doc *ir.Document) error {
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
// rewrites it when -update is set. Failures include a full diff.
func CompareGolden(t *testing.T, goldenPath string, doc *ir.Document) {
	t.Helper()
	compareGolden(t, goldenPath, doc)
}

// compareGolden holds the comparison logic against the minimal testingT surface.
func compareGolden(t testingT, goldenPath string, doc *ir.Document) {
	t.Helper()
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
