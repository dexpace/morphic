// This file is a package-level suite, not a per-source-file test: it measures the
// whole committed corpus against the whole IR type graph, so it pairs with no
// single source file.
package openapi_test // external test package — exercises only the public API

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// unwitnessedGolden snapshots the IR fields no committed spec makes non-zero.
const unwitnessedGolden = conformanceDir + "/unwitnessed.golden.txt"

// corpusRoot is every committed spec directory, addressed relative to this file.
const corpusRoot = "../../testdata"

// maxValueDepth bounds the value walk (the bounded-recursion rule). IR documents
// nest an order of magnitude shallower than this; exceeding it means the walk
// found a cycle, which a Document is not allowed to contain.
const maxValueDepth = 128

// TestConformance_UnwitnessedIRFields snapshots every ir (struct, field) pair
// that no committed spec drives to a non-zero value. It is the corpus's own
// coverage report: a case that reaches a new field removes a line, a new IR field
// adds one, and a compiler that stops writing a field it used to write adds one
// too. Regenerate with `go test ./compilers/openapi -run TestConformance -update`.
//
// Derived, never maintained, is the whole point: -update recomputes the file from
// the corpus, so it cannot drift the way a hand-kept allowlist does.
//
// Two weaknesses are deliberate. It does not classify, so a removed line does not
// say whether a corpus case landed or an IR field was deleted — the diff shows
// which, the file does not. And IsZero cannot see an assignment of a zero value,
// so a bool field written false, or an int index written 0, stays listed however
// many times the compiler writes it. FileInfo.IsText is the standing case of
// that: the compiler's only construction site writes it false, so no spec can
// ever remove its line — it is listed permanently, not uncovered.
func TestConformance_UnwitnessedIRFields(t *testing.T) {
	t.Parallel()
	universe := irFieldUniverse(t)
	witnessed := witnessedFields(t)
	require.NotEmpty(t, universe, "the static walk must reach ir fields")
	require.NotEmpty(t, witnessed, "the corpus walk must witness ir fields")

	unwitnessed := make([]string, 0, len(universe))
	for field := range universe {
		if !witnessed[field] {
			unwitnessed = append(unwitnessed, field)
		}
	}
	slices.Sort(unwitnessed)
	for field := range witnessed {
		assert.Contains(t, universe, field,
			"the corpus walk witnessed %s, which the static walk never reached: "+
				"the two walks disagree about how a field is named", field)
	}
	compareTextGolden(t, unwitnessedGolden, strings.Join(unwitnessed, "\n")+"\n")
}

// compareTextGolden compares got against the golden file at path, or rewrites it
// when -update is set — irtest.CompareGolden's contract for a non-document
// artifact, sharing its flag so one -update run refreshes every golden.
func compareTextGolden(t *testing.T, path, got string) {
	t.Helper()
	if irtest.Update() {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "read golden %s (run with -update to create)", path)
	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("golden mismatch for %s (-golden +got):\n%s", path, diff)
	}
}

// irFieldUniverse returns every exported (owner struct, field) pair reachable
// from ir.Document, keyed "Owner.Field". The seen set bounds the walk: each
// distinct reflect.Type is visited once, so recursive shapes terminate.
func irFieldUniverse(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	seen := map[reflect.Type]bool{}
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		if rt == nil || seen[rt] {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem())
		case reflect.Map:
			walk(rt.Key())
			walk(rt.Elem())
		case reflect.Struct:
			for f := range rt.Fields() {
				f := f
				if f.PkgPath != "" {
					continue // unexported: no consumer can read it
				}
				out[rt.Name()+"."+f.Name] = true
				walk(f.Type)
			}
		}
	}

	walk(reflect.TypeFor[ir.Document]())
	// Sealed TypeDef kinds are reached only through the interface, which
	// reflection on the static type graph cannot enumerate. Seed each concrete
	// kind from the kinds the ir sources declare, so a new variant joins the
	// universe the day it is added rather than the day someone extends a list.
	for _, k := range sealedTypeKinds(t) {
		td, ok := ir.NewTypeDef(k)
		require.True(t, ok, "no concrete type registered for kind %q", k)
		rt := reflect.TypeOf(td)
		require.Equal(t, reflect.Pointer, rt.Kind(), "NewTypeDef must return a pointer for %q", k)
		walk(rt.Elem())
	}
	return out
}

// witnessedFields returns the "Owner.Field" pairs some committed spec drives to a
// non-zero value.
func witnessedFields(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, path := range corpusSpecs(t) {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		doc, _, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Base(path), Data: data}}, compilers.Options{})
		if err != nil || doc == nil {
			continue // the compiler declines cyclic and amplifying fixtures outright
		}
		markWitnessed(t, out, reflect.ValueOf(*doc), 0)
	}
	return out
}

// markWitnessed records every non-zero exported field reachable from v.
func markWitnessed(t *testing.T, out map[string]bool, v reflect.Value, depth int) {
	t.Helper()
	require.True(t, v.IsValid(), "the walk reached an invalid value at depth %d", depth)
	require.Less(t, depth, maxValueDepth, "value walk exceeded its depth cap at %s", v.Type())
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			markWitnessed(t, out, v.Elem(), depth+1)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			markWitnessed(t, out, v.Index(i), depth+1)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			markWitnessed(t, out, k, depth+1)
			markWitnessed(t, out, v.MapIndex(k), depth+1)
		}
	case reflect.Struct:
		markStructFields(t, out, v, depth)
	}
}

// markStructFields records v's own non-zero fields and descends into each.
func markStructFields(t *testing.T, out map[string]bool, v reflect.Value, depth int) {
	t.Helper()
	rt := v.Type()
	for i := range rt.NumField() {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		field := v.Field(i)
		if !field.IsZero() {
			out[rt.Name()+"."+f.Name] = true
		}
		markWitnessed(t, out, field, depth+1)
	}
}

// corpusSpecs lists every committed spec under testdata, excluding golden
// snapshots, in WalkDir's lexical order.
func corpusSpecs(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.FromSlash(corpusRoot), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".golden.json") {
			return err
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			out = append(out, p)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out, "corpus sweep found no specs")
	return out
}

// sealedTypeKinds returns every ir.TypeKind constant the ir package's production
// sources declare — the sealed sum's full membership, since ir.TypeDef admits no
// implementation from outside that package.
func sealedTypeKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	var out []ir.TypeKind
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.CONST {
				continue
			}
			out = append(out, typeKindValues(t, gd)...)
		}
	}
	require.NotEmpty(t, out, "the ir sources must declare TypeKind constants")
	return out
}

// typeKindValues returns one const group's TypeKind values. A spec declaring
// neither type nor value repeats the previous spec, so the group's last explicit
// type carries forward; a spec with a value of its own declares its own type.
func typeKindValues(t *testing.T, gd *ast.GenDecl) []ir.TypeKind {
	t.Helper()
	var out []ir.TypeKind
	isKind := false
	for _, spec := range gd.Specs {
		vs, isValue := spec.(*ast.ValueSpec)
		require.True(t, isValue, "const spec is not a ValueSpec: %#v", spec)
		switch {
		case vs.Type != nil:
			id, isIdent := vs.Type.(*ast.Ident)
			isKind = isIdent && id.Name == "TypeKind"
		case len(vs.Values) > 0:
			isKind = false
		}
		if !isKind {
			continue
		}
		out = append(out, stringLiteralValues(t, vs)...)
	}
	return out
}

// stringLiteralValues returns a value spec's string-literal constant values.
func stringLiteralValues(t *testing.T, vs *ast.ValueSpec) []ir.TypeKind {
	t.Helper()
	out := make([]ir.TypeKind, 0, len(vs.Names))
	for i, name := range vs.Names {
		require.Less(t, i, len(vs.Values), "TypeKind constant %s must declare its own value", name.Name)
		lit, isLit := vs.Values[i].(*ast.BasicLit)
		require.True(t, isLit && lit.Kind == token.STRING,
			"TypeKind constant %s must be a string literal", name.Name)
		unquoted, err := strconv.Unquote(lit.Value)
		require.NoError(t, err, "unquoting the value of %s", name.Name)
		out = append(out, ir.TypeKind(unquoted))
	}
	return out
}

// irSourceFiles lists the ir package's non-test Go files, located relative to
// this test's own path so the result does not depend on the working directory.
func irSourceFiles(t *testing.T) []string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this test's path")
	dir := filepath.Join(filepath.Dir(self), "..", "..", "ir")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	require.NotEmpty(t, out, "the ir package must have production sources")
	return out
}
