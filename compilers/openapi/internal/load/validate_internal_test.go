package load

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// TestDeclaredVersion_ReadsTheTreeNotTheModel pins which version the table is
// held to: the scalar under "openapi", through an alias or a merge key, and none
// for a document that writes no such scalar.
func TestDeclaredVersion_ReadsTheTreeNotTheModel(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ src, want string }{
		"written":        {"openapi: 3.1.0\n", "3.1.0"},
		"a number":       {"openapi: 3.1\n", "3.1"},
		"through alias":  {"v: &v 3.0.3\nopenapi: *v\n", "3.0.3"},
		"through merge":  {"<<: {openapi: 3.2.0}\n", "3.2.0"},
		"a list":         {"openapi: [3.1.0]\n", ""},
		"absent":         {"info: {}\n", ""},
		"not a mapping":  {"- openapi\n", ""},
		"an empty tree":  {"", ""},
		"a null version": {"openapi: ~\n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(c.src), &root))
			assert.Equal(t, c.want, declaredVersion(&root))
		})
	}
}

// TestJudgeSource_PlacesEachFindingAtItsNode pins that a finding is placed as
// the source's own always were: at the node it is about, the object for a
// missing field and the value for a wrong one, with that node's position and
// pointer in the tree, and no position or pointer for a node a merge key
// supplies.
func TestJudgeSource_PlacesEachFindingAtItsNode(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {version: "1", description: [d]}
paths: {}
components:
  schemas:
    S:
      <<: {minLength: -1}
`
	root := decodeRoot(t, src)
	tree := newSourceTree(4, overlay.Origin{}, root)

	got := judgeSource(root, tree, 0)

	type placed struct {
		Code, Pointer string
		Line, Column  int
	}
	diags := make([]placed, 0, len(got.diags))
	for _, d := range got.diags {
		diags = append(diags, placed{d.Code, string(d.Provenance.Pointer), d.Provenance.Position.Line, d.Provenance.Position.Column})
		assert.Equal(t, 4, d.Provenance.Source)
	}
	assert.Empty(t, cmp.Diff([]placed{
		{diag.Validation + "/validation-required-field", "/info", 2, 7},
		{diag.Validation + "/validation-type-mismatch", "/info/description", 2, 35},
		{diag.Validation + "/validation-type-mismatch", "", 7, 23},
	}, diags))
}

// TestJudgeSource_RecordsTheObjectsItRead pins what the source's check reports
// of what it read, by the mapping and the kind: a $ref standing in an object's
// place is none, and an alias is the mapping it names.
func TestJudgeSource_RecordsTheObjectsItRead(t *testing.T) {
	t.Parallel()
	root := decodeRoot(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    P: &p {name: p, in: query, schema: {type: string}}
    Q: *p
    R: {$ref: '#/components/parameters/P'}
`)
	top := nodeAt(t, root, "components", "parameters")
	p := nodeAt(t, top, "P")

	got := judgeSource(root, newSourceTree(0, overlay.Origin{}, root), 0)

	assert.Empty(t, got.diags)
	assert.True(t, got.seen[objectKey{node: p, kind: oas.Parameter}], "the parameter, once however it is reached")
	assert.False(t, got.seen[objectKey{node: p, kind: oas.Header}], "and not as another kind")
	assert.False(t, got.seen[objectKey{node: nodeAt(t, top, "R"), kind: oas.Parameter}], "a reference object is no parameter")
}

// nodeAt returns the node at the keys below n, which must hold them.
func nodeAt(t *testing.T, n *yaml.Node, keys ...string) *yaml.Node {
	t.Helper()
	cur := n
	if cur.Kind == yaml.DocumentNode {
		cur = cur.Content[0]
	}
	for _, key := range keys {
		var next *yaml.Node
		for i := 0; i+1 < len(cur.Content); i += 2 {
			if cur.Content[i].Value == key {
				next = cur.Content[i+1]
			}
		}
		require.NotNil(t, next, "key %q", key)
		cur = next
	}
	return cur
}

// TestJudgeSource_AnOverlayOwnsWhatItWrote pins the attribution of a finding
// about a node an overlay introduced: the overlay's index, and the node's
// pointer, where a node the overlay left alone keeps the source's.
func TestJudgeSource_AnOverlayOwnsWhatItWrote(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T}\npaths: {}\n"
	opts := overlayOptions("  - target: $.info\n    update: {description: [d]}\n")

	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), opts)

	require.NoError(t, err)
	got := map[jsontext.Pointer]int{}
	for _, d := range diags {
		got[d.Provenance.Pointer] = d.Provenance.Source
	}
	assert.Equal(t, map[jsontext.Pointer]int{"/info": 0, "/info/description": 1}, got,
		"the missing version is the source's to answer for, the description the overlay's")
}

// TestSourceTree_LocatorsAnswerAtTheLastNodeTheyReach pins that a pointer
// leading nowhere is placed at the last node it reached, and that a tree
// holding no node places every finding at the reference that reached it.
func TestSourceTree_LocatorsAnswerAtTheLastNodeTheyReach(t *testing.T) {
	t.Parallel()
	root := decodeRoot(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n")
	tree := newSourceTree(0, overlay.Origin{}, root)

	for _, locate := range []oas.Locator{tree.locatorIn(root), tree.reachedLocator(root)} {
		got := locate("/info/nowhere/at/all")
		assert.Equal(t, jsontext.Pointer("/info"), got.Pointer, "placed at /info, the last node the pointer reached")
	}
	assert.Equal(t, ir.Provenance{Source: 0}, tree.locatorIn(&yaml.Node{Kind: yaml.DocumentNode})("/x"), "an empty tree is placed by source alone")

	assert.False(t, sourceTree{}.has(root), "a tree that holds nothing holds no node")
	assert.True(t, tree.has(root.Content[0]), "and one holds the nodes it indexed")
	foreign := sourceTree{}.reachedLocator(root)("/info")
	assert.Equal(t, ir.Provenance{Source: foreign.Source, Position: foreign.Position}, foreign)
	assert.Equal(t, 2, foreign.Position.Line)
	assert.False(t, holds(ir.Diagnostic{Provenance: foreign}), "marked foreign")
}

// TestPlaceAt_PutsForeignFindingsAtTheReference pins where a finding about an
// object a reference reaches is reported: one placed in a document stays where
// it is, and one marked foreign goes to the reference with the place it lies
// in named in its message.
func TestPlaceAt_PutsForeignFindingsAtTheReference(t *testing.T) {
	t.Parallel()
	site := ir.Provenance{Source: 2, Pointer: "/paths/~1a"}
	held := ir.Diagnostic{Message: "held", Provenance: ir.Provenance{Source: 2, Pointer: "/x-a", Position: ir.Position{Line: 3, Column: 4}}}
	other := ir.Diagnostic{Message: "other", Provenance: ir.Provenance{Source: foreign, Position: ir.Position{Line: 5, Column: 6}}}

	assert.Equal(t, held, placeAt(held, site, "other.yaml"))
	moved := placeAt(other, site, "other.yaml")
	assert.Equal(t, site, moved.Provenance)
	assert.Equal(t, "other, at 5:6 of other.yaml", moved.Message)
}

// TestIdentity_NamesTheDocumentOfAForeignFinding pins what makes two findings
// one: for one marked foreign, taken before it is placed at a reference, the
// finding with the document it lies in; for one placed in a document, the
// finding as placed, whichever position read it.
func TestIdentity_NamesTheDocumentOfAForeignFinding(t *testing.T) {
	t.Parallel()
	other := ir.Diagnostic{Message: "m", Provenance: ir.Provenance{Source: foreign, Position: ir.Position{Line: 1, Column: 2}}}
	held := ir.Diagnostic{Message: "m", Provenance: ir.Provenance{Source: 1, Pointer: "/x"}}
	readAt := held
	readAt.Message = "m; read at /y"

	assert.Equal(t, "m of a.yaml", identity(other, "a.yaml").Message)
	assert.NotEqual(t, identity(other, "a.yaml"), identity(other, "b.yaml"))
	assert.Equal(t, held, identity(held, "a.yaml"))
	assert.Equal(t, held, identity(readAt, "a.yaml"), "the position that read it is no part of it")
}

// TestLoad_WhatAnAliasMountsIsReportedOnce pins that a defect in a mapping two
// keys name through an alias is one finding, at the node it is written at.
func TestLoad_WhatAnAliasMountsIsReportedOnce(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  parameters:\n" +
		"    A: &p {name: a, in: sideways, schema: {type: string}}\n    B: *p\n"

	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})

	require.NoError(t, err)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, 6, diags[0].Provenance.Position.Line)
}

// TestJudgeSource_ReadsEveryObjectTheCompilerAdmits pins the check's bounds to
// what a load admits: its depth to the nesting the pre-parse scan admits, so a
// schema 600 items deep is judged at its bottom, and its work to nodesAdmitted,
// the node and alias budgets together, or the walker's default where either is
// unbounded. A budget the check does cross is reported as an error.
func TestJudgeSource_ReadsEveryObjectTheCompilerAdmits(t *testing.T) {
	t.Parallel()
	assert.Equal(t, scan.MaxNesting, oas.MaxDepth)

	const depth = 600
	schema := strings.Repeat("{type: array, items: ", depth) + "{type: bogus}" + strings.Repeat("}", depth)
	src := compilers.Source{Path: "deep.yaml", Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: '1'}\n" +
		"paths: {}\ncomponents:\n  schemas:\n    A: " + schema + "\n")}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	bottom := jsontext.Pointer("/components/schemas/A" + strings.Repeat("/items", depth) + "/type")
	var at []jsontext.Pointer
	for _, d := range diags {
		assert.NotEqual(t, diag.BudgetExceeded, d.Code, "%+v", d)
		if d.Code == diag.Validation+"/validation-allowed-values" {
			at = append(at, d.Provenance.Pointer)
		}
	}
	assert.Equal(t, []jsontext.Pointer{bottom}, at)

	assert.Equal(t, 7, nodesAdmitted(Options{MaxSourceNodes: 3, MaxAliasSurplus: 4}))
	assert.Zero(t, nodesAdmitted(Options{MaxSourceNodes: 3}))
	assert.Zero(t, nodesAdmitted(Options{MaxAliasSurplus: 4}))

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\npaths: {/a: {}, /b: {}}\n"), &root))
	got := judgeSource(&root, newSourceTree(0, overlay.Origin{}, &root), 2)
	require.Len(t, got.diags, 1, "%+v", got.diags)
	assert.Equal(t, diag.BudgetExceeded, got.diags[0].Code)
	assert.Equal(t, ir.SeverityError, got.diags[0].Severity)
}

// TestJudgeSource_RefusesTheAnchorsNoReferenceCanName holds the table's grammar
// for an anchor's name to the binder's, which reads a $ref's plain-name
// fragment: the table refuses a $anchor exactly where a $ref to its name
// resolves to nothing.
func TestJudgeSource_RefusesTheAnchorsNoReferenceCanName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"a", "_a", "Z9", "a.b-c_d", "1a", "-a", ".a", "_", "a:b", "a/b", "é"} {
		spec := "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
			"    A: {$anchor: '" + name + "', type: string}\n    B: {$ref: '#" + name + "'}\n"
		_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "a.yaml", Data: []byte(spec)}, Options{})
		require.NoError(t, err)
		refused, unresolved := false, false
		for _, d := range diags {
			refused = refused || d.Provenance.Pointer == "/components/schemas/A/$anchor"
			unresolved = unresolved || d.Code == diag.UnresolvedRef
		}
		assert.Equal(t, unresolved, refused, "%q: %+v", name, diags)
	}
}

// TestLoad_ValidationErrorsBecomeDiagnostics pins that what the rule table
// finds reaches the caller as a diagnostic at the node it is about: responses
// written as a string is a field of the wrong type, placed by pointer and by
// position.
func TestLoad_ValidationErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "bad.yaml", Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {/x: {get: {responses: \"nope\"}}}\n")}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.Validation+"/validation-type-mismatch", diags[0].Code)
	assert.Equal(t, ir.Provenance{Pointer: "/paths/~1x/get/responses", Position: ir.Position{Line: 3, Column: 31}},
		diags[0].Provenance)
}

// TestLoad_OperationIDAndParameterUniquenessAreOthersToReport pins the two
// rules the table states and leaves to others: a repeated operationId is judged
// by the service lowering once it has seen every claim (GitHub #502), a repeated
// parameter by the load phase's check of every parameter list, which reports it
// once. The table adds no report of either, for the alias form the library read
// as a repeat or for a true one.
func TestLoad_OperationIDAndParameterUniquenessAreOthersToReport(t *testing.T) {
	t.Parallel()
	const (
		aliasedOperationID = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`
		repeatedOperationID = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {operationId: dup, responses: {"200": {description: ok}}}}
  /b: {get: {operationId: dup, responses: {"200": {description: ok}}}}
`
		duplicateParameter = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
        - {name: q, in: query, schema: {type: integer}}
      responses: {"200": {description: ok}}
`
	)
	for _, spec := range []string{aliasedOperationID, repeatedOperationID, duplicateParameter} {
		_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
		require.NoError(t, err)
		for _, d := range diags {
			assert.False(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "no finding of the table: %+v", d)
		}
	}
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(duplicateParameter)}, Options{})
	require.NoError(t, err)
	assert.Equal(t, 1, countErrorsAt(diags, diag.DuplicateParameter), "%+v", diags)
}

// TestLoad_AnUnownedRuleIsReported is the control for the test above: the table
// leaves two rules to others, not every one inside an operation, so a path
// parameter not marked required must still surface.
func TestLoad_AnUnownedRuleIsReported(t *testing.T) {
	t.Parallel()
	const optionalPathParameter = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a/{id}:
    get:
      parameters:
        - {name: id, in: path, schema: {type: string}}
      responses: {"200": {description: ok}}
`
	src := compilers.Source{Path: "spec.yaml", Data: []byte(optionalPathParameter)}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	assert.Equal(t, 1, countErrorsAt(diags, diag.Validation+"/validation-required-field"),
		"a rule the table does not leave to others must still surface: %+v", diags)
}

// TestLoad_AFindingOnASourceNodeCarriesItsPointer pins that a finding on a node
// of the source is placed at the node's JSON pointer as well as its position
// (GitHub #803), the source it came from included. A node written under a
// `<<` merge key keeps its position and has no pointer: the pairs it merges
// are held at other pointers. A mapping that merely holds a merge keeps its own.
func TestLoad_AFindingOnASourceNodeCarriesItsPointer(t *testing.T) {
	t.Parallel()
	const header = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	const (
		required = "openapi/validation/validation-required-field"
		allowed  = "openapi/validation/validation-allowed-values"
		inline   = header + "paths:\n  /x:\n    get:\n      parameters:\n        - %s\n      responses: {\"200\": {description: ok}}\n"
	)
	for _, c := range []struct {
		name, spec, marker, code string
		pointer                  jsontext.Pointer
	}{
		{"a mapping", header + "paths: {}\ncomponents:\n  securitySchemes:\n    s: {type: oauth2}\n",
			"{type: oauth2}", required, "/components/securitySchemes/s"},
		{"a value", fmt.Sprintf(inline, "{name: q, in: sideways, schema: {type: string}}"),
			"sideways", allowed, "/paths/~1x/get/parameters/0/in"},
		{"a value under a merge key", fmt.Sprintf(inline, "<<: {name: q, in: sideways, schema: {type: string}}"),
			"sideways", allowed, ""},
		{"a mapping holding a merge key", header + "paths: {}\ncomponents:\n  securitySchemes:\n    s:\n      <<: {type: oauth2}\n",
			"<<: {type: oauth2}", required, "/components/securitySchemes/s"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			line, col := markerPosition(t, c.spec, c.marker)

			_, diags, err := Load(t.Context(), 2, openapitest.SourceOf(c.spec), Options{})

			require.NoError(t, err)
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, c.code, diags[0].Code)
			assert.Empty(t, cmp.Diff(ir.Provenance{Source: 2, Pointer: c.pointer, Position: ir.Position{Line: line, Column: col}},
				diags[0].Provenance))
		})
	}
}
