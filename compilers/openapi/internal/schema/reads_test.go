// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/v2"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir"
)

// applicators are the keys of the census's domain whose value holds schemas,
// which a build reading the key lowers below the key's pointer.
var applicators = []string{"properties", "patternProperties", "additionalProperties", "items", "prefixItems", "enum"}

// TestUnread_KeepsWhatNoBuildRead holds the keeping of what a build did not
// read over the component schemas of every document under testdata, of the
// edges readEdges writes and of every core case, each lowered once for both
// of its checks, as lowering is nearly all their cost.
func TestUnread_KeepsWhatNoBuildRead(t *testing.T) {
	t.Parallel()
	inputs := readInputs(t)
	t.Run("no key is both read and kept", func(t *testing.T) {
		for _, in := range inputs {
			assertNoKeyIsBothReadAndKept(t, in.doc)
		}
	})
	t.Run("every key is read, handled or kept", func(t *testing.T) {
		domain := schema.CensusDomain()
		for _, in := range inputs {
			for _, js := range in.statements {
				assertEveryKeyIsReadHandledOrKept(t, js.Schema, domain)
			}
		}
	})
}

// assertNoKeyIsBothReadAndKept holds each node of doc to keeping only what no
// build read: no key holding schemas that a node keeps verbatim has a node or
// a property built from what it holds. A builder omitting a key it read would
// keep a key it typed.
func assertNoKeyIsBothReadAndKept(t *testing.T, doc *ir.Document) {
	t.Helper()
	built := map[string]bool{}
	for _, td := range doc.Types {
		built[string(td.Common().Provenance.Pointer)] = true
		if m, ok := td.(*ir.Model); ok {
			for _, p := range m.Properties {
				built[string(p.Provenance.Pointer)] = true
			}
		}
	}
	for id, td := range doc.Types {
		for _, k := range applicators {
			entry, kept := td.Common().Unmodeled["openapi:"+k]
			if !kept {
				continue
			}
			prefix := string(entry.Provenance.Pointer) + "/"
			for at := range built {
				assert.False(t, strings.HasPrefix(at, prefix), "%s keeps %s and %s was built from it", id, k, at)
			}
		}
	}
}

// assertEveryKeyIsReadHandledOrKept holds the keeping of what a build did not
// read to every key of domain, the census's, that s writes: kept unless read
// or handled, in the domain's order.
func assertEveryKeyIsReadHandledOrKept(t *testing.T, s *statement.Schema, domain []string) {
	t.Helper()
	var written []string
	for _, k := range domain {
		if _, read := s.Key(k); read {
			written = append(written, k)
		}
	}
	assert.Equal(t, written, nilIfEmpty(schema.UnreadKeys(s, nil, nil)), "%s: nothing read keeps every key", s.At.Pointer)
	assert.Empty(t, schema.UnreadKeys(s, domain, nil), "%s: everything read keeps nothing", s.At.Pointer)
	assert.Empty(t, schema.UnreadKeys(s, nil, domain), "%s: everything handled keeps nothing", s.At.Pointer)
	even := make([]string, 0, len(written))
	var odd []string
	for i, k := range written {
		if i%2 == 0 {
			even = append(even, k)
		} else {
			odd = append(odd, k)
		}
	}
	assert.Equal(t, odd, nilIfEmpty(schema.UnreadKeys(s, even, nil)), "%s: a key read is the one not kept", s.At.Pointer)
}

// keyValues writes a value for each key of the census's domain beside a target.
var keyValues = map[string]string{
	"additionalItems": "{type: string}", "additionalProperties": "{type: string}", "const": "x",
	"discriminator": "{propertyName: k}", "enum": "[x, y]", "format": "date", "items": "{type: string}",
	"maxItems": "2", "minItems": "2", "patternProperties": "{'^a': {type: string}}",
	"prefixItems": "[{type: string}]", "properties": "{a: {type: string}}", "required": "[a]",
	"uniqueItems": "true", "exclusiveMaximum": "10", "exclusiveMinimum": "0", "maxLength": "9",
	"maxProperties": "9", "maximum": "9", "minLength": "1", "minProperties": "1", "minimum": "1",
	"multipleOf": "2", "pattern": "'^a'",
}

// keyTargets are the schemas each key is written beside, one of each kind a
// position lowers to.
var keyTargets = []string{
	"type: string", "type: integer", "type: boolean", "type: object", "type: array",
	"enum: [p, q]", "const: p", "oneOf: [{type: string}, {type: integer}]", "type: [string, integer]",
	"allOf: [{type: object, properties: {b: {type: string}}}]",
}

// TestUnread_EveryKeyShapesOrIsKept writes each key of the census's domain but
// type beside each target kind and holds the compiled types to differ from the
// target's alone: a key a build read shapes the node, and one none read is
// kept. A build saying it read a key it did not would drop it in silence.
func TestUnread_EveryKeyShapesOrIsKept(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    S: {"
	types := func(target, extra string) string {
		doc, _ := schema.LowerSpec(t, head+target+extra+"}\n")
		data, err := json.Marshal(doc.Types, json.Deterministic(true))
		require.NoError(t, err)
		return string(data)
	}
	for _, target := range keyTargets {
		alone := types(target, "")
		for _, k := range schema.CensusDomain() {
			if k == "type" || strings.Contains(target, k+":") {
				continue
			}
			assert.NotEqual(t, alone, types(target, ", "+k+": "+keyValues[k]), "%s beside %s changes nothing", k, target)
		}
	}
}

// nilIfEmpty returns keys, nil where it holds none.
func nilIfEmpty(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// lowered is a document lowered once: its IR and the statements of its
// component schemas.
type lowered struct {
	doc        *ir.Document
	statements []*statement.Statement
}

// readInputs returns the documents the census's replacement is held to: the
// component schemas of every document under testdata, of the edges readEdges
// writes and of every core case, lowered.
func readInputs(t *testing.T) []lowered {
	t.Helper()
	var out []lowered
	for _, src := range readSources(t) {
		if doc, statements, loaded := schema.LowerSource(t, src); loaded {
			out = append(out, lowered{doc: doc, statements: statements})
		}
	}
	return out
}

// readSources returns the sources of those documents.
func readSources(t *testing.T) []compilers.Source {
	t.Helper()
	out := append(testdataSources(t), readEdges()...)
	cases := openapicases.New()
	each, err := harness.Each(cases)
	require.NoError(t, err)
	core := harness.Core(cases)
	for _, at := range each {
		if doc, writes := cases.Write(at); writes && (*fullGrid || core(at)) {
			out = append(out, doc.Sources[0])
		}
	}
	return out
}

// TestUnread_EdgesCompileAsTheyDid pins what the documents readEdges adds
// compile to, which the census decides and the builds' reads must keep: the
// empty pattern no variant holds is kept on the union, and the type an enum's
// members all are values of says nothing the Enum does not.
func TestUnread_EdgesCompileAsTheyDid(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, string(readEdges()[0].Data))
	pattern := doc.Types[ids.Document{}.ForPointer(ids.Ptr("components", "schemas", "EmptyPattern"))]
	require.NotNil(t, pattern)
	assert.Equal(t, ir.KindUnion, pattern.Kind())
	assert.Contains(t, pattern.Common().Unmodeled, "openapi:pattern", "no variant holds the empty pattern")
	enum, ok := doc.Types[ids.Document{}.ForPointer(ids.Ptr("components", "schemas", "EnumOfTypes"))].(*ir.Enum)
	require.True(t, ok, "the members are of one kind")
	assert.Equal(t, ir.PrimString, enum.ValueType)
	assert.NotContains(t, enum.Unmodeled, "openapi:type", "every member is a value of the types")
}

// readEdges returns the documents of what reaches a census site no document
// under testdata or core case does: an empty pattern beside a type list whose
// string variant is a Scalar of its own, and an enum whose members are all
// values of the several types beside it.
func readEdges() []compilers.Source {
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    EmptyPattern: {type: [string, integer], format: x-text, pattern: ""}
    EnumOfTypes: {type: [string, integer], enum: [a, b]}
`
	return []compilers.Source{{Path: "edges.yaml", Data: []byte(spec)}}
}

// fullGrid widens the cases the tests of what a build did not read lower from
// the core cases to every case, as make cases does.
var fullGrid = flag.Bool("full", false, "lower every case, not the core cases alone")

// testdataSources returns each OpenAPI document under the repository's
// testdata, read as a source.
func testdataSources(t *testing.T) []compilers.Source {
	t.Helper()
	var out []compilers.Source
	root := filepath.Join("..", "..", "..", "..", "testdata")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !slices.Contains([]string{".yaml", ".yml", ".json"}, filepath.Ext(path)) {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "openapi") || strings.Contains(string(data), "swagger") {
			out = append(out, compilers.Source{Path: path, Data: data})
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	return out
}
