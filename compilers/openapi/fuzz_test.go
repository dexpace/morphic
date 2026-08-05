// This file is a package-level suite, not a per-source-file test: it fuzzes the
// whole compile pipeline (corpus-seeded and mutation-based) rather than any one
// source file's logic, so it has no single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// goldenPetstore is the larger real-ish spec seeded alongside the conformance
// corpus, addressed relative to this test file.
const goldenPetstore = "../../testdata/golden/openapi/petstore.yaml"

// cycleFixtureDir holds the cycle-detector reproducer and pointer-resolution
// fixtures, addressed relative to this test file.
const cycleFixtureDir = "../../testdata/openapi"

// FuzzCompile hammers the OpenAPI compiler with mutated spec bytes and asserts the
// structural oracles (irverify passes, JSON round-trips byte-identical, recompile
// is deterministic) on every document that compiles cleanly. A Go error, nil
// document, or error-severity diagnostic (e.g. a dangling $ref) marks malformed
// input or a spec-author problem rather than a compiler defect and is skipped,
// mirroring the gate internal/harness.Check applies before running its own
// oracles. A panic is always a real defect and fails the fuzz run.
func FuzzCompile(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, diags, err := compileSpec(t.Context(), "fuzz.yaml", data)
		if err != nil || doc == nil || diag.HasError(diags) {
			return // malformed input or a spec-author problem is not a compiler defect
		}
		assertOracles(t, "fuzz.yaml", data, doc)
	})
}

// FuzzLowerSchema targets the schema lowerer specifically: each fuzzed fragment is
// embedded as the sole component schema of a minimal OpenAPI 3.1 document, which is
// then compiled and checked against the same structural oracles. A fragment that is
// not valid JSON cannot be embedded and is skipped; an embeddable fragment the
// compiler rejects — a Go error, a nil document, or an error-severity diagnostic —
// returns without failing.
func FuzzLowerSchema(f *testing.F) {
	for _, s := range schemaSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, fragment []byte) {
		spec, ok := embedSchema(fragment)
		if !ok {
			return // fragment is not valid JSON; nothing to embed
		}
		doc, diags, err := compileSpec(t.Context(), "fuzz-schema.json", spec)
		if err != nil || doc == nil || diag.HasError(diags) {
			return
		}
		assertOracles(t, "fuzz-schema.json", spec, doc)
	})
}

// compileSpec compiles one in-memory source under the given logical path with
// default options — the single compile entry point the fuzz targets share so the
// determinism oracle can recompile with an identical source path.
func compileSpec(ctx context.Context, path string, data []byte) (*ir.Document, []ir.Diagnostic, error) {
	return openapi.New().Compile(ctx,
		[]compilers.Source{{Path: path, Data: data}}, compilers.Options{})
}

// assertOracles applies the structural oracles to a cleanly compiled document:
// irverify reports no violations, the document round-trips through JSON
// byte-for-byte, and a recompile of the same input yields identical IR. Each
// failure is reported with the triggering input so the fuzzer's persisted
// reproducer is self-describing.
func assertOracles(t *testing.T, path string, input []byte, doc *ir.Document) {
	t.Helper()
	if vs := irverify.Verify(doc); len(vs) > 0 {
		t.Errorf("irverify reported %d violation(s) on input %q: %+v", len(vs), input, vs)
	}
	if err := roundTrip(doc); err != nil {
		t.Errorf("round-trip mismatch on input %q: %v", input, err)
	}
	if err := recompileStable(t.Context(), path, input, doc); err != nil {
		t.Errorf("nondeterministic compile on input %q: %v", input, err)
	}
}

// roundTrip marshals doc, unmarshals it into a fresh Document, re-marshals that,
// and requires the two encodings to be byte-identical — the same serialized
// round-trip oracle internal/harness applies. Comparing JSON rather than structs
// ignores the unpreservable nil-vs-empty-collection distinction while still
// catching any real serialization loss.
func roundTrip(doc *ir.Document) error {
	first, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var back ir.Document
	if err := json.Unmarshal(first, &back); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	second, err := json.Marshal(&back)
	if err != nil {
		return fmt.Errorf("remarshal: %w", err)
	}
	if !bytes.Equal(first, second) {
		return fmt.Errorf("JSON differs:\n first: %s\nsecond: %s", first, second)
	}
	return nil
}

// recompileStable recompiles input under the same source path and requires its IR
// to marshal to the same bytes as doc, catching compile-time nondeterminism (for
// example output assembled by ranging a Go map without sorting keys) that a single
// document's round-trip cannot observe. This is the determinism oracle
// internal/harness applies as its fourth check; feeding it thousands of varied
// inputs makes the fuzzer the ideal place to surface map-ordering bugs.
func recompileStable(ctx context.Context, path string, input []byte, doc *ir.Document) error {
	first, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal first: %w", err)
	}
	again, _, err := compileSpec(ctx, path, input)
	if err != nil {
		return fmt.Errorf("recompile: %w", err)
	}
	second, err := json.Marshal(again)
	if err != nil {
		return fmt.Errorf("marshal second: %w", err)
	}
	if !bytes.Equal(first, second) {
		return errors.New("IR JSON differs across two compiles")
	}
	return nil
}

// embedSchema wraps a JSON Schema fragment as the sole component schema of a
// minimal OpenAPI 3.1 document, returning the marshaled bytes. It reports false
// when fragment is not valid JSON: json.Marshal validates the embedded
// json.RawMessage and fails, and there is nothing meaningful to compile.
func embedSchema(fragment []byte) ([]byte, bool) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "FuzzSchema", "version": "1.0.0"},
		"paths":   map[string]any{},
		"components": map[string]any{
			"schemas": map[string]any{"Fuzzed": json.RawMessage(fragment)},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, false
	}
	return b, true
}

// seedCorpus adds every committed OpenAPI spec — the conformance corpus, the
// golden petstore, and the testdata/openapi fixtures (cycle-detector
// reproducers, pointer-resolution cases, and the unreadable response keys of
// GitHub #262) — to the fuzz corpus, so mutation starts from valid,
// feature-dense documents instead of empty input. The
// degenerate cycle shapes (an alias-valued $ref, a `<<` merge key) are seeded
// too: mutating them found five variants of one crash in GitHub #26, and
// FuzzCompile already skips inputs with an error diagnostic, so seeding one
// that's expected to be refused can't fail an oracle.
func seedCorpus(f *testing.F) {
	f.Helper()
	addYAMLDir(f, conformanceDir)
	addYAMLDir(f, cycleFixtureDir)
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(f, err)
	f.Add(data)
}

// addYAMLDir adds every *.yaml file directly under dir to the fuzz corpus.
func addYAMLDir(f *testing.F, dir string) {
	f.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(f, err)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(f, err)
		f.Add(data)
	}
}

// schemaSeeds are small JSON Schema fragments that exercise distinct branches of
// the schema lowerer: scalars, composition, unions, containers, constraints,
// nullability, and validation-only keywords.
func schemaSeeds() []string {
	return []string{
		`{"type":"string"}`,
		`{"type":"integer","format":"int64"}`,
		`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
		`{"type":"array","items":{"type":"number"}}`,
		`{"allOf":[{"type":"object"},{"type":"object"}]}`,
		`{"oneOf":[{"type":"string"},{"type":"integer"}]}`,
		`{"anyOf":[{"type":"boolean"},{"type":"null"}]}`,
		`{"enum":["a","b",1,null]}`,
		`{"const":"v1"}`,
		`{"type":["string","null"]}`,
		`{"type":"object","additionalProperties":{"type":"string"}}`,
		`{"prefixItems":[{"type":"string"},{"type":"integer"}]}`,
		`{"type":"number","minimum":0,"maximum":10,"multipleOf":0.1}`,
		`{"not":{"type":"string"}}`,
	}
}
