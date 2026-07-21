package openapi_test // external test package — exercises only the public API

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// danglingDir holds the twelve issue-#14 reproducers, copied out of triage so the
// tests are self-contained.
const danglingDir = "../../testdata/dangling/openapi"

// danglingRefs returns a sorted, human-readable list of every reference in doc
// that resolves to nothing — a TypeRef, discriminator, or value ref whose TypeID
// is absent from doc.Types, or a SchemeUse whose AuthID is absent from doc.Auth.
// It delegates to irverify.Verify, the shared reflection-based structural checker
// (ir/irverify/refs.go) that the compiler fuzzer already runs on every document,
// and keeps only its dangling-reference violations. Reusing that one walker is
// what lets this test track the IR's ID surface automatically as new ID-bearing
// fields land, instead of a bespoke traversal that would silently miss them. An
// empty result means the IR is referentially closed — the property issue #14
// restores.
func danglingRefs(doc *ir.Document) []string {
	var out []string
	for _, v := range irverify.Verify(doc) {
		if !strings.HasPrefix(v.Code, "ir/dangling-") {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", v.Path, v.Message))
	}
	sort.Strings(out)
	return out
}

// compileFile compiles one spec file through the public compiler, using srcPath
// as the source path (which drives same-file $ref resolution).
func compileFile(t *testing.T, dir, file, srcPath string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, file))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: srcPath, Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// hasErrorRef reports whether the diagnostics carry an error-severity
// unresolved-ref finding — the signal that an offending entry was dropped.
func hasErrorRef(diags []ir.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == ir.SeverityError && d.Code == "openapi/unresolved-ref" {
			return true
		}
	}
	return false
}

func hasAnyError(diags []ir.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			return true
		}
	}
	return false
}

// outcome classifies how a reproducer must be made referentially sound.
type outcome int

const (
	// drops the offending entry with an error-severity unresolved-ref diagnostic.
	drops outcome = iota
	// interns the target so the reference resolves, with no error diagnostic.
	interns
	// resolves internally but the loader still reports a problem it could not
	// avoid — an external file it cannot fetch (f12) or a malformed pointer it must
	// flag (f32): only referential closure is asserted.
	internsNoisy
)

// TestDanglingRefs_Reproducers compiles each issue-#14 reproducer and asserts the
// produced IR has zero dangling references — every offending entry either interns
// correctly or is dropped with an error-severity diagnostic.
func TestDanglingRefs_Reproducers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file    string
		srcPath string
		want    outcome
	}{
		{"f04-composition.yaml", "f04.yaml", drops},
		{"f05-discriminator.yaml", "f05.yaml", drops},
		{"f06-discriminator.yaml", "f06.yaml", drops},
		{"f07-discriminator.yaml", "f07.yaml", interns},
		{"f08-discriminator.yaml", "f08.yaml", drops},
		{"f09-discriminator.yaml", "f09.yaml", drops},
		{"f10-refs.yaml", "f10.yaml", interns},
		{"f11-refs.yaml", "f11.yaml", interns},
		{"f12-refs.yaml", "m.yaml", internsNoisy},
		{"f13-refs.yaml", "f13.yaml", drops},
		{"f28-maps-additional.yaml", "f28.yaml", interns},
		{"f30-protocol-surface.yaml", "f30.yaml", drops},
		{"f31-discriminator-empty-name.yaml", "f31.yaml", interns},
		{"f32-ref-noncanonical-escape.yaml", "f32.yaml", internsNoisy},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileFile(t, danglingDir, tc.file, tc.srcPath)
			assert.Empty(t, danglingRefs(doc), "IR must be referentially closed")
			switch tc.want {
			case drops:
				assert.True(t, hasErrorRef(diags),
					"a dropped entry must leave an error-severity unresolved-ref diagnostic")
			case interns:
				assert.False(t, hasAnyError(diags),
					"an interned reference must not raise an error diagnostic")
			case internsNoisy:
				// f12 resolves the same-file ref internally; the loader still reports
				// the external m.yaml it could not open. Only closure is asserted.
			}
		})
	}
}

// TestDanglingRefs_f07 pins the positive case: a mapping value that contains '/'
// but names an existing schema resolves to it rather than dangling.
func TestDanglingRefs_f07(t *testing.T) {
	t.Parallel()
	doc, diags := compileFile(t, danglingDir, "f07-discriminator.yaml", "f07.yaml")
	assert.False(t, hasAnyError(diags))
	pet, ok := doc.Types[namedID("Pet")].(*ir.Union)
	require.True(t, ok)
	require.NotNil(t, pet.Discriminator)
	target, ok := pet.Discriminator.Mapping["x"]
	require.True(t, ok, "the mapping entry survives")
	// The schema is literally named "A/B"; its pointer segment is RFC 6901-escaped.
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/A~1B"), target,
		"resolves to the existing schema named A/B")
	_, ok = doc.Types[target]
	assert.True(t, ok, "the resolved target is present in the registry")
}

// TestDanglingRefs_f30 pins the auth case: a security requirement naming an
// undeclared scheme is dropped and diagnosed, never written as a dangling AuthID.
func TestDanglingRefs_f30(t *testing.T) {
	t.Parallel()
	doc, diags := compileFile(t, danglingDir, "f30-protocol-surface.yaml", "f30.yaml")
	assert.Empty(t, danglingRefs(doc))
	assert.Empty(t, doc.Auth, "no scheme is declared")
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Auth, 1, "the requirement option is kept")
	assert.Empty(t, doc.Services[0].Auth[0].Schemes, "its undeclared scheme is dropped")
	assert.True(t, hasErrorRef(diags), "the drop is diagnosed")
}

// TestDanglingRefs_f10 pins that a $ref to a component sub-schema interns the
// sub-schema at its pointer-derived ID so the reference resolves.
func TestDanglingRefs_f10(t *testing.T) {
	t.Parallel()
	doc, _ := compileFile(t, danglingDir, "f10-refs.yaml", "f10.yaml")
	foo, ok := doc.Types[namedID("Foo")].(*ir.Model)
	require.True(t, ok)
	x, ok := propByWire(foo, "x")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Foo/properties/bar"), x.Type.Target)
	_, ok = doc.Types[x.Type.Target]
	assert.True(t, ok, "the referenced sub-schema is interned under its pointer-derived ID")
}

// TestDanglingRefs_Corpus runs the oracle over the whole conformance corpus and
// the petstore golden so a future change cannot reintroduce a dangling reference.
func TestDanglingRefs_Corpus(t *testing.T) {
	t.Parallel()
	specs, err := filepath.Glob(filepath.Join(conformanceDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, specs)
	specs = append(specs, "../../testdata/golden/openapi/petstore.yaml")
	for _, spec := range specs {
		t.Run(filepath.Base(spec), func(t *testing.T) {
			t.Parallel()
			base := filepath.Base(spec)
			doc, _ := compileFile(t, filepath.Dir(spec), base, base)
			assert.Empty(t, danglingRefs(doc), "corpus spec %s must be referentially closed", base)
		})
	}
}
