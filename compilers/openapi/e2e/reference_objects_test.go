// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// refSpec is a spec compiled as root.yaml in a directory of its own, beside
// the documents others holds by name, through reader.Disk when disk is set and
// with no reader otherwise.
type refSpec struct {
	spec   string
	others map[string]string
	disk   bool
}

// compile compiles source in place of s.spec, as s says.
func (s refSpec) compile(t *testing.T, source string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range s.others {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	opts := compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: s.disk}}
	if s.disk {
		opts.Reader = reader.Disk{}
	}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(source)}}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// inBothOrders compiles s as written and with every mapping reversed, runs
// check on each, and requires the two to hold one type registry and one set of
// findings. A spec is written in the order the binder once answered wrongly.
func (s refSpec) inBothOrders(t *testing.T, check func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic)) {
	t.Helper()
	forward, forwardDiags := s.compile(t, s.spec)
	back, backDiags := s.compile(t, string(reversed(t, s.spec)))
	check(t, forward, forwardDiags)
	check(t, back, backDiags)
	assert.Empty(t, cmp.Diff(typesJSON(t, forward), typesJSON(t, back)), "one registry in either order")
	assert.Equal(t, placedFindings(forwardDiags), placedFindings(backDiags), "one set of findings in either order")
}

// sitedFindings returns each of diags as its severity, code and pointer, in
// order.
func sitedFindings(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
	}
	return out
}

// requestType is the type the request body of the operation named name holds.
func requestType(t *testing.T, doc *ir.Document, name string) ir.TypeID {
	t.Helper()
	op, ok := opByName(doc, name)
	require.True(t, ok, name)
	return requestContent(t, op).Type.Target
}

// bodyAt is the type ID of the JSON body a request body at pointer declares.
func bodyAt(pointer string) ir.TypeID {
	return ir.TypeID("t/anon" + pointer + "/requestBody/content/application~1json/schema")
}

// TestReferenceObjects_APointerThroughAPathItemRefReadsWhereItsChainEnds pins
// GitHub #876: a request body $ref whose pointer passes /z, a path item $ref,
// reads on in /b, where /z's chain ends, whichever is declared first and
// though /z is read after the $ref passing it, and names the body /b declares
// rather than a second one at the spelling.
func TestReferenceObjects_APointerThroughAPathItemRefReadsWhereItsChainEnds(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /c:
    post:
      operationId: c
      requestBody: {$ref: '#/paths/~1z/post/requestBody'}
      responses: {"200": {description: ok}}
  /z: {$ref: '#/paths/~1b'}
  /b:
    post:
      operationId: b
      requestBody: {content: {application/json: {schema: {type: object, properties: {x: {type: string}}}}}}
      responses: {"200": {description: ok}}
`}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		assertNoErrorDiags(t, diags)
		assert.Equal(t, bodyAt("/paths/~1b/post"), requestType(t, doc, "c"))
		assert.NotContains(t, doc.Types, bodyAt("/paths/~1z/post"), "no second node at the spelling")
	})
}

// TestReferenceObjects_AFieldBesideAPathItemRefIsReadWhereItIsWritten pins
// GitHub #824: a pointer through a path item $ref reads a field written beside
// the $ref there, the layer a mount keeps, and any other field in the item the
// $ref names, with or without a reader for other documents
// (docs/compilers/openapi.md#path-item-ref-fields).
func TestReferenceObjects_AFieldBesideAPathItemRefIsReadWhereItIsWritten(t *testing.T) {
	t.Parallel()
	const body = "{content: {application/json: {schema: {type: object, properties: {%s: {type: string}}}}}}"
	op := func(id, body string) string {
		return fmt.Sprintf("{operationId: %s, requestBody: %s, responses: {\"200\": {description: ok}}}", id, body)
	}
	schemaRef := func(pointer string) string {
		return "{content: {application/json: {schema: {$ref: '#" + pointer + "/requestBody/content/application~1json/schema'}}}}"
	}
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /c1: {post: " + op("c1", "{$ref: '#/paths/~1a/put/requestBody'}") + "}\n" +
		"  /c2: {post: " + op("c2", "{$ref: '#/paths/~1a/get/requestBody'}") + "}\n" +
		"  /c3: {post: " + op("c3", "{$ref: '#/paths/~1a/post/requestBody'}") + "}\n" +
		"  /c4: {post: " + op("c4", schemaRef("/paths/~1a/put")) + "}\n" +
		"  /c5: {post: " + op("c5", schemaRef("/paths/~1a/post")) + "}\n" +
		"  /a:\n    $ref: '#/paths/~1b'\n" +
		"    put: " + op("aput", fmt.Sprintf(body, "p")) + "\n    get: " + op("aget", fmt.Sprintf(body, "g")) + "\n" +
		"  /b:\n    get: " + op("bget", fmt.Sprintf(body, "bg")) + "\n    post: " + op("bpost", fmt.Sprintf(body, "bp")) + "\n"
	want := map[string]ir.TypeID{
		"c1": bodyAt("/paths/~1a/put"), "c2": bodyAt("/paths/~1a/get"), "c3": bodyAt("/paths/~1b/post"),
		"c4": bodyAt("/paths/~1a/put"), "c5": bodyAt("/paths/~1b/post"),
	}
	for _, disk := range []bool{false, true} {
		t.Run(fmt.Sprintf("reader %t", disk), func(t *testing.T) {
			t.Parallel()
			refSpec{spec: spec, disk: disk}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
				t.Helper()
				assertNoErrorDiags(t, diags)
				for name, typ := range want {
					assert.Equal(t, typ, requestType(t, doc, name), name)
				}
				assert.NotContains(t, doc.Types, bodyAt("/paths/~1a/post"), "no second node at the spelling")
			})
		})
	}
}

// TestReferenceObjects_AnEndingSlashNamesAnEmptyMember pins GitHub #1330: a
// pointer ending in '/' names the member keyed "" of what precedes it, as
// RFC 6901 reads it, in the source or another document. Where there is none,
// the $ref is unresolved and the object declares one node; where there is one,
// the $ref lands on it.
func TestReferenceObjects_AnEndingSlashNamesAnEmptyMember(t *testing.T) {
	t.Parallel()
	const object = "{name: q, in: query, schema: {type: object, properties: {a: {type: string}}}}"
	const ops = "paths:\n  /a: {get: {operationId: a, parameters: [{$ref: '%sx-params/q/'}], responses: {\"200\": {description: ok}}}}\n" +
		"  /b: {get: {operationId: b, parameters: [{$ref: '#/x-params/q'}], responses: {\"200\": {description: ok}}}}\n"
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	for _, where := range []string{"#/", "ext.yaml#/"} {
		t.Run(where+" none", func(t *testing.T) {
			t.Parallel()
			s := refSpec{spec: head + fmt.Sprintf(ops, where) + "x-params:\n  q: " + object + "\n", disk: true,
				others: map[string]string{"ext.yaml": "x-params:\n  q: " + object + "\n"}}
			s.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
				t.Helper()
				assert.Equal(t, []string{"error " + diag.UnresolvedRef + " /paths/~1a/get/parameters/0: unresolved $ref \"" +
					where + "x-params/q/\": it names nothing in the document it points into"}, placedFindings(diags))
				assert.NotContains(t, doc.Types, ir.TypeID("t/anon/x-params/q//schema"))
				assert.Contains(t, doc.Types, ir.TypeID("t/anon/x-params/q/schema"), "/b's parameter")
			})
		})
	}
	s := refSpec{spec: head + fmt.Sprintf(ops, "#/") + "x-params:\n  q: {\"\": " + object + ", name: r, in: query, schema: {type: string}}\n"}
	s.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		assertNoErrorDiags(t, diags)
		op, ok := opByName(doc, "a")
		require.True(t, ok)
		require.Len(t, op.Params, 1)
		assert.Equal(t, ir.TypeID("t/anon/x-params/q//schema"), op.Params[0].Type.Target, "the member keyed \"\"")
	})
}

// TestReferenceObjects_AMergeListIsReadAsTheDecoderMergesIt pins GitHub #1578:
// a reference object's pointer reads through a `<<` naming a list of mappings,
// in raw YAML, as the decoder merges it, at every kind, and to a key below the
// merged mapping. An alias may not precede its anchor, so each spec is
// compiled in its written order alone.
func TestReferenceObjects_AMergeListIsReadAsTheDecoderMergesIt(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"
	const param = "{name: q, in: query, schema: {type: string}}"
	const getA = "paths:\n  /a: {get: {operationId: a, parameters: [{$ref: '%s'}], responses: {'200': {description: ok}}}}\n"
	for name, spec := range map[string]string{
		"a list": fmt.Sprintf(getA, "#/x-params/q") + "x-c: &c {other: " + param + "}\nx-b: &b {q: " + param + "}\n" +
			"x-params:\n  <<: [*c, *b]\n",
		"a list of one": fmt.Sprintf(getA, "#/x-params/q") + "x-b: &b {q: " + param + "}\nx-params:\n  <<: [*b]\n",
		"a key below": fmt.Sprintf(getA, "#/x-params/deep/q") + "x-c: &c {other: " + param + "}\n" +
			"x-b: &b {deep: {q: " + param + "}}\nx-params:\n  <<: [*c, *b]\n",
		"a response": "paths:\n  /a: {get: {operationId: a, responses: {'200': {$ref: '#/x-r/r'}}}}\n" +
			"x-rc: &rc {o: {description: o}}\nx-rb: &rb {r: {description: r}}\nx-r:\n  <<: [*rc, *rb]\n",
		"a path item": "paths:\n  /a: {$ref: '#/x-p/a'}\n" +
			"x-c: &c {o: {get: {responses: {'200': {description: o}}}}}\n" +
			"x-b: &b {a: {get: {operationId: a, responses: {'200': {description: a}}}}}\nx-p:\n  <<: [*c, *b]\n",
		"a security scheme": "paths:\n  /a: {get: {operationId: a, security: [{S: []}], responses: {'200': {description: ok}}}}\n" +
			"x-b: &b {q: {type: http, scheme: bearer}}\nx-s:\n  <<: [*b]\ncomponents:\n  securitySchemes:\n" +
			"    S: {$ref: '#/x-s/q'}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := refSpec{spec: head + spec}.compile(t, head+spec)
			assert.Empty(t, placedFindings(diags))
			_, ok := opByName(doc, "a")
			assert.True(t, ok, "the operation lowers")
		})
	}
}

// TestReferenceObjects_APointerMayHoldACharacterAboveUFFFF pins GitHub #516: a
// reference object's pointer token holding a character outside the Basic
// Multilingual Plane names the member spelled so.
func TestReferenceObjects_APointerMayHoldACharacterAboveUFFFF(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
x-items:
  "😀":
    get:
      operationId: g
      responses: {"200": {description: ok}}
x-params:
  "😀": {name: p, in: query, schema: {type: string}}
paths:
  /a: {$ref: '#/x-items/😀'}
  /b:
    get:
      operationId: b
      parameters: [{$ref: '#/x-params/😀'}]
      responses: {"200": {description: ok}}
`}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		assert.Empty(t, placedFindings(diags))
		_, ok := opByName(doc, "g")
		assert.True(t, ok, "the path item lowers")
		op, ok := opByName(doc, "b")
		require.True(t, ok)
		require.Len(t, op.Params, 1, "the parameter lowers")
		assert.Equal(t, "p", op.Params[0].Name.Source)
	})
}

// TestReferenceObjects_AFailingChainIsReportedWhereItFails pins GitHub #767
// with docs/compilers/openapi.md#references: X, A and B chain into a response
// none declares, and only B, whose own hop fails, is reported, in each of the
// six orders the three are declared in.
func TestReferenceObjects_AFailingChainIsReportedWhereItFails(t *testing.T) {
	t.Parallel()
	decls := map[string]string{
		"X": "    X: {$ref: '#/components/responses/A'}\n",
		"A": "    A: {$ref: '#/components/responses/B'}\n",
		"B": "    B: {$ref: '#/components/responses/Missing'}\n",
	}
	for _, order := range []string{"XAB", "XBA", "AXB", "ABX", "BXA", "BAX"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
				"  /a: {get: {operationId: a, responses: {\"200\": {$ref: '#/components/responses/X'}}}}\n" +
				"components:\n  responses:\n"
			for _, name := range order {
				spec += decls[string(name)]
			}
			_, diags := refSpec{spec: spec}.compile(t, spec)
			assert.Equal(t, []string{"error " + diag.UnresolvedRef + " /components/responses/B: unresolved $ref " +
				`"#/components/responses/Missing": it names nothing in the document it points into`}, placedFindings(diags))
		})
	}
}

// TestReferenceObjects_ALoopIsReportedOnceAtItsLeastMember pins GitHub #580: a
// loop of reference objects is a cyclic $ref, reported once, at its least
// member, as a loop of schemas beside it is, whether it has one member, two or
// three; a use leading into one reports nothing of its own
// (docs/compilers/openapi.md#reference-cycles).
func TestReferenceObjects_ALoopIsReportedOnceAtItsLeastMember(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody: {$ref: '#/components/requestBodies/RA'}
      responses:
        "200": {$ref: '#/components/responses/Self'}
        "201": {$ref: '#/components/responses/T3'}
        "202": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/S2'}}}}
components:
  schemas:
    S2: {$ref: '#/components/schemas/S1'}
    S1: {$ref: '#/components/schemas/S2'}
  responses:
    A0: {$ref: '#/components/responses/T3'}
    T3: {$ref: '#/components/responses/T1'}
    T2: {$ref: '#/components/responses/T3'}
    T1: {$ref: '#/components/responses/T2'}
    Self: {$ref: '#/components/responses/Self'}
  requestBodies:
    RB: {$ref: '#/components/requestBodies/RA'}
    RA: {$ref: '#/components/requestBodies/RB'}
`}.inBothOrders(t, func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		const cyclic = "error " + diag.CyclicRef + " /components/"
		assert.Equal(t, []string{
			cyclic + "requestBodies/RA: cyclic $ref: the chain of $refs through #/components/requestBodies/RA, " +
				"#/components/requestBodies/RB comes back to where it started and never reaches a request body",
			cyclic + "responses/Self: cyclic $ref: the chain of $refs through #/components/responses/Self comes back " +
				"to where it started and never reaches a response",
			cyclic + "responses/T1: cyclic $ref: the chain of $refs through #/components/responses/T1, " +
				"#/components/responses/T2, #/components/responses/T3 comes back to where it started and never " +
				"reaches a response",
			cyclic + "schemas/S1: cyclic $ref: the chain of $refs through #/components/schemas/S1, " +
				"#/components/schemas/S2 comes back to where it started and never reaches a schema",
		}, placedFindings(diags))
	})
}

// TestReferenceObjects_AReadNeedingItselfIsUnresolved pins a pointer whose read
// passes a reference whose own read needs the first: each such $ref is
// unresolved, with no budget spent past it, and a use of one reports nothing
// of its own. A chain of reads each needing the next, as deep as a document
// may nest, resolves.
func TestReferenceObjects_AReadNeedingItselfIsUnresolved(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/paths/~1a/get'}
  /u: {get: {operationId: u, responses: {"200": {$ref: '#/components/responses/A'}}}}
components:
  responses:
    A: {$ref: '#/components/responses/B/headers/H'}
    B: {$ref: '#/components/responses/A/headers/H'}
`}.inBothOrders(t, func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		const passes = ": its pointer passes through a reference that does not resolve"
		assert.Equal(t, []string{
			"error " + diag.UnresolvedRef + ` /components/responses/A: unresolved $ref "#/components/responses/B/headers/H"` +
				passes,
			"error " + diag.UnresolvedRef + ` /components/responses/B: unresolved $ref "#/components/responses/A/headers/H"` +
				passes,
			"error " + diag.UnresolvedRef + ` /paths/~1a: unresolved $ref "#/paths/~1a/get"` + passes,
		}, placedFindings(diags))
	})

	n := scan.MaxNesting - 16
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a: {$ref: '#/components/pathItems/P0'}\n" +
		"components:\n  pathItems:\n")
	for i := range n {
		fmt.Fprintf(&b, "    P%d: {$ref: '#/components/pathItems/P%d/x-next'}\n", i, i+1)
	}
	fmt.Fprintf(&b, "    P%d: %s{get: {operationId: deep, responses: {\"200\": {description: ok}}}}%s\n", n,
		strings.Repeat("{x-next: ", n), strings.Repeat("}", n))
	doc, diags := refSpec{}.compile(t, b.String())
	assert.Empty(t, placedFindings(diags))
	_, ok := opByName(doc, "deep")
	assert.True(t, ok, "/a mounts the item at the end of %d reads, each needing the next", n)
}

// TestReferenceObjects_AReferenceLandsOnAnObjectOfItsKind pins that a reference
// object's $ref names an object of its own kind: one the source's grammar
// reads as another kind, a schema, a map of objects or a value is unresolved,
// and a mapping in raw YAML is read as the kind, its fields held to the
// object's rules.
func TestReferenceObjects_AReferenceLandsOnAnObjectOfItsKind(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters:
        - {$ref: '#/components/headers/H'}
        - {$ref: '#/components/schemas/S'}
        - {$ref: '#/components/parameters'}
        - {$ref: '#/x-raw/s'}
        - {$ref: '#/x-raw/m'}
      responses: {"200": {description: ok}}
components:
  headers:
    H: {schema: {type: string}}
  schemas:
    S: {type: string}
  parameters:
    P: {name: p, in: query, schema: {type: string}}
x-raw:
  s: a string
  m: {in: query, schema: {type: string}}
`}.inBothOrders(t, func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		got := sitedFindings(diags)
		for _, d := range diags {
			if d.Code == diag.UnresolvedRef {
				assert.Contains(t, d.Message, ": it names a position that holds no parameter", d.Provenance.Pointer)
			}
		}
		const unresolved = "error " + diag.UnresolvedRef + " /paths/~1a/get/parameters/"
		assert.ElementsMatch(t, []string{unresolved + "0", unresolved + "1", unresolved + "2", unresolved + "3",
			"error openapi/validation/validation-required-field /x-raw/m"}, got)
	})
}

// TestReferenceObjects_AMergedRefIsAReference pins that a mapping at a
// reference object's position is a reference when it holds a string $ref, a
// `<<` merging one in included, as the document's JSON spells it: the
// parameter, the response and the path item it names lower, and what is
// written beside it, a description or a path item's operation.
func TestReferenceObjects_AMergedRefIsAReference(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters:
        - {<<: {$ref: '#/components/parameters/P'}}
        - {<<: {$ref: '#/components/parameters/Q'}, description: beside}
      responses:
        "200": {<<: {$ref: '#/components/responses/R'}}
  /m: {<<: {$ref: '#/paths/~1b'}, put: {operationId: mput, responses: {"200": {description: ok}}}}
  /b: {get: {responses: {"200": {description: ok}}}}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
    Q: {name: q, in: query, schema: {type: string}}
  responses:
    R: {description: r, content: {application/json: {schema: {type: string}}}}
`}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		assert.Empty(t, placedFindings(diags))
		operationAt(t, doc, "GET", "/m")
		assert.Equal(t, "mput", operationAt(t, doc, "PUT", "/m").Name.Source, "the field beside the merged $ref")
		op, ok := opByName(doc, "a")
		require.True(t, ok)
		require.Len(t, op.Params, 2)
		assert.ElementsMatch(t, []string{"p", "q"}, []string{op.Params[0].Name.Source, op.Params[1].Name.Source})
		assert.Equal(t, ir.TypeID("t/prim/string"), firstContent(t, op).Type.Target, "the response R declares")
	})
}

// TestReferenceObjects_NothingBesideARefIsRead pins that what a reference
// object writes beside its $ref, which OpenAPI ignores, is no reference:
// a $ref there naming nothing draws no finding.
func TestReferenceObjects_NothingBesideARefIsRead(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters:
        - $ref: '#/components/parameters/P'
          schema: {$ref: '#/components/schemas/Ghost'}
          examples: {e: {$ref: '#/components/examples/Nope'}}
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
`}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		assert.Empty(t, placedFindings(diags))
		op, ok := opByName(doc, "a")
		require.True(t, ok)
		require.Len(t, op.Params, 1)
		assert.Equal(t, ir.TypeID("t/prim/string"), op.Params[0].Type.Target, "the parameter P declares")
	})
}

// TestReferenceObjects_AReferenceInsideRawYAMLResolves pins GitHub #1320: each
// reference object inside an object a $ref reaches in raw YAML resolves as one
// in the source's model does, and one that fails is reported where it is
// written.
func TestReferenceObjects_AReferenceInsideRawYAMLResolves(t *testing.T) {
	t.Parallel()
	t.Run("a response", func(t *testing.T) {
		t.Parallel()
		refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {operationId: a, responses: {"200": {$ref: '#/x-shared/R'}}}}
x-shared:
  R:
    description: ok
    headers:
      X-Rate: {$ref: '#/components/headers/Rate'}
    content:
      application/json:
        schema: {type: string}
        examples:
          one: {$ref: '#/components/examples/E'}
components:
  headers:
    Rate: {schema: {type: integer}}
  examples:
    E: {value: x}
`}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
			t.Helper()
			assert.Empty(t, placedFindings(diags))
			op, ok := opByName(doc, "a")
			require.True(t, ok)
			require.Len(t, op.Responses, 1)
			h, ok := headerByWire(op.Responses[0].Headers, "X-Rate")
			require.True(t, ok, "the header lowers")
			assert.Equal(t, ir.TypeID("t/prim/integer"), h.Type.Target)
			assert.Len(t, firstContent(t, op).Examples, 1, "the example lowers")
		})
	})
	t.Run("a path item", func(t *testing.T) {
		t.Parallel()
		refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/x-items/a'}
x-items:
  a:
    get:
      operationId: a
      parameters: [{$ref: '#/components/parameters/P'}, {$ref: '#/components/parameters/Ghost'}]
      responses: {"200": {$ref: '#/components/responses/R'}}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
  responses:
    R: {description: r}
`}.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
			t.Helper()
			assert.Equal(t, []string{"error " + diag.UnresolvedRef + " /x-items/a/get/parameters/1: unresolved $ref " +
				`"#/components/parameters/Ghost": it names nothing in the document it points into`}, placedFindings(diags))
			op, ok := opByName(doc, "a")
			require.True(t, ok)
			require.Len(t, op.Params, 1, "the parameter P declares lowers")
			assert.Len(t, op.Responses, 1, "the response R declares lowers")
		})
	})
}

// TestReferenceObjects_ARepeatedKeyIsReadAtItsLastCopy pins GitHub #1574: a
// reference object's pointer reads a key a mapping writes twice at its last
// copy, as the model does, so what the first holds is never read, and what the
// last holds is reported where it is written.
func TestReferenceObjects_ARepeatedKeyIsReadAtItsLastCopy(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {get: {parameters: [{$ref: '#/x-params/q'}], responses: {'200': {description: ok}}}}
x-params:
  q: {name: q, in: query, schema: {type: object, not: {$ref: '#/components/schemas/Ghost'}}}
x-params:
  q: {name: q, in: query, schema: {type: string}}
`
	const duplicate = "warning openapi/validation/validation-duplicate-key /x-params"
	doc, diags := refSpec{}.compile(t, spec)
	got := sitedFindings(diags)
	assert.Equal(t, []string{duplicate}, got)
	require.Len(t, doc.Services[0].Groups[0].Operations[0].Params, 1)
	assert.Equal(t, ir.TypeID("t/prim/string"), doc.Services[0].Groups[0].Operations[0].Params[0].Type.Target)

	swapped := strings.Replace(spec, "x-params:\n  q: {name: q, in: query, schema: {type: object",
		"x-params:\n  q: {name: q, in: query, schema: {type: string}}\nx-params:\n  q: {name: q, in: query, schema: {type: object", 1)
	swapped = strings.TrimSuffix(swapped, "x-params:\n  q: {name: q, in: query, schema: {type: string}}\n")
	_, diags = refSpec{}.compile(t, swapped)
	got = sitedFindings(diags)
	assert.Equal(t, []string{duplicate, "error " + diag.UnresolvedRef + " /x-params/q/schema/not",
		"info " + diag.ValidationOnlyKeyword + " /x-params/q/schema"}, got)

	firstRefs := strings.Replace(spec, "x-params:\n  q: {name: q, in: query, schema: {type: object, not: {$ref: '#/components/schemas/Ghost'}}}",
		"x-params:\n  q: {$ref: '#/components/parameters/Ghost'}", 1)
	doc, diags = refSpec{}.compile(t, firstRefs)
	got = sitedFindings(diags)
	assert.Equal(t, []string{duplicate}, got, "a $ref the first copy writes is never read")
	require.Len(t, doc.Services[0].Groups[0].Operations[0].Params, 1)
}

// TestReferenceObjects_AnObjectThatCannotBeBuiltIsReported pins a reference
// object landing on raw YAML that no object can be built from, which the
// parser faults on: it is reported at the reference, never lowered as nothing.
func TestReferenceObjects_AnObjectThatCannotBeBuiltIsReported(t *testing.T) {
	t.Parallel()
	refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: '#/x-p/q'}]
      responses: {"200": {$ref: '#/x-r/B'}}
x-p:
  q: {$ref: null}
x-r:
  B: {$ref: null}
`}.inBothOrders(t, func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
		t.Helper()
		got := map[jsontext.Pointer]string{}
		for _, d := range diags {
			if d.Code == diag.Validation {
				got[d.Provenance.Pointer] = d.Message
			}
		}
		const panicked = "parser panicked: runtime error: invalid memory address or nil pointer dereference"
		assert.Equal(t, map[jsontext.Pointer]string{
			"/paths/~1a/get/parameters/0":  "the $ref here reaches #/x-p/q, where no parameter could be built: " + panicked,
			"/paths/~1a/get/responses/200": "the $ref here reaches #/x-r/B, where no response could be built: " + panicked,
		}, got)
	})
}

// TestReferenceObjects_AChainCrossesDocuments pins a chain from the source
// through another document and back: it resolves where it ends, a hop written
// in the other document that fails is reported there, and with no reader each
// $ref naming another document fails at the source.
func TestReferenceObjects_AChainCrossesDocuments(t *testing.T) {
	t.Parallel()
	s := refSpec{spec: `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: 'ext.yaml#/P'}, {$ref: 'ext.yaml#/F'}]
      responses: {"200": {description: ok}}
components:
  parameters:
    Q: {name: q, in: query, schema: {type: integer}}
`, others: map[string]string{"ext.yaml": "P: {$ref: 'root.yaml#/components/parameters/Q'}\nF: {$ref: '#/Missing'}\n"}}
	t.Run("read", func(t *testing.T) {
		t.Parallel()
		s := s
		s.disk = true
		s.inBothOrders(t, func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
			t.Helper()
			require.Len(t, diags, 1)
			assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/F"}, ir.Provenance{Source: diags[0].Provenance.Source,
				Pointer: diags[0].Provenance.Pointer}, "where ext.yaml writes the hop that fails")
			op, ok := opByName(doc, "a")
			require.True(t, ok)
			require.Len(t, op.Params, 1)
			assert.Equal(t, ir.TypeID("t/prim/integer"), op.Params[0].Type.Target, "Q, in the source")
		})
	})
	t.Run("unread", func(t *testing.T) {
		t.Parallel()
		s.inBothOrders(t, func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
			t.Helper()
			const off = `": it names another document, and external references are not read`
			assert.Equal(t, []string{
				"error " + diag.UnresolvedRef + ` /paths/~1a/get/parameters/0: unresolved $ref "ext.yaml#/P` + off,
				"error " + diag.UnresolvedRef + ` /paths/~1a/get/parameters/1: unresolved $ref "ext.yaml#/F` + off,
			}, placedFindings(diags))
		})
	})
}

// TestReferenceObjects_AnEndInTheSourceIsReadThereUnderSelf pins a response
// reference in a document declaring $self (GitHub #863): its fragment names a
// position of the source, so a schema $ref through it resolves to the schema
// the response declares.
func TestReferenceObjects_AnEndInTheSourceIsReadThereUnderSelf(t *testing.T) {
	t.Parallel()
	doc, diags := refSpec{}.compile(t, `openapi: 3.2.0
$self: https://example.com/api/openapi.yaml
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"404": {$ref: '#/components/responses/NotFound'}}}}
components:
  responses:
    NotFound: {description: nf, content: {application/json: {schema: {$ref: '#/components/schemas/X'}}}}
  schemas:
    X: {type: integer}
    S: {$ref: '#/paths/~1a/get/responses/404/content/application~1json/schema'}
`)
	assert.Empty(t, diags)
	s, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/anon/components/responses/NotFound/content/application~1json/schema"), s.Base.Target)
}

// TestReferenceObjects_AnEndInAnotherDocumentIsDeclaredThere pins a schema $ref
// through a parameter reference into another document: the schema is declared
// where that document writes it.
func TestReferenceObjects_AnEndInAnotherDocumentIsDeclaredThere(t *testing.T) {
	t.Parallel()
	doc, diags := refSpec{disk: true, others: map[string]string{
		"ext.yaml": "x-params:\n  q: {name: q, in: query, schema: {type: object, properties: {a: {type: string}}}}\n",
	}}.compile(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {parameters: [{$ref: './ext.yaml#/x-params/q'}], responses: {"200": {description: ok}}}}
components:
  schemas:
    S: {$ref: '#/paths/~1a/get/parameters/0/schema'}
`)
	assert.Empty(t, diags)
	s, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, ids.In("ext.yaml").AnonType("/x-params/q/schema"), s.Base.Target)
}

// TestReferenceObjects_AURLWithASpaceNamesTheDocumentItsEscapeDoes pins GitHub
// #1048: a reference object naming a served document by a URL written with a
// literal space reads the document its "%20" spelling names, and what it names
// lowers where that document declares it. The space, which no URI reference
// admits, draws the rule table's error and nothing else.
func TestReferenceObjects_AURLWithASpaceNamesTheDocumentItsEscapeDoes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, err := w.Write([]byte(`openapi: 3.1.0
info: {title: c, version: "1"}
paths:
  /x:
    get:
      parameters:
        - {name: inline, in: query, schema: {type: object, properties: {i: {type: string}}}}
      responses: {"200": {description: ok}}
`))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	want := ids.In(srv.URL + "/c%20d.yaml").AnonType("/paths/~1x/get/parameters/0/schema")
	for spelling, findings := range map[string][]string{
		"c d.yaml":   {"error openapi/validation/validation-invalid-format /paths/~1p/$ref"},
		"c%20d.yaml": {},
	} {
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "root.yaml", Data: []byte(
			"openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /p: {$ref: '" + srv.URL + "/" + spelling +
				"#/paths/~1x'}\n")}}, compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err, spelling)
		assert.Equal(t, findings, sitedFindings(diags), spelling)
		params := allOperations(doc)[0].Params
		require.Len(t, params, 1, spelling)
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/parameters/0"}, params[0].Provenance, spelling)
		assert.Equal(t, want, params[0].Type.Target, spelling)
	}
}

// TestReferenceObjects_ADocumentPartIsReadWhereTheSourceLies pins a reference
// object's document part read as a schema $ref's is, against where the source
// lies though it declares $self, so the two kinds read one document alike
// (GitHub #863, docs/compilers/openapi.md#self-uri).
func TestReferenceObjects_ADocumentPartIsReadWhereTheSourceLies(t *testing.T) {
	t.Parallel()
	doc, diags := refSpec{disk: true, others: map[string]string{"other.yaml": `openapi: 3.2.0
info: {title: o, version: "1"}
paths: {}
components:
  responses:
    R: {description: r}
  schemas:
    OS: {type: string}
`}}.compile(t, `openapi: 3.2.0
$self: https://example.com/api/openapi.yaml
info: {title: t, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200": {$ref: "other.yaml#/components/responses/R"}
        "201": {description: s, content: {application/json: {schema: {$ref: "other.yaml#/components/schemas/OS"}}}}
`)
	assert.Empty(t, sitedFindings(diags))
	op, ok := opByName(doc, "a")
	require.True(t, ok)
	require.Len(t, op.Responses, 2)
	assert.Equal(t, ir.Docs{Description: "r"}, op.Responses[0].Docs, "the response other.yaml declares")
}
