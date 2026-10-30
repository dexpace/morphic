package irtest

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/ir"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing")

// Update reports whether -update was passed to go test.
func Update() bool { return *update }

// encodeGolden serializes doc as a golden stores it: deterministic indented
// JSON with a trailing newline, without the irVersion member. Both WriteGolden
// and compareGolden encode through this one function, so the golden writer and
// the golden comparer cannot disagree about what "the golden encoding" is.
// Both screen out a nil or foreign-stamped doc first, so one never reaches
// here.
func encodeGolden(doc *ir.Document) ([]byte, error) {
	unstamped := *doc
	unstamped.IRVersion = ""
	raw, err := json.Marshal(&unstamped, jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// stampError reports a document a golden cannot stand for: one not stamped
// with ir.IRVersion. A golden leaves the stamp out, so this check is what
// still asserts it.
func stampError(doc *ir.Document) error {
	if doc.IRVersion != ir.IRVersion {
		return fmt.Errorf("document stamped irVersion %q, not ir.IRVersion %s", doc.IRVersion, ir.IRVersion)
	}
	return nil
}

// WriteGolden serializes doc deterministically, without its irVersion, and
// writes it to path, creating parent directories as needed. An empty path, a
// nil document and one not stamped with ir.IRVersion are caller errors,
// refused before anything is written.
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
	if err := stampError(doc); err != nil {
		return fmt.Errorf("irtest: write golden %s: %w", path, err)
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
// goldenPath, a nil document and one not stamped with ir.IRVersion each fail
// the test immediately: this is a test helper, so refusing loudly is the only
// channel it has.
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
	if err := stampError(doc); err != nil {
		t.Fatalf("irtest: compare golden %s: %v", goldenPath, err)
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

// ReadGolden decodes the golden at path into the document it stands for,
// stamped with ir.IRVersion, which the golden leaves out. The Document decoder
// reads the result, so a golden that stores a stamp of its own is refused as
// a duplicate member, like anything else that decoder refuses.
func ReadGolden(path string) (*ir.Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("irtest: read golden: %w", err)
	}
	body := bytes.TrimLeft(raw, jsonSpace)
	if len(body) == 0 || body[0] != '{' {
		return nil, fmt.Errorf("irtest: read golden %s: not a JSON object", path)
	}
	members := body[1:]
	sep := ","
	if rest := bytes.TrimLeft(members, jsonSpace); len(rest) > 0 && rest[0] == '}' {
		sep = ""
	}

	// The version is MAJOR.MINOR.PATCH, so Go's quoting of it is JSON's.
	stamped := slices.Concat([]byte(`{"irVersion":`+strconv.Quote(ir.IRVersion)+sep), members)
	var doc ir.Document
	if err := json.Unmarshal(stamped, &doc); err != nil {
		return nil, fmt.Errorf("irtest: read golden %s: %w", path, err)
	}
	return &doc, nil
}

// jsonSpace is the whitespace JSON allows between tokens (RFC 8259 §2).
const jsonSpace = " \t\n\r"
