package openapi_test // external test package — exercises only the public API

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// goldenPetstore is the larger real-ish spec seeded alongside the conformance
// corpus, addressed relative to this test file.
const goldenPetstore = "../../testdata/golden/openapi/petstore.yaml"

// FuzzCompile hammers the OpenAPI compiler with mutated spec bytes and asserts the
// structural oracles on every document it produces: a successful compile must
// verify clean under irverify and survive a serialized-JSON round-trip. Malformed
// input — a Go error or a nil document — is expected and returns without failing;
// a panic is a real compiler defect the fuzzer captures on its own. Error-severity
// diagnostics are ignored, since mutated input legitimately provokes them yet must
// still yield a structurally sound document.
func FuzzCompile(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, _, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
		if err != nil || doc == nil {
			return // malformed input is not a failure
		}
		assertOracles(t, doc, data)
	})
}

// FuzzLowerSchema targets the schema lowerer specifically: each fuzzed fragment is
// embedded as the sole component schema of a minimal OpenAPI 3.1 document, which is
// then compiled and checked against the same structural oracles. A fragment that is
// not valid JSON cannot be embedded and is skipped; an embeddable fragment the
// compiler rejects returns without failing.
func FuzzLowerSchema(f *testing.F) {
	for _, s := range schemaSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, fragment []byte) {
		spec, ok := embedSchema(fragment)
		if !ok {
			return // fragment is not valid JSON; nothing to embed
		}
		doc, _, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz-schema.json", Data: spec}}, compilers.Options{})
		if err != nil || doc == nil {
			return
		}
		assertOracles(t, doc, spec)
	})
}

// assertOracles applies the structural oracles to a successfully compiled
// document: irverify must report no violations and the document must round-trip
// through JSON byte-for-byte. Each failure is reported with the triggering input so
// the fuzzer's persisted reproducer is self-describing.
func assertOracles(t *testing.T, doc *ir.Document, input []byte) {
	t.Helper()
	if vs := irverify.Verify(doc); len(vs) > 0 {
		t.Errorf("irverify reported %d violation(s) on input %q: %+v", len(vs), input, vs)
	}
	if err := roundTrip(doc); err != nil {
		t.Errorf("round-trip mismatch on input %q: %v", input, err)
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

// seedCorpus adds every committed OpenAPI spec — the full conformance corpus plus
// the larger golden petstore — to the fuzz corpus so mutation starts from valid,
// feature-dense documents rather than from empty input.
func seedCorpus(f *testing.F) {
	f.Helper()
	entries, err := os.ReadDir(conformanceDir)
	require.NoError(f, err)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(conformanceDir, e.Name()))
		require.NoError(f, err)
		f.Add(data)
	}
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(f, err)
	f.Add(data)
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
