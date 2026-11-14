// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// typeMismatch is the code of a value of the wrong type for its field.
const typeMismatch = diag.Validation + "/validation-type-mismatch"

// refPosition is one place a Reference Object, or a Path Item's $ref, can be
// written. body is the document after its header: a line starting with "@@"
// belongs to the entry under test, which a copy of the document leaves out by
// dropping those lines, and the entry's $ref is written as "$ref:", to which
// each spelling is appended.
type refPosition struct {
	name string
	// since31 is set for a position 3.0 has no place for.
	since31 bool
	// pointer is where the entry's $ref sits.
	pointer string
	// pathItem is set where the $ref is the item's own field, so that an
	// ill-typed one is the null warning and the item lowers what it writes.
	pathItem bool
	body     string
}

const refOp = "paths:\n  /a:\n    get:\n      operationId: getA\n      responses: {\"200\": {description: ok}}\n"

// refPositions is every position the compiler reads a Reference Object at, the
// Path Item's own $ref among them.
var refPositions = []refPosition{
	{
		name: "a path item under paths", pathItem: true, pointer: "/paths/~1p/$ref",
		body: refOp + "@@  /p:\n@@    $ref:\n",
	},
	{
		name: "a path item under webhooks", since31: true, pathItem: true, pointer: "/webhooks/w/$ref",
		body: "paths: {}\nwebhooks:\n  ok: {get: {responses: {\"200\": {description: ok}}}}\n@@  w:\n@@    $ref:\n",
	},
	{
		name: "a path item of a callback", pathItem: true,
		pointer: "/paths/~1a/post/callbacks/cb/{$request.body#~1v}/$ref",
		body: "paths:\n  /a:\n    post:\n      operationId: postA\n      responses: {\"200\": {description: ok}}\n" +
			"      callbacks:\n        cb:\n          '{$request.body#/u}':\n            post: {responses: {\"200\": {description: ok}}}\n" +
			"@@          '{$request.body#/v}':\n@@            $ref:\n",
	},
	{
		name: "a path item under components/pathItems", since31: true, pathItem: true, pointer: "/components/pathItems/P/$ref",
		body: "paths: {}\ncomponents:\n  pathItems:\n    Ok: {get: {responses: {\"200\": {description: ok}}}}\n@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline parameter", pointer: "/paths/~1a/get/parameters/1/$ref",
		body: "paths:\n  /a:\n    get:\n      operationId: getA\n      parameters:\n" +
			"        - {name: ok, in: query, schema: {type: string}}\n@@        - $ref:\n      responses: {\"200\": {description: ok}}\n",
	},
	{
		name: "a component parameter", pointer: "/components/parameters/P/$ref",
		body: "paths: {}\ncomponents:\n  parameters:\n    Ok: {name: ok, in: query, schema: {type: string}}\n@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline request body", pointer: "/paths/~1a/post/requestBody/$ref",
		body: "paths:\n  /a:\n    post:\n      operationId: postA\n@@      requestBody:\n@@        $ref:\n" +
			"      responses: {\"200\": {description: ok}}\n",
	},
	{
		name: "a component request body", pointer: "/components/requestBodies/P/$ref",
		body: "paths: {}\ncomponents:\n  requestBodies:\n    Ok: {content: {application/json: {schema: {type: string}}}}\n" +
			"@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline response", pointer: "/paths/~1a/get/responses/404/$ref",
		body: "paths:\n  /a:\n    get:\n      operationId: getA\n      responses:\n        \"200\": {description: ok}\n" +
			"@@        \"404\":\n@@          $ref:\n",
	},
	{
		name: "a component response", pointer: "/components/responses/P/$ref",
		body: "paths: {}\ncomponents:\n  responses:\n    Ok: {description: ok}\n@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline header", pointer: "/paths/~1a/get/responses/200/headers/H/$ref",
		body: "paths:\n  /a:\n    get:\n      operationId: getA\n      responses:\n        \"200\":\n          description: ok\n" +
			"          headers:\n            Ok: {schema: {type: string}}\n@@            H:\n@@              $ref:\n",
	},
	{
		name: "a component header", pointer: "/components/headers/P/$ref",
		body: "paths: {}\ncomponents:\n  headers:\n    Ok: {schema: {type: string}}\n@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline example", pointer: "/paths/~1a/get/responses/200/content/application~1json/examples/E/$ref",
		body: "paths:\n  /a:\n    get:\n      operationId: getA\n      responses:\n        \"200\":\n          description: ok\n" +
			"          content:\n            application/json:\n              schema: {type: string}\n              examples:\n" +
			"                Ok: {value: x}\n@@                E:\n@@                  $ref:\n",
	},
	{
		name: "a component example", pointer: "/components/examples/P/$ref",
		body: "paths: {}\ncomponents:\n  examples:\n    Ok: {value: x}\n@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline link", pointer: "/paths/~1a/get/responses/200/links/L/$ref",
		body: "paths:\n  /a:\n    get:\n      operationId: getA\n      responses:\n        \"200\":\n          description: ok\n" +
			"          links:\n            Ok: {operationId: getA}\n@@            L:\n@@              $ref:\n",
	},
	{
		name: "a component link", pointer: "/components/links/P/$ref",
		body: refOp + "components:\n  links:\n    Ok: {operationId: getA}\n@@    P:\n@@      $ref:\n",
	},
	{
		name: "an inline callback", pointer: "/paths/~1a/post/callbacks/cb2/$ref",
		body: "paths:\n  /a:\n    post:\n      operationId: postA\n      responses: {\"200\": {description: ok}}\n" +
			"      callbacks:\n        cb:\n          '{$request.body#/u}':\n            post: {responses: {\"200\": {description: ok}}}\n" +
			"@@        cb2:\n@@          $ref:\n",
	},
	{
		name: "a component callback", pointer: "/components/callbacks/P/$ref",
		body: "paths: {}\ncomponents:\n  callbacks:\n    Ok:\n      '{$request.body#/u}':\n        post: {responses: {\"200\": {description: ok}}}\n" +
			"@@    P:\n@@      $ref:\n",
	},
	{
		name: "a component security scheme", pointer: "/components/securitySchemes/P/$ref",
		body: "paths: {}\ncomponents:\n  securitySchemes:\n    ok: {type: http, scheme: bearer}\n@@    P:\n@@      $ref:\n",
	},
}

// refSpellings are the three ways a $ref can be written null.
var refSpellings = []string{"", " ~", " null"}

// refVersions are the three minors the table is held at.
var refVersions = []string{"3.0.3", "3.1.0", "3.2.0"}

// render writes the position's document at version, with its entry under test
// spelled with spelling, or left out when leftOut.
func (p refPosition) render(version, spelling string, leftOut bool) string {
	var b strings.Builder
	b.WriteString("openapi: " + version + "\ninfo: {title: t, version: \"1\"}\n")
	for line := range strings.SplitSeq(strings.TrimSuffix(p.body, "\n"), "\n") {
		rest, marked := strings.CutPrefix(line, "@@")
		switch {
		case !marked:
			b.WriteString(line + "\n")
		case leftOut:
		default:
			b.WriteString(strings.Replace(rest, "$ref:", "$ref:"+spelling, 1) + "\n")
		}
	}
	return b.String()
}

// TestCompile_ANullRefOfAReferenceObjectLeavesTheEntryOut holds a Reference
// Object whose $ref is written null, in each of its three spellings, at every
// position and on three minors, to one finding at the $ref, and the entry read
// as left out: the document lowers as it does without the entry. A Path Item's
// own $ref is the item's field, so an item holding one is the null warning and
// the item is read for what it writes.
func TestCompile_ANullRefOfAReferenceObjectLeavesTheEntryOut(t *testing.T) {
	t.Parallel()
	for _, p := range refPositions {
		for _, version := range refVersions {
			if p.since31 && version == "3.0.3" {
				continue
			}
			control, controlDiags := compileText(t, "spec.yaml", p.render(version, "", true), openapi.Options{})
			require.NotNil(t, control, "%s on %s", p.name, version)
			require.Empty(t, controlDiags, "the document without the entry is valid: %s on %s: %+v", p.name, version, controlDiags)

			for _, spelling := range refSpellings {
				t.Run(p.name+" on "+version+" spelled $ref:"+spelling, func(t *testing.T) {
					t.Parallel()
					doc, diags := compileText(t, "spec.yaml", p.render(version, spelling, false), openapi.Options{})

					require.NotNil(t, doc, "the source is read, not refused: %+v", diags)
					require.Len(t, diags, 1, "one finding, at the node: %+v", diags)
					assert.Equal(t, typeMismatch, diags[0].Code)
					assert.Equal(t, p.pointer, string(diags[0].Provenance.Pointer))
					if p.pathItem && spelling != "" {
						assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
						assert.Contains(t, diags[0].Message, "it is null, which is read as the field left out")
					}
					if !p.pathItem {
						assert.Equal(t, ir.SeverityError, diags[0].Severity)
					}
					assert.Equal(t, jsonOf(t, control), jsonOf(t, doc), "the entry is left out, as written null it would be")
				})
			}
		}
	}
}

// TestCompile_AParameterBesideANullRefLowers holds the row as the issue writes
// it, the neighbour written after the entry whose $ref is null: the document is
// read, the one finding stands at the $ref, and the operation holds the
// neighbour.
func TestCompile_AParameterBesideANullRefLowers(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /a:\n    get:\n      parameters:\n" +
		"        - $ref:\n        - {name: ok, in: query, schema: {type: string}}\n" +
		"      responses: {\"200\": {description: ok}}\n"

	doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	require.NotNil(t, doc)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, typeMismatch, diags[0].Code)
	assert.Equal(t, "/paths/~1a/get/parameters/0/$ref", string(diags[0].Provenance.Pointer))
	ops := allOperations(doc)
	require.Len(t, ops, 1)
	require.Len(t, ops[0].Params, 1, "the neighbour lowers, the entry is left out")
	assert.Equal(t, "ok", ops[0].Params[0].Name.Source)
}

// TestCompile_APathItemRefThatIsNoStringIsTheItemsOwnField holds a Path Item
// whose $ref is a list, null or a mapping to what it writes beside it: its
// operations lower, a list or a mapping draws the type error, null draws the
// null warning, and no key is reported unknown.
func TestCompile_APathItemRefThatIsNoStringIsTheItemsOwnField(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n"
	const responses = ", get: {operationId: %s, responses: {\"200\": {description: ok}}}}\n"
	spec := head +
		"  /b: {$ref: []" + strings.Replace(responses, "%s", "b", 1) +
		"  /c: {$ref: ~" + strings.Replace(responses, "%s", "c", 1) +
		"  /d: {$ref: {}" + strings.Replace(responses, "%s", "d", 1)

	doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	require.NotNil(t, doc, "%+v", diags)
	names := map[string]bool{}
	for _, op := range allOperations(doc) {
		names[op.Name.Source] = true
	}
	assert.Equal(t, map[string]bool{"b": true, "c": true, "d": true}, names)

	got := map[string]ir.Severity{}
	for _, d := range diags {
		assert.Equal(t, typeMismatch, d.Code, "only the $ref is reported: %+v", d)
		got[string(d.Provenance.Pointer)] = d.Severity
	}
	assert.Equal(t, map[string]ir.Severity{
		"/paths/~1b/$ref": ir.SeverityError,
		"/paths/~1c/$ref": ir.SeverityWarning,
		"/paths/~1d/$ref": ir.SeverityError,
	}, got)
	assert.Empty(t, byCode(diags, diag.UnknownObjectKey), "$ref is a declared key of the item")
}

// additionalOps is a 3.2 document whose path item writes an additionalOperations
// entry entry, beside LINK, which is well formed.
func additionalOps(entry string) string {
	return "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a:\n    get: {responses: {\"200\": {description: ok}}}\n" +
		"    additionalOperations:\n" + entry +
		"      LINK: {responses: {\"200\": {description: ok}}}\n"
}

// TestCompile_AnAdditionalOperationThatIsNoMappingIsLeftOut holds an entry of
// additionalOperations that is no Operation Object, whatever it is written as,
// directly or supplied by a merge key, to one error at the entry, no such
// operation, and the entries beside it lowered.
func TestCompile_AnAdditionalOperationThatIsNoMappingIsLeftOut(t *testing.T) {
	t.Parallel()
	control, controlDiags := compileText(t, "spec.yaml", additionalOps(""), openapi.Options{})
	require.NotNil(t, control)
	require.Empty(t, controlDiags)
	assert.Len(t, allOperations(control), 2, "get and LINK")

	for name, tc := range map[string]struct {
		entry   string
		pointer string
	}{
		"null":      {"      PURGE: ~\n", "/paths/~1a/additionalOperations/PURGE"},
		"x":         {"      PURGE: x\n", "/paths/~1a/additionalOperations/PURGE"},
		"7":         {"      PURGE: 7\n", "/paths/~1a/additionalOperations/PURGE"},
		"true":      {"      PURGE: true\n", "/paths/~1a/additionalOperations/PURGE"},
		"a list":    {"      PURGE: [a]\n", "/paths/~1a/additionalOperations/PURGE"},
		"a merge":   {"      <<: {PURGE: ~}\n", "/paths/~1a/additionalOperations/PURGE"},
		"a merge x": {"      <<: {PURGE: x}\n", "/paths/~1a/additionalOperations/PURGE"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileText(t, "spec.yaml", additionalOps(tc.entry), openapi.Options{})

			require.NotNil(t, doc, "the source is read, not refused: %+v", diags)
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, typeMismatch, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, tc.pointer, string(diags[0].Provenance.Pointer))
			assert.Equal(t, jsonOf(t, control), jsonOf(t, doc), "no such operation: the entry is left out")
		})
	}
}

// TestCompile_AnAliasedNullRefIsReadWithoutAFault holds a Reference Object
// that an alias mounts, whose $ref is null, to the entry left out and the
// finding of the table, not a refusal of the source.
func TestCompile_AnAliasedNullRefIsReadWithoutAFault(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"", " ~", " null"} {
		spec := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\nx-p: &p {$ref:" + spelling + "}\n" +
			"paths:\n  /a:\n    get:\n      operationId: getA\n      parameters:\n" +
			"        - {name: ok, in: query, schema: {type: string}}\n        - *p\n" +
			"      responses: {\"200\": {description: ok}}\n"

		doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

		require.NotNil(t, doc, "$ref:%s: %+v", spelling, diags)
		require.NotEmpty(t, diags)
		for _, d := range diags {
			assert.Equal(t, typeMismatch, d.Code, "%+v", d)
			assert.True(t, strings.HasSuffix(string(d.Provenance.Pointer), "/$ref"), "%+v", d)
		}
		ops := allOperations(doc)
		require.Len(t, ops, 1)
		require.Len(t, ops[0].Params, 1, "the entry is left out, the neighbour lowers")
	}
}
