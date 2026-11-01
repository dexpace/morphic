// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it runs the
// issue-#14 dangling-ref reproducer corpus through the whole compiler, so it has
// no single source file to pair with. This is deliberate, not a leftover from the
// test-file consolidation — do not fold it into another file.
package e2e_test // external test package — exercises only the public API

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// danglingDir holds the issue-#14 reproducers, copied out of triage so the tests
// are self-contained. danglingReproducers is the enumeration, and
// TestDanglingRefs_EveryReproducerIsExercised holds it to the directory.
const danglingDir = "../../../testdata/dangling/openapi"

// danglingRefs returns a sorted, human-readable list of every dangling reference
// in doc — a TypeRef, discriminator, or value ref whose TypeID is absent from
// doc.Types, or a SchemeUse whose AuthID is absent from doc.Auth — by filtering
// irverify.Verify's output to violations coded "ir/dangling-". Reusing that
// shared walker (ir/irverify/refs.go) means new ID-bearing IR fields are covered
// automatically, rather than by a bespoke traversal that could miss them. An
// empty result is the referential-closure property issue #14 restores.
func danglingRefs(doc *ir.Document) []string {
	var out []string
	for _, v := range irverify.Verify(doc) {
		if !strings.HasPrefix(v.Code, "ir/dangling-") {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", v.Path, v.Message))
	}
	slices.Sort(out)
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
	// mints a node of its own for the target, so the reference resolves to
	// something that says it has no mechanism, with an error-severity diagnostic.
	mints
)

// danglingCase is one reproducer: the fixture under danglingDir, the source path
// it is compiled as, and what the compiler is expected to do with it.
type danglingCase struct {
	file    string
	srcPath string
	want    outcome
}

// danglingReproducers enumerates the fixtures under danglingDir.
// TestDanglingRefs_EveryReproducerIsExercised fails when the directory holds one
// this list does not, so a fixture added to the corpus cannot sit unexercised.
func danglingReproducers() []danglingCase {
	return []danglingCase{
		{"f04-composition.yaml", "f04.yaml", drops},
		{"f05-discriminator.yaml", "f05.yaml", drops},
		{"f06-discriminator.yaml", "f06.yaml", drops},
		{"f07-discriminator.yaml", "f07.yaml", interns},
		{"f08-discriminator.yaml", "f08.yaml", interns},
		{"f09-discriminator.yaml", "f09.yaml", drops},
		{"f10-refs.yaml", "f10.yaml", interns},
		{"f11-refs.yaml", "f11.yaml", interns},
		{"f12-refs.yaml", "m.yaml", internsNoisy},
		{"f13-refs.yaml", "f13.yaml", interns},
		{"f28-maps-additional.yaml", "f28.yaml", interns},
		{"f30-protocol-surface.yaml", "f30.yaml", mints},
		{"f31-discriminator-empty-name.yaml", "f31.yaml", interns},
		{"f32-ref-noncanonical-escape.yaml", "f32.yaml", internsNoisy},
	}
}

// TestDanglingRefs_EveryReproducerIsExercised holds danglingReproducers to the
// directory it enumerates. A fixture copied into danglingDir and not added to the
// table would otherwise be compiled by nothing, and the suite would stay green
// while the corpus grew past it — the drift a hand-maintained list invites.
func TestDanglingRefs_EveryReproducerIsExercised(t *testing.T) {
	t.Parallel()
	onDisk, err := filepath.Glob(filepath.Join(danglingDir, "*.yaml"))
	require.NoError(t, err, "globbing the reproducer directory")
	require.NotEmpty(t, onDisk, "the reproducer directory is not empty")

	table := danglingReproducers()
	listed := make(map[string]bool, len(table))
	for _, tc := range table {
		listed[tc.file] = true
	}
	for _, path := range onDisk {
		assert.True(t, listed[filepath.Base(path)],
			"%s is in %s but not in danglingReproducers", filepath.Base(path), danglingDir)
	}
	// Neutral about which side is short, because it catches both: a row naming no
	// fixture is what it is here for, and it fires again beside the check above
	// when the directory is the side holding the extra.
	assert.Len(t, table, len(onDisk),
		"danglingReproducers and %s hold different numbers of fixtures", danglingDir)
}

// TestDanglingRefs_Reproducers compiles each issue-#14 reproducer and asserts the
// produced IR has zero dangling references — every offending entry interns
// correctly, or takes an error-severity diagnostic and is dropped or backed by a
// node minted for it.
func TestDanglingRefs_Reproducers(t *testing.T) {
	t.Parallel()
	for _, tc := range danglingReproducers() {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileFile(t, danglingDir, tc.file, tc.srcPath)
			assert.Empty(t, danglingRefs(doc), "IR must be referentially closed")
			switch tc.want {
			case drops:
				assert.True(t, hasErrorRef(diags),
					"a dropped entry must leave an error-severity unresolved-ref diagnostic")
			case interns:
				assert.False(t, diag.HasError(diags),
					"an interned reference must not raise an error diagnostic")
			case internsNoisy:
				// f12 resolves the same-file ref internally; the loader still reports
				// the external m.yaml it could not open. Only closure is asserted.
			case mints:
				assert.True(t, hasErrorNamingNoMechanism(diags), "the minted target is reported as an error")
			}
		})
	}
}

// hasErrorNamingNoMechanism reports whether diags hold the error a requirement
// draws for a name that reaches no mechanism.
func hasErrorNamingNoMechanism(diags []ir.Diagnostic) bool {
	return slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return d.Severity == ir.SeverityError && d.Code == diag.UnresolvedSecurityScheme
	})
}

// TestDanglingRefs_f07 pins the positive case: a mapping value that contains '/'
// but names an existing schema resolves to it rather than dangling.
func TestDanglingRefs_f07(t *testing.T) {
	t.Parallel()
	doc, diags := compileFile(t, danglingDir, "f07-discriminator.yaml", "f07.yaml")
	assert.False(t, diag.HasError(diags))
	pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
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
// undeclared scheme stays and is diagnosed, its AuthID backed by a scheme minted
// for the name, never left dangling (issue #14) and never written as
// AuthRequirement{Schemes: nil}, the "no auth is one acceptable choice" encoding
// a broken requirement must not collapse into (issue #41).
func TestDanglingRefs_f30(t *testing.T) {
	t.Parallel()
	doc, diags := compileFile(t, danglingDir, "f30-protocol-surface.yaml", "f30.yaml")
	assert.Empty(t, danglingRefs(doc))
	const minted ir.AuthID = "auth/unresolved/components/securitySchemes/x"
	require.Contains(t, doc.Auth, minted, "the reference holds, to a scheme minted for the name")
	assert.Equal(t, ir.AuthKindUnresolved, doc.Auth[minted].Kind)
	require.Len(t, doc.Services, 1)
	assert.Empty(t, cmp.Diff([]ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: minted}}}}, doc.Services[0].Auth,
		cmpopts.EquateEmpty()), "the sole option stays")
	assert.True(t, hasErrorNamingNoMechanism(diags), "and the name that reached no mechanism is reported")
}

// TestDanglingRefs_f10 pins that a $ref to a component sub-schema interns the
// sub-schema at its pointer-derived ID so the reference resolves.
func TestDanglingRefs_f10(t *testing.T) {
	t.Parallel()
	doc, _ := compileFile(t, danglingDir, "f10-refs.yaml", "f10.yaml")
	foo, ok := doc.Types[componentID("Foo")].(*ir.Model)
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
	specs = append(specs, "../../../testdata/golden/openapi/petstore.yaml")
	for _, spec := range specs {
		t.Run(filepath.Base(spec), func(t *testing.T) {
			t.Parallel()
			base := filepath.Base(spec)
			doc, _ := compileFile(t, filepath.Dir(spec), base, base)
			assert.Empty(t, danglingRefs(doc), "corpus spec %s must be referentially closed", base)
		})
	}
}
