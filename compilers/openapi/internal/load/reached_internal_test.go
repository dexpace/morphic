// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// externalDir writes files, name to content, into a fresh directory for a root
// to reach, and returns the directory, its links resolved, as a compile names
// the files in it.
func externalDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	return dir
}

// loadExternal loads root as root.yaml in dir, with external references
// allowed, and requires it to load.
func loadExternal(t *testing.T, dir, root string, opts Options) (*LoadedOpenAPIDocument, []ir.Diagnostic) {
	t.Helper()
	opts.Reader = reader.Disk{}
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}, opts)
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// TestLandingTargets covers which of the table's schema landings are judged,
// and where, read off the table alone before anything is built: one a $ref's
// chain reaches, at the least such $ref and named by its document, in the
// source or not, read as written where it lands. One no $ref reaches, one where
// nothing is written and a boolean schema are none, but a value that is no
// schema is one, unless it is the rest of the tuple its site writes, which its
// `items` judges; a tuple's rest written as a mapping is one.
func TestLandingTargets(t *testing.T) {
	t.Parallel()
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	five := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"}
	written := map[reftable.DocumentPointer]*yaml.Node{
		{Pointer: "/x-lib/A"}:                         mapping,
		{Doc: 1, Pointer: "/B"}:                       mapping,
		{Doc: 1, Pointer: "/F"}:                       mapping,
		{Doc: 1, Pointer: "/C"}:                       mapping,
		{Pointer: "/x-lib/E"}:                         {Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
		{Pointer: "/x-lib/G"}:                         five,
		{Pointer: "/x-lib/T/additionalItems"}:         five,
		{Pointer: "/x-lib/U/additionalItems"}:         mapping,
		{Doc: 1, Pointer: "/x-lib/T/additionalItems"}: five,
	}
	got := landingTargets([]reftable.Landing{
		{At: reftable.DocumentPointer{Pointer: "/x-lib/A"}, Name: "spec.yaml", Site: reftable.DocumentPointer{Pointer: "/s"}, Reached: true},
		{At: reftable.DocumentPointer{Doc: 1, Pointer: "/B"}, Name: "other.yaml", Site: reftable.DocumentPointer{Pointer: "/t"}, Reached: true},
		{At: reftable.DocumentPointer{Doc: 1, Pointer: "/F"}, Name: "other.yaml", Site: reftable.DocumentPointer{Doc: 1, Pointer: "/u"},
			Reached: true},
		{At: reftable.DocumentPointer{Doc: 1, Pointer: "/C"}, Name: "other.yaml"},
		{At: reftable.DocumentPointer{Pointer: "/x-lib/D"}, Reached: true},
		{At: reftable.DocumentPointer{Pointer: "/x-lib/E"}, Reached: true},
		{At: reftable.DocumentPointer{Pointer: "/x-lib/G"}, Name: "spec.yaml", Site: reftable.DocumentPointer{Pointer: "/g"}, Reached: true},
		{At: reftable.DocumentPointer{Pointer: "/x-lib/T/additionalItems"}, Site: reftable.DocumentPointer{Pointer: "/x-lib/T"}, Reached: true},
		{At: reftable.DocumentPointer{Pointer: "/x-lib/U/additionalItems"}, Site: reftable.DocumentPointer{Pointer: "/x-lib/U"}, Reached: true},
		{At: reftable.DocumentPointer{Doc: 1, Pointer: "/x-lib/T/additionalItems"}, Site: reftable.DocumentPointer{Pointer: "/x-lib/T"},
			Reached: true},
	}, func(p reftable.DocumentPointer) *yaml.Node { return written[p] })

	require.Len(t, got, 6)
	assert.Equal(t, reachedTarget{site: reftable.DocumentPointer{Pointer: "/s"}, at: reftable.DocumentPointer{Pointer: "/x-lib/A"},
		kind: specrules.Schema, node: mapping, doc: "spec.yaml"}, got[0])
	assert.True(t, got[0].inSource())
	assert.Equal(t, reftable.DocumentPointer{Pointer: "/t"}, got[1].site, "a $ref the source writes places it")
	assert.Equal(t, reftable.DocumentPointer{Doc: 1, Pointer: "/B"}, got[1].at, "read where it lands, in that document")
	assert.Equal(t, "other.yaml", got[1].doc)
	assert.False(t, got[1].inSource())
	assert.Equal(t, reftable.DocumentPointer{Doc: 1, Pointer: "/u"}, got[2].site, "a $ref only another document writes places it")
	assert.Equal(t, reachedTarget{site: reftable.DocumentPointer{Pointer: "/g"}, at: reftable.DocumentPointer{Pointer: "/x-lib/G"},
		kind: specrules.Schema, node: five, doc: "spec.yaml"}, got[3], "a number is no schema, which the check reports")
	assert.Equal(t, reftable.DocumentPointer{Pointer: "/x-lib/U/additionalItems"}, got[4].at, "a tuple's rest written as a mapping")
	assert.Equal(t, reftable.DocumentPointer{Doc: 1, Pointer: "/x-lib/T/additionalItems"}, got[5].at,
		"another document's position is no rest of the tuple its site writes")
}

// TestObjectTargets covers which object landings are validated, and where: one
// a reference object's chain reaches, at the least such reference, as the kind
// the landing is read as, the node written there. One no reference reaches and
// one where nothing is written are none.
func TestObjectTargets(t *testing.T) {
	t.Parallel()
	param := &yaml.Node{Kind: yaml.MappingNode}
	written := map[reftable.DocumentPointer]*yaml.Node{{Pointer: "/x-params/q"}: param, {Doc: 1, Pointer: "/q"}: param}
	got := objectTargets([]reftable.ObjectLanding{
		{At: reftable.DocumentPointer{Pointer: "/x-params/q"}, Kind: reftable.RefParameter, Name: "spec.yaml",
			Site: reftable.DocumentPointer{Pointer: "/p"}, Reached: true},
		{At: reftable.DocumentPointer{Doc: 1, Pointer: "/q"}, Kind: reftable.RefHeader, Name: "other.yaml",
			Site: reftable.DocumentPointer{Doc: 1, Pointer: "/h"}, Reached: true},
		{At: reftable.DocumentPointer{Pointer: "/x-params/unreached"}, Kind: reftable.RefParameter, Name: "spec.yaml"},
		{At: reftable.DocumentPointer{Pointer: "/x-params/gone"}, Kind: reftable.RefParameter, Name: "spec.yaml", Reached: true},
	}, func(p reftable.DocumentPointer) *yaml.Node { return written[p] })

	assert.Equal(t, []reachedTarget{
		{site: reftable.DocumentPointer{Pointer: "/p"}, at: reftable.DocumentPointer{Pointer: "/x-params/q"}, kind: specrules.Parameter, node: param,
			doc: "spec.yaml"},
		{site: reftable.DocumentPointer{Doc: 1, Pointer: "/h"}, at: reftable.DocumentPointer{Doc: 1, Pointer: "/q"}, kind: specrules.Header,
			node: param, doc: "other.yaml"},
	}, got)
}

// TestObjectKinds_NamesTheRuleTablesKindOfEach pins that every kind of object a
// reference object can stand for is held to the rule table as that kind.
func TestObjectKinds_NamesTheRuleTablesKindOfEach(t *testing.T) {
	t.Parallel()
	want := map[reftable.RefKind]specrules.Kind{
		reftable.RefPathItem: specrules.PathItem, reftable.RefParameter: specrules.Parameter, reftable.RefHeader: specrules.Header,
		reftable.RefRequestBody: specrules.RequestBody, reftable.RefResponse: specrules.Response, reftable.RefCallback: specrules.Callback,
		reftable.RefExample: specrules.Example, reftable.RefLink: specrules.Link, reftable.RefSecurityScheme: specrules.SecurityScheme,
		reftable.RefMediaType: specrules.MediaType,
	}
	assert.Equal(t, want, objectKinds)
}

// TestWrittenIn_ReadsTheNodeAtAPosition pins what writtenIn reads at a
// position of a bound document: the node written there, an alias followed,
// and nil where the pointer names nothing.
func TestWrittenIn_ReadsTheNodeAtAPosition(t *testing.T) {
	t.Parallel()
	root := openapitest.YAMLDocument(t, "openapi: 3.1.0\nx-a: &a 5\nx-b: *a\n")
	table := reftable.Bind(t.Context(), root, reftable.Config{Self: "spec.yaml"})
	view := nodeview.New()
	five := writtenIn(view, table, reftable.DocumentPointer{Pointer: "/x-b"})
	require.NotNil(t, five)
	assert.Equal(t, "5", five.Value, "the alias's anchor")
	assert.Nil(t, writtenIn(view, table, reftable.DocumentPointer{Pointer: "/x-c/d"}), "nothing is written there")
}

// TestLoad_ExternalRefResolutionErrors pins how a finding inside a document an
// external reference names is reported: under its own rule and severity, in
// that document's LoadedOpenAPIDocument.Sources entry, at the node it is about (GitHub #537,
// #1030). Findings come in the table's order: an object's missing fields, then
// the fields it holds.
func TestLoad_ExternalRefResolutionErrors(t *testing.T) {
	t.Parallel()
	path := "../../../../testdata/openapi/resolve_main_external.yaml"
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ld, diags, loadErr := Load(t.Context(), 0, compilers.Source{Path: path, Data: data},
		Options{Reader: reader.Disk{}})
	require.NoError(t, loadErr)
	require.NotNil(t, ld)

	want := []struct {
		severity ir.Severity
		code     string
		at       ir.Provenance
		message  string
	}{
		{ir.SeverityError, "openapi/validation/validation-required-field",
			ir.Provenance{Source: 1, Pointer: "/components/responses/Bad", Position: ir.Position{Line: 7, Column: 7}},
			"Response Object field \"description\" is REQUIRED in OpenAPI 3.1 (Response Object)"},
		{ir.SeverityError, "openapi/validation/validation-type-mismatch",
			ir.Provenance{Source: 1, Pointer: "/components/responses/Bad/headers/X/required",
				Position: ir.Position{Line: 10, Column: 21}},
			"Header Object field \"required\" must be a boolean in OpenAPI 3.1 (Header Object)"},
	}
	require.Len(t, diags, len(want), "%+v", diags)
	for i, w := range want {
		assert.Equal(t, w.severity, diags[i].Severity, "entry %d", i)
		assert.Equal(t, w.code, diags[i].Code, "entry %d", i)
		assert.Equal(t, w.at, diags[i].Provenance, "entry %d", i)
		assert.Equal(t, w.message, diags[i].Message, "entry %d", i)
	}
	other, ok := ld.Others.At(1)
	require.True(t, ok)
	assert.Equal(t, "resolve_target_invalid.yaml", other.Label, "the document the $ref names")
}

// TestReached_CheckAllStopsOnceTheContextIsDone pins the context the checks of
// reached objects ask before each target: handed a done one, no target is
// read, where a live one reads and checks each.
func TestReached_CheckAllStopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	doc := parseSpec(t, minimal31)
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{name: p}"), &node))
	targets := map[objectKey]reachedTarget{
		{node: node.Content[0], kind: specrules.Parameter}: {site: reftable.DocumentPointer{Pointer: "/x"}, kind: specrules.Parameter,
			node: node.Content[0], doc: "other.yaml"},
	}
	sites := places{}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	stopped := newReached(doc, Options{}, provenance.Tree{}, judgment{checker: specrules.NewChecker()})
	assert.Empty(t, stopped.checkAll(cancelled, sites, targets, comparePositionsForTest))
	assert.Empty(t, stopped.seen, "no target was read")

	live := newReached(doc, Options{}, provenance.Tree{}, judgment{checker: specrules.NewChecker()})
	assert.NotEmpty(t, live.checkAll(t.Context(), sites, targets, comparePositionsForTest), "a parameter with no `in` is found wanting")
	assert.NotEmpty(t, live.seen)
}

// comparePositionsForTest orders positions by document, then pointer, as a
// table orders positions of one document.
func comparePositionsForTest(p, q reftable.DocumentPointer) int {
	if c := cmp.Compare(p.Doc, q.Doc); c != 0 {
		return c
	}
	return cmp.Compare(p.Pointer, q.Pointer)
}

// TestReached_ASchemaTheSourceWritesUnmodelledIsJudgedInItsRows pins a value
// of a schema the source writes where the parser models none, an extension's
// content, which a $ref reaches: the source's own check reads no schema there,
// so the check of what references reach judges it, in the source's rows.
func TestReached_ASchemaTheSourceWritesUnmodelledIsJudgedInItsRows(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\n" +
		"x-defs: {A: {type: integer, exclusiveMinimum: true}}\n" +
		"components:\n  schemas:\n    S: {$ref: '#/x-defs/A'}\n"
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "s.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	var at []jsontext.Pointer
	for _, d := range diags {
		if d.Code == diag.ExclusiveBoundForm {
			at = append(at, d.Provenance.Pointer)
		}
	}
	assert.Equal(t, []jsontext.Pointer{"/x-defs/A/exclusiveMinimum"}, at)
}

// TestLoad_WhatAReferenceReachesIsJudgedOnceBeforeItIsBuilt counts the objects
// the checks of what the references reach read, by node and kind: a parameter
// under an extension a $ref reaches, holding values the table refuses, is read
// once as each kind, every read is made before the phase that reports them,
// and none in it, so the judgement moved rather than being made twice.
func TestLoad_WhatAReferenceReachesIsJudgedOnceBeforeItIsBuilt(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /a:\n    get:\n      operationId: a\n" +
		"      parameters:\n        - $ref: '#/x-lib/p'\n      responses:\n        '200': {description: ok}\n" +
		"x-lib:\n  p: {name: p, in: query, explode: \"true\", required: on, schema: {type: array, items: hello}}\n"
	reads := map[objectKey]int{}
	var current phase
	inReach := 0
	opts := Options{
		entered: func(p phase) { current = p },
		judging: func(kind specrules.Kind, node *yaml.Node) {
			reads[objectKey{node: nodeview.Deref(node), kind: kind}]++
			if current == phaseReach {
				inReach++
			}
		},
	}

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), opts)

	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	require.NotEmpty(t, reads, "the parameter under x-lib is judged")
	kinds := map[specrules.Kind]bool{}
	for key, n := range reads {
		assert.Equal(t, 1, n, "%v at line %d is read once", key.kind, key.node.Line)
		kinds[key.kind] = true
	}
	assert.True(t, kinds[specrules.Parameter], "the parameter is among what is read: %v", kinds)
	assert.Zero(t, inReach, "nothing is judged once the findings are reported")
	assert.Equal(t, 3, countErrorsAt(diags, "openapi/validation/validation-type-mismatch"), "%+v", diags)
}

// TestLoad_AnObjectInsideAnotherTargetIsReadOnceInEitherOrder counts the reads
// of a parameter two references reach, one naming it and one naming the path
// item holding it, the parameter's checked last and then first: each (node,
// kind) is read once, whichever check meets it first, and its one defect is
// reported once (GitHub #571).
func TestLoad_AnObjectInsideAnotherTargetIsReadOnceInEitherOrder(t *testing.T) {
	t.Parallel()
	const item = "x-lib:\n  item:\n    get:\n      operationId: a\n      parameters:\n" +
		"        - {name: p, in: query, explode: 7}\n      responses: {'200': {description: ok}}\n"
	whole := func(path string) string { return "  /" + path + ": {$ref: '#/x-lib/item'}\n" }
	param := func(path string) string {
		return "  /" + path + ":\n    get:\n      operationId: b\n      parameters:\n" +
			"        - {$ref: '#/x-lib/item/get/parameters/0'}\n      responses: {'200': {description: ok}}\n"
	}
	for name, paths := range map[string]string{
		"the item's site first":      whole("a") + param("b"),
		"the parameter's site first": param("a") + whole("b"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n" + paths + item
			reads := map[objectKey]int{}
			opts := Options{judging: func(kind specrules.Kind, node *yaml.Node) {
				reads[objectKey{node: nodeview.Deref(node), kind: kind}]++
			}}

			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), opts)

			require.NoError(t, err)
			require.NotNil(t, doc, "%+v", diags)
			require.NotEmpty(t, reads, "what the references reach is judged")
			for key, n := range reads {
				assert.Equal(t, 1, n, "%v at line %d is read once", key.kind, key.node.Line)
			}
			assert.Equal(t, 1, countErrorsAt(diags, "openapi/validation/validation-type-mismatch"), "%+v", diags)
		})
	}
}
