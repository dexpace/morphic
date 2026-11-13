// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// recursiveRefGroups are the schemas of the JSON Schema Test Suite's
// tests/draft2019-09/recursiveRef.json, groups 0 to 8, at the revision the
// canary's vendored copy of the suite names, its `$schema` aside, which each
// is read with (recursiveRefDocument).
var recursiveRefGroups = []string{
	// 0: $recursiveRef without $recursiveAnchor works like $ref.
	`{"properties":{"foo":{"$recursiveRef":"#"}},"additionalProperties":false}`,
	// 1: $recursiveRef without using nesting.
	`{"$id":"http://localhost:4242/draft2019-09/recursiveRef2/schema.json","$defs":{"myobject":{"$id":"my` +
		`object.json","$recursiveAnchor":true,"anyOf":[{"type":"string"},{"type":"object","additionalProperti` +
		`es":{"$recursiveRef":"#"}}]}},"anyOf":[{"type":"integer"},{"$ref":"#/$defs/myobject"}]}`,
	// 2: $recursiveRef with nesting.
	`{"$id":"http://localhost:4242/draft2019-09/recursiveRef3/schema.json","$recursiveAnchor":true,"$defs` +
		`":{"myobject":{"$id":"myobject.json","$recursiveAnchor":true,"anyOf":[{"type":"string"},{"type":"obj` +
		`ect","additionalProperties":{"$recursiveRef":"#"}}]}},"anyOf":[{"type":"integer"},{"$ref":"#/$defs/m` +
		`yobject"}]}`,
	// 3: $recursiveRef with $recursiveAnchor: false works like $ref.
	`{"$id":"http://localhost:4242/draft2019-09/recursiveRef4/schema.json","$recursiveAnchor":false,"$def` +
		`s":{"myobject":{"$id":"myobject.json","$recursiveAnchor":false,"anyOf":[{"type":"string"},{"type":"o` +
		`bject","additionalProperties":{"$recursiveRef":"#"}}]}},"anyOf":[{"type":"integer"},{"$ref":"#/$defs` +
		`/myobject"}]}`,
	// 4: $recursiveRef with no $recursiveAnchor works like $ref.
	`{"$id":"http://localhost:4242/draft2019-09/recursiveRef5/schema.json","$defs":{"myobject":{"$id":"my` +
		`object.json","$recursiveAnchor":false,"anyOf":[{"type":"string"},{"type":"object","additionalPropert` +
		`ies":{"$recursiveRef":"#"}}]}},"anyOf":[{"type":"integer"},{"$ref":"#/$defs/myobject"}]}`,
	// 5: $recursiveRef with no $recursiveAnchor in the initial target schema resource.
	`{"$id":"http://localhost:4242/draft2019-09/recursiveRef6/base.json","$recursiveAnchor":true,"anyOf":` +
		`[{"type":"boolean"},{"type":"object","additionalProperties":{"$id":"http://localhost:4242/draft2019-` +
		`09/recursiveRef6/inner.json","$comment":"there is no $recursiveAnchor: true here, so we do NOT recur` +
		`se to the base","anyOf":[{"type":"integer"},{"type":"object","additionalProperties":{"$recursiveRef"` +
		`:"#"}}]}}]}`,
	// 6: $recursiveRef with no $recursiveAnchor in the outer schema resource.
	`{"$id":"http://localhost:4242/draft2019-09/recursiveRef7/base.json","anyOf":[{"type":"boolean"},{"ty` +
		`pe":"object","additionalProperties":{"$id":"http://localhost:4242/draft2019-09/recursiveRef7/inner.j` +
		`son","$recursiveAnchor":true,"anyOf":[{"type":"integer"},{"type":"object","additionalProperties":{"$` +
		`recursiveRef":"#"}}]}}]}`,
	// 7: multiple dynamic paths to the $recursiveRef keyword.
	`{"$id":"https://example.com/recursiveRef8_main.json","$defs":{"inner":{"$id":"recursiveRef8_inner.js` +
		`on","$recursiveAnchor":true,"title":"inner","additionalProperties":{"$recursiveRef":"#"}}},"if":{"pr` +
		`opertyNames":{"pattern":"^[a-m]"}},"then":{"title":"any type of node","$id":"recursiveRef8_anyLeafNo` +
		`de.json","$recursiveAnchor":true,"$ref":"recursiveRef8_inner.json"},"else":{"title":"integer node","` +
		`$id":"recursiveRef8_integerNode.json","$recursiveAnchor":true,"type":["object","integer"],"$ref":"re` +
		`cursiveRef8_inner.json"}}`,
	// 8: dynamic $recursiveRef destination (not predictable at schema compile time).
	`{"$id":"https://example.com/main.json","$defs":{"inner":{"$id":"inner.json","$recursiveAnchor":true,` +
		`"title":"inner","additionalProperties":{"$recursiveRef":"#"}}},"if":{"propertyNames":{"pattern":"^[a` +
		`-m]"}},"then":{"title":"any type of node","$id":"anyLeafNode.json","$recursiveAnchor":true,"$ref":"m` +
		`ain.json#/$defs/inner"},"else":{"title":"integer node","$id":"integerNode.json","$recursiveAnchor":t` +
		`rue,"type":["object","integer"],"$ref":"main.json#/$defs/inner"}}`,
}

// recursiveRefDocument returns the group as a document of its own, naming
// 2019-09 by its `$schema`.
func recursiveRefDocument(group string) string {
	return strings.Replace(group, "{", `{"$schema":"https://json-schema.org/draft/2019-09/schema",`, 1)
}

// compileRecursiveRef compiles a 3.1 source whose one component $refs file, a
// document of its own beside it holding body, and returns the IR and every
// diagnostic.
func compileRecursiveRef(t *testing.T, file, body string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600))
	src := "openapi: 3.1.0\ninfo: {title: W, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n    S: {$ref: '" + file + "'}\n"
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	return doc, diags
}

// recursiveRefsOf returns, by the pointer of each $recursiveRef in file,
// what became of it: the target it expanded to, or "kept" with the reason.
func recursiveRefsOf(diags []ir.Diagnostic) map[string]string {
	out := map[string]string{}
	for _, d := range diags {
		at, ok := strings.CutSuffix(string(d.Provenance.Pointer), "/$recursiveRef")
		switch {
		case !ok:
		case d.Code == diag.DynamicRefExpanded:
			target, _, _ := strings.Cut(strings.TrimPrefix(d.Message, `$recursiveRef expanded to "`), `"`)
			out[at] = target
		case d.Code == diag.DegradedConstruct:
			out[at] = "kept: " + d.Message
		}
	}
	return out
}

// TestOlderDrafts_ARecursiveRefExpandsWhereItsTargetIsStatic pins 2019-09's
// $recursiveRef on the JSON Schema Test Suite's groups, each a document a 3.1
// component reaches: it expands to the root of the resource it is written in
// where that declares no $recursiveAnchor true, or where every schema of its
// document declaring one lies in that resource, and is kept where two
// resources do, as the target then depends on the evaluation path. One only a
// kept `then` or `else` reaches is never lowered, and says nothing.
func TestOlderDrafts_ARecursiveRefExpandsWhereItsTargetIsStatic(t *testing.T) {
	t.Parallel()
	in := func(group int, pointer string) string {
		return string(ids.In("g" + strconv.Itoa(group) + ".json").ForPointer(jsontext.Pointer(pointer)))
	}
	const tail = "/anyOf/1/additionalProperties"
	want := map[int]map[string]string{
		0: {"/properties/foo": in(0, "")},
		1: {"/$defs/myobject" + tail: in(1, "/$defs/myobject")},
		2: {"/$defs/myobject" + tail: "kept: $recursiveRef was not expanded because $recursiveAnchor: true is declared " +
			"in 2 schema resources of this document, so the target depends on the evaluation path; it is kept verbatim " +
			"under Unmodeled"},
		3: {"/$defs/myobject" + tail: in(3, "/$defs/myobject")},
		4: {"/$defs/myobject" + tail: in(4, "/$defs/myobject")},
		5: {tail + tail: in(5, tail)},
		6: {tail + tail: in(6, tail)},
		7: {},
		8: {},
	}
	require.Len(t, recursiveRefGroups, len(want))
	for group, schema := range recursiveRefGroups {
		t.Run(strconv.Itoa(group), func(t *testing.T) {
			t.Parallel()
			_, diags := compileRecursiveRef(t, "g"+strconv.Itoa(group)+".json", recursiveRefDocument(schema))
			assert.Equal(t, want[group], recursiveRefsOf(diags))
		})
	}
	// An object inner, so its additionalProperties lowers, reached through a
	// union, which lowers each branch, where 7 reaches it through a conditional.
	t.Run("7 with anyOf for its conditional", func(t *testing.T) {
		t.Parallel()
		schema := strings.NewReplacer(`"if":{"propertyNames":{"pattern":"^[a-m]"}},"then":`, `"anyOf":[`,
			`,"else":`, `,`, `"title":"inner",`, `"title":"inner","type":"object",`).Replace(recursiveRefGroups[7])
		schema = schema[:len(schema)-1] + "]}"
		_, diags := compileRecursiveRef(t, "g7.json", recursiveRefDocument(schema))
		got := recursiveRefsOf(diags)
		require.Len(t, got, 1, "%v", got)
		for _, outcome := range got {
			assert.Contains(t, outcome, "is declared in 3 schema resources of this document")
		}
	})
}

// dynamicTwin returns the 2020-12 twin of group, a document whose
// $recursiveRef expands to target, a pointer of it: the $dynamicAnchor node
// declared at target alone, and each $recursiveRef a $dynamicRef naming it.
func dynamicTwin(t *testing.T, group string, target jsontext.Pointer) string {
	t.Helper()
	var doc any
	require.NoError(t, json.Unmarshal([]byte(group), &doc))
	for stack := []any{doc}; len(stack) > 0; {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := v.(type) {
		case map[string]any:
			delete(v, "$recursiveAnchor")
			if _, ok := v["$recursiveRef"]; ok {
				delete(v, "$recursiveRef")
				v["$dynamicRef"] = "#node"
			}
			for _, child := range v {
				stack = append(stack, child)
			}
		case []any:
			stack = append(stack, v...)
		}
	}
	at := doc
	for token := range target.Tokens() {
		switch v := at.(type) {
		case map[string]any:
			at = v[token]
		case []any:
			i, err := strconv.Atoi(token)
			require.NoError(t, err)
			at = v[i]
		}
	}
	anchored, ok := at.(map[string]any)
	require.True(t, ok, "%s holds a schema", target)
	anchored["$dynamicAnchor"] = "node"
	out, err := json.Marshal(doc, json.Deterministic(true))
	require.NoError(t, err)
	return strings.Replace(string(out), "{", `{"$schema":"https://json-schema.org/draft/2020-12/schema",`, 1)
}

// TestOlderDrafts_ARecursiveRefReadsAsItsDynamicTwin pins each $recursiveRef of
// the suite's groups that expands against its twin written in 2020-12, a
// $dynamicAnchor at the target and a $dynamicRef naming it: both expand to one
// type, the target's own.
func TestOlderDrafts_ARecursiveRefReadsAsItsDynamicTwin(t *testing.T) {
	t.Parallel()
	const tail = "/anyOf/1/additionalProperties"
	for group, target := range map[int]jsontext.Pointer{
		0: "", 1: "/$defs/myobject", 3: "/$defs/myobject", 4: "/$defs/myobject", 5: tail, 6: tail,
	} {
		t.Run(strconv.Itoa(group), func(t *testing.T) {
			t.Parallel()
			file := "g" + strconv.Itoa(group) + ".json"
			_, recursive := compileRecursiveRef(t, file, recursiveRefDocument(recursiveRefGroups[group]))
			_, dynamic := compileRecursiveRef(t, file, dynamicTwin(t, recursiveRefGroups[group], target))
			expanded := func(diags []ir.Diagnostic, keyword string) []string {
				var out []string
				for _, d := range diags {
					if d.Code == diag.DynamicRefExpanded {
						at, _ := strings.CutSuffix(string(d.Provenance.Pointer), "/"+keyword)
						target, _, _ := strings.Cut(strings.TrimPrefix(d.Message, keyword+` expanded to "`), `"`)
						out = append(out, at+" "+target)
					}
				}
				return out
			}
			want := expanded(recursive, "$recursiveRef")
			require.Len(t, want, 1)
			assert.Equal(t, want, expanded(dynamic, "$dynamicRef"))
		})
	}
}
