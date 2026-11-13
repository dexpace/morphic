// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// findingsIn checks doc as a 3.1 Schema Object read in dialect, and returns
// what it finds, ordered as findings orders them.
func findingsIn(t *testing.T, dialect, doc string) []finding {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &n))
	diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{
		Start:  oas.Schema,
		Rows:   rulesOf("3.1.0", dialect),
		Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
	})
	out := make([]finding, 0, len(diags))
	for _, d := range diags {
		out = append(out, finding{Code: d.Code, At: string(d.Provenance.Pointer)})
	}
	sortFindings(out)
	return out
}

// TestCheck_ASchemaIsHeldToItsDialectsRows pins a schema read with a JSON
// Schema draft's rows: each keyword is held to the draft's row and rules, a
// schema only the draft places is entered, true and false stand as schemas
// only from draft 6, a keyword only 2020-12 defines is warned of and judged by
// its row, and a draft's own tuple is read as one.
func TestCheck_ASchemaIsHeldToItsDialectsRows(t *testing.T) {
	for _, tc := range []struct {
		name, dialect, doc string
		want               []finding
	}{
		{"an empty required in draft 4", draft04, `{required: []}`, []finding{at(entries, "/required")}},
		{"an empty required in draft 7", draft07, `{required: []}`, nil},
		{"a number for draft 4's id", draft04, `{id: 5}`, []finding{at(mismatch, "/id")}},
		{"a $id draft 4 reads for nothing", draft04, `{$id: 'not a uri#x'}`, nil},
		{"draft 4's definitions", draft04, `{definitions: {Unused: {type: string, minLength: abc}}}`,
			[]finding{at(mismatch, "/definitions/Unused/minLength")}},
		{"a number for definitions", draft04, `{definitions: 5}`, []finding{at(mismatch, "/definitions")}},
		{"a number for dependencies", draft04, `{dependencies: 5}`, []finding{at(mismatch, "/dependencies")}},
		{"a boolean schema in draft 4", draft04, `{properties: {a: true}, not: false}`,
			[]finding{at(mismatch, "/properties/a"), at(mismatch, "/not")}},
		{"a boolean schema in draft 6", draft06, `{properties: {a: true}, not: false}`, nil},
		{"draft 4's additionalProperties takes a boolean", draft04, `{additionalProperties: false}`, nil},
		{"draft 7's dependencies", draft07, `{dependencies: {a: {minLength: abc}, b: [x, x], c: 5, d: [], e: true}}`,
			[]finding{at(mismatch, "/dependencies/a/minLength"), at(duplicate, "/dependencies/b/1"),
				at(mismatch, "/dependencies/c")}},
		{"draft 4's dependencies", draft04, `{dependencies: {a: [], b: [{}], c: true}}`,
			[]finding{at(entries, "/dependencies/a"), at(mismatch, "/dependencies/b"), at(mismatch, "/dependencies/c")}},
		{"a draft's tuple", draft07, `{items: [{minLength: abc}, 5], additionalItems: 5}`,
			[]finding{at(mismatch, "/items/0/minLength"), at(mismatch, "/items/1"), at(mismatch, "/additionalItems")}},
		{"a null rest", draft07, `{items: [{}], additionalItems: null}`, []finding{at(mismatch, "/additionalItems")}},
		{"draft 4's exclusive bound", draft04, `{minimum: 1, exclusiveMinimum: true, exclusiveMaximum: 5}`,
			[]finding{at(form, "/exclusiveMaximum")}},
		{"a list of items in 2020-12", draft2020, `{items: [{}]}`, []finding{at(itemsList, "/items")}},
		{"a keyword 2020-12 added", draft04, `{examples: 5, $defs: {A: {minLength: abc}}}`,
			[]finding{at(later, "/examples"), at(mismatch, "/examples"), at(later, "/$defs"),
				at(mismatch, "/$defs/A/minLength")}},
		{"prefixItems beside a draft's tuple", draft07, `{items: [{}], prefixItems: [{}]}`, []finding{at(later, "/prefixItems")}},
		{"an anchor draft 7 defines none of", draft07, `{$anchor: 1a}`, []finding{at(later, "/$anchor"), at(badFormat, "/$anchor")}},
		{"an anchor in 2019-09's grammar", draft2019, `{$anchor: 'a:b'}`, nil},
		{"an empty anchor in 2019-09", draft2019, `{$anchor: ''}`, []finding{at(empty, "/$anchor")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := append([]finding(nil), tc.want...)
			sortFindings(want)
			assert.Empty(t, cmp.Diff(want, findingsIn(t, tc.dialect, tc.doc), cmpopts.EquateEmpty()))
		})
	}
}

// TestCheck_AFindingInADraftNamesItsDialect pins the wording of a finding
// about a schema a JSON Schema draft's rows read: it holds the schema to its
// dialect and cites the draft's section, where one the minor's own rows read
// names the minor.
func TestCheck_AFindingInADraftNamesItsDialect(t *testing.T) {
	read := func(dialect string) string {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(`{required: []}`), &n))
		diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Start: oas.Schema, Rows: rulesOf("3.1.0", dialect)})
		require.Len(t, diags, 1)
		return diags[0].Message
	}
	assert.Equal(t, `Schema Object field "required" must hold at least 1 entry in this schema's dialect (5.4.3.1. Valid values)`,
		read(draft04))
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`{prefixItems: []}`), &n))
	diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Start: oas.Schema})
	require.Len(t, diags, 1)
	assert.Equal(t, `Schema Object field "prefixItems" must hold at least 1 entry in OpenAPI 3.1 (Schema Object)`, diags[0].Message)
	assert.Equal(t, `keyword "prefixItems" is not defined by this schema's dialect; it is read as JSON Schema 2020-12 defines it; `+
		`this schema writes its dialect's own tuple, an items list, so prefixItems is kept verbatim beside it`,
		laterMessage(t, draft07, `{items: [{}], prefixItems: [{}]}`))
	assert.Equal(t, `keyword "examples" is not defined by this schema's dialect; it is read as JSON Schema 2020-12 defines it`,
		laterMessage(t, draft04, `{examples: [1]}`))
}

// laterMessage returns the message of the one later-version warning checking
// doc as a 3.1 Schema Object read in dialect draws.
func laterMessage(t *testing.T, dialect, doc string) string {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &n))
	diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Start: oas.Schema, Rows: rulesOf("3.1.0", dialect)})
	require.Len(t, diags, 1)
	assert.Equal(t, later, diags[0].Code)
	return diags[0].Message
}

// TestCheck_ARefsSiblingsInADraftAreWarnedOf pins the keywords beside a $ref
// in a schema read in a draft that ignores them: each the draft defines is
// warned of at its key, bar the $ref, the `$schema`, `definitions`, `$comment`
// and the identifier, and read as the schema's own; one the draft defines
// none of draws the warning of a keyword 2020-12 added instead.
func TestCheck_ARefsSiblingsInADraftAreWarnedOf(t *testing.T) {
	doc := `{$ref: '#/a', description: d, minLength: abc, $schema: 'http://json-schema.org/draft-07/schema#', ` +
		`definitions: {}, $comment: c, $id: x, x-a: 1, $defs: {}}`
	assert.Empty(t, cmp.Diff([]finding{
		at(later, "/$defs"), at(later, "/description"), at(later, "/minLength"), at(mismatch, "/minLength"),
	}, findingsIn(t, draft07, doc)))
	assert.Empty(t, findingsIn(t, draft2019, `{$ref: '#/a', description: d}`), "2019-09 applies them")
	for _, d := range func() []ir.Diagnostic {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(`{$ref: '#/a', description: d}`), &n))
		return oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Start: oas.Schema, Rows: rulesOf("3.1.0", draft04)})
	}() {
		assert.Equal(t, `keyword "description" beside a $ref is ignored by this schema's dialect, as by the JSON Schema `+
			`drafts before 2019-09; it is applied as in 2020-12`, d.Message)
	}
}

// TestCheck_ARootReadsItsOwnRows pins Options.RootRows: a schema it names a
// dialect root of is read with that root's rows, and so is what that schema
// holds, while a schema beside it reads the document's.
func TestCheck_ARootReadsItsOwnRows(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents:\n  schemas:\n"+
		"    S: {required: [], properties: {a: {required: []}}}\n    T: {required: [], properties: {b: {required: []}}}\n"), &n))
	root := jsontext.Pointer("/x/components/schemas/S")
	var asked []jsontext.Pointer
	diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{
		Root:   "/x",
		Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
		RootRows: func(at jsontext.Pointer) (oas.SchemaRules, bool) {
			asked = append(asked, at)
			return rulesOf("3.1.0", draft04), at == root
		},
	})
	at := make([]jsontext.Pointer, 0, len(diags))
	for _, d := range diags {
		at = append(at, d.Provenance.Pointer)
	}
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/S/properties/a/required", "/components/schemas/S/required"}, at,
		"what a schema holds is read among its fields, before its rules")
	assert.Contains(t, asked, jsontext.Pointer("/x/components/schemas/T/properties/b"), "asked at every schema, Root included")
}

// TestCheckPending_ReadsWhatACheckHandedBack pins Options.Later and
// CheckPending: a check hands back each Schema Object no schema holds and reads
// none of it, and CheckPending reads them, in order, with the rows it is given,
// telling Seen of each mapping it reads, and once of a node two of them mount.
func TestCheckPending_ReadsWhatACheckHandedBack(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\npaths: {}\ncomponents:\n"+
		"  schemas:\n    S: &s {required: [], properties: {a: {required: []}}}\n    T: *s\n"+
		"  parameters:\n    P: {name: p, in: query, schema: {required: []}}\n"), &n))
	var pending []oas.Pending
	seen := map[oas.Kind]int{}
	alias := strings.NewReplacer("/schemas/T", "/schemas/S")
	opts := oas.Options{
		Locate: func(p jsontext.Pointer) ir.Provenance {
			return ir.Provenance{Pointer: jsontext.Pointer(alias.Replace(string(p)))}
		},
		Seen:  func(k oas.Kind, _ *yaml.Node) { seen[k]++ },
		Later: func(p oas.Pending) { pending = append(pending, p) },
	}
	checker := oas.NewChecker()
	assert.Empty(t, checker.Check(t.Context(), &n, "3.1.0", opts))
	require.Len(t, pending, 3)
	assert.Zero(t, seen[oas.Schema], "a schema handed back is not read")
	opts.Rows = rulesOf("3.1.0", draft04)
	diags := checker.CheckPending(t.Context(), pending, "3.1.0", opts)
	at := make([]jsontext.Pointer, 0, len(diags))
	for _, d := range diags {
		at = append(at, d.Provenance.Pointer)
	}
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/S/properties/a/required", "/components/schemas/S/required",
		"/components/parameters/P/schema/required"}, at, "in order, and once for the node S and T mount")
	assert.Equal(t, 5, seen[oas.Schema], "each mapping read as a schema, S's through T as well")
	assert.Empty(t, checker.CheckPending(t.Context(), pending, "2.0", opts), "a version the table does not cover")
}

// TestCheck_BoundsFollowTheSchemasRows pins a numeric bound, judged as the
// schema lowering reads it, with the rows of the schema's dialect: a literal no
// decimal spells, quoted or not, and an exclusive bound in the other form than
// the rows give, each an error at the value; a sound one, quoted or not, none.
// A node an alias mounts twice is one defect, reported once, where it is
// written.
func TestCheck_BoundsFollowTheSchemasRows(t *testing.T) {
	for _, tc := range []struct {
		name, dialect, doc string
		want               []finding
	}{
		{"decimals", "", `{minimum: '5', maximum: 1e3, multipleOf: 2}`, nil},
		{"no decimal", "", `{maximum: .inf}`, []finding{at(literal, "/maximum")}},
		{"a step no decimal spells", "", `{multipleOf: abc}`, []finding{at(literal, "/multipleOf")}},
		{"a number bound in 2020-12", "", `{exclusiveMinimum: 5}`, nil},
		{"a boolean bound in 2020-12", "", `{exclusiveMinimum: true}`, []finding{at(form, "/exclusiveMinimum")}},
		{"a number bound past a decimal", "", `{exclusiveMaximum: .inf}`, []finding{at(literal, "/exclusiveMaximum")}},
		{"a boolean bound in draft 4", draft04, `{minimum: 1, exclusiveMaximum: false}`, nil},
		{"a number bound in draft 4", draft04, `{exclusiveMinimum: 5}`, []finding{at(form, "/exclusiveMinimum")}},
		{"a boolean bound in draft 6", draft06, `{exclusiveMinimum: true}`, []finding{at(form, "/exclusiveMinimum")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, cmp.Diff(tc.want, findingsIn(t, tc.dialect, tc.doc), cmpopts.EquateEmpty()))
		})
	}
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{properties: {a: &bad {exclusiveMinimum: true}, b: *bad, c: *bad}}"), &n))
	written := strings.NewReplacer("/properties/b/", "/properties/a/", "/properties/c/", "/properties/a/")
	diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Start: oas.Schema, ReadAt: true,
		Locate: func(p jsontext.Pointer) ir.Provenance {
			return ir.Provenance{Pointer: jsontext.Pointer(written.Replace(string(p)))}
		}})
	require.Len(t, diags, 1, "one defect at one node")
	assert.Equal(t, form, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "exclusiveMinimum must be a number in this schema's dialect", diags[0].Message,
		"the first read is where it is written")
}

// TestCheck_AnItemsListFollowsTheSchemasRows pins `items` written as a list:
// a tuple where the rows of the schema's dialect read one, its entries schemas,
// and elsewhere an error at it saying what a document of its version writes
// instead.
func TestCheck_AnItemsListFollowsTheSchemasRows(t *testing.T) {
	assert.Empty(t, findingsIn(t, draft04, `{items: [{type: string}]}`))
	for _, tc := range []struct {
		version, remedy string
	}{
		{"3.1.0", "write a tuple as prefixItems, or name a JSON Schema draft that reads a list as one by $schema"},
		{"3.0.3", "OpenAPI 3.0 writes no tuple, which 3.1 writes as prefixItems"},
	} {
		diags := run(t, tc.version, oas.Schema, `{items: [{type: string}]}`)
		require.Len(t, diags, 1, tc.version)
		assert.Equal(t, itemsList, diags[0].Code)
		assert.Equal(t, ir.SeverityError, diags[0].Severity)
		assert.Equal(t, "items must be a schema in this schema's dialect, not a list; "+tc.remedy, diags[0].Message)
	}
	assert.Empty(t, run(t, "3.1.0", oas.Schema, `{items: {type: string}}`), "one items schema")
}
