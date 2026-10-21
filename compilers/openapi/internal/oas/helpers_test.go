package oas_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// The codes the engine reports under.
const (
	required  = diag.Validation + "/validation-required-field"
	mismatch  = diag.Validation + "/validation-type-mismatch"
	allowed   = diag.Validation + "/validation-allowed-values"
	badFormat = diag.Validation + "/validation-invalid-format"
	badSyntax = diag.Validation + "/validation-invalid-syntax"
	exclusive = diag.Validation + "/validation-mutually-exclusive-fields"
	empty     = diag.Validation + "/validation-empty-value"
	duplicate = diag.Validation + "/validation-duplicate-key"
	dangling  = diag.Validation + "/validation-invalid-reference"
	entries   = diag.Validation + "/validation-entry-count"
	forbidden = diag.Validation + "/validation-forbidden-field"
	later     = diag.LaterVersionConstruct
	noEffect  = diag.InapplicableLocationKeyword
)

// finding is what a test compares of a diagnostic: its code and where it sits.
type finding struct{ Code, At string }

// at returns the finding of code at the pointer.
func at(code, pointer string) finding { return finding{Code: code, At: pointer} }

// run checks doc as an object of the kind in a document declaring version, and
// returns the diagnostics.
func run(t *testing.T, version string, kind oas.Kind, doc string) []ir.Diagnostic {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &n))
	return oas.NewChecker().Check(&n, version, oas.Options{
		Start:  kind,
		Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
	})
}

// findings returns what run finds, ordered by position then code.
func findings(t *testing.T, version string, kind oas.Kind, doc string) []finding {
	t.Helper()
	diags := run(t, version, kind, doc)
	out := make([]finding, 0, len(diags))
	for _, d := range diags {
		out = append(out, finding{Code: d.Code, At: string(d.Provenance.Pointer)})
	}
	sortFindings(out)
	return out
}

// sortFindings orders findings by pointer, then code.
func sortFindings(fs []finding) {
	slices.SortFunc(fs, func(a, b finding) int {
		return strings.Compare(a.At+"|"+a.Code, b.At+"|"+b.Code)
	})
}

// scenario is one document and what checking it finds.
type scenario struct {
	name    string
	version string
	kind    oas.Kind
	doc     string
	want    []finding
}

// verify runs the scenarios, each compared with its findings in full, so an
// extra finding fails as a missing one does.
func verify(t *testing.T, scenarios []scenario) {
	t.Helper()
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			want := slices.Clone(sc.want)
			sortFindings(want)
			assert.Empty(t, cmp.Diff(want, findings(t, sc.version, sc.kind, sc.doc), cmpopts.EquateEmpty()))
		})
	}
}
