// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// The pieces of the param-choices fixture, each indented as it sits in the
// spec: the five parameters, and the two choices the key states.
const (
	choiceParamsText = `        - {name: parent_id, in: query, schema: {type: string}}
        - {name: parent_slug, in: query, schema: {type: string}}
        - {name: parent_external_id, in: query, schema: {type: string}}
        - {name: before, in: query, schema: {type: string}}
        - {name: after, in: query, schema: {type: string}}
`
	parentChoiceText = `        parent:
          optional: false
          variants:
            by_id: [parent_id]
            by_external_id: [parent_slug, parent_external_id]
`
	cursorChoiceText = `        cursor:
          optional: true
          variants:
            forward: [after]
            backward: [before]
`
)

// choicesSpec is a whole document: listResources declares params under its
// parameters, the path item declares pathItemParams, and the key written as
// key holds groups.
func choicesSpec(key, pathItemParams, params, groups string) string {
	return fmt.Sprintf(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /resources:
%s    get:
      operationId: listResources
      parameters:
%s      %s:
%s      responses: {"200": {description: ok}}
`, pathItemParams, params, key, groups)
}

// choicesFixture is param-choices.yaml as a document.
func choicesFixture() string {
	return choicesSpec(choicesKey, "", choiceParamsText, parentChoiceText+cursorChoiceText)
}

// compileChoices compiles spec under opts and returns the document and every
// finding, failing on a refusal.
func compileChoices(t *testing.T, spec string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// unpromoted returns the findings of diags that report a promotion key stating
// nothing usable.
func unpromoted(diags []ir.Diagnostic) []ir.Diagnostic {
	return slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool {
		return d.Code != "openapi/unpromoted-extension"
	})
}

// TestParamChoices_FillsChoices compiles the fixture and holds the operation to
// the facts of the conformance case, through the compiler's public face.
func TestParamChoices_FillsChoices(t *testing.T) {
	t.Parallel()
	doc, diags := compileChoices(t, choicesFixture(), openapi.Options{})
	assertParamChoices(t, doc, diags)
}

// TestParamChoices_ChangesNothingElse compiles the fixture by default and with
// promotion switched off, and requires the two documents to differ in the
// operation's paramChoices and its inferred marker and nowhere else: the
// parameters stay declared, in order, each bound as it was.
func TestParamChoices_ChangesNothingElse(t *testing.T) {
	t.Parallel()
	on, _ := compileChoices(t, choicesFixture(), openapi.Options{})
	off, _ := compileChoices(t, choicesFixture(),
		openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}})

	onOp, offOp := listResources(t, on), listResources(t, off)
	assert.Equal(t, declaredChoiceParams(), opParamIDs(onOp))
	assert.Equal(t, declaredChoiceParams(), opParamIDs(offOp))
	assert.Equal(t, offOp.Bindings, onOp.Bindings, "no binding is rewritten")
	assert.Equal(t, offOp.Params, onOp.Params, "no parameter is rewritten")
	assert.NotEmpty(t, onOp.ParamChoices)
	assert.Empty(t, offOp.ParamChoices)
	assert.Equal(t, ir.Inferred("extension-promotion"), onOp.Provenance.Inferred)
	assert.Empty(t, offOp.Provenance.Inferred)

	// What is left once those two members are set aside is the same document.
	opOn := &on.Services[0].Groups[0].Operations[0]
	opOn.ParamChoices = nil
	opOn.Provenance.Inferred = ""
	assert.Empty(t, cmp.Diff(irtest.Encoded(t, off), irtest.Encoded(t, on)), "the promotion writes nothing else")
}

// TestParamChoices_SourceOrderDecidesNothing compiles the fixture with the
// choices written the other way round, each variants mapping reversed and the
// five parameters declared in reverse, and holds it to the choices of the
// fixture and to the same findings, none.
func TestParamChoices_SourceOrderDecidesNothing(t *testing.T) {
	t.Parallel()
	reversedParams := `        - {name: after, in: query, schema: {type: string}}
        - {name: before, in: query, schema: {type: string}}
        - {name: parent_external_id, in: query, schema: {type: string}}
        - {name: parent_slug, in: query, schema: {type: string}}
        - {name: parent_id, in: query, schema: {type: string}}
`
	cursorFirst := `        cursor:
          optional: true
          variants:
            backward: [before]
            forward: [after]
`
	parentSecond := `        parent:
          optional: false
          variants:
            by_external_id: [parent_slug, parent_external_id]
            by_id: [parent_id]
`
	doc, diags := compileChoices(t, choicesSpec(choicesKey, "", reversedParams, cursorFirst+parentSecond), openapi.Options{})
	assert.Empty(t, diags)
	op := listResources(t, doc)
	assert.Equal(t, paramChoicesFilled(), op.ParamChoices)

	want := declaredChoiceParams()
	slices.Reverse(want)
	assert.Equal(t, want, opParamIDs(op), "the parameters keep the order they are declared in")
	assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
}

// choiceHole is one way the fixture's parent choice is written badly: the
// parent block and parameters it replaces, where the finding is reported
// relative to the key, and a phrase the message holds.
type choiceHole struct {
	name   string
	parent string
	params string
	at     string
	why    string
}

// TestParamChoices_LeavesAChoiceItCannotRead changes the parent choice of the
// fixture in each way the convention can be broken. The cursor choice stays
// written well, so it is filled and the operation is marked; the parent choice
// is filled whole or not at all, and the one finding is reported at the most
// specific place the document wrote it.
func TestParamChoices_LeavesAChoiceItCannotRead(t *testing.T) {
	t.Parallel()
	const parentIDVariant = "            by_id: [parent_id]\n"
	const secondVariant = "            by_external_id: [parent_slug, parent_external_id]\n"
	const head = "        parent:\n"
	for _, tc := range []choiceHole{
		{
			name: "a choice that is a list", parent: head[:len(head)-1] + " [by_id]\n", params: choiceParamsText,
			at: "/parent", why: `holds a list for choice "parent", not an object`,
		},
		{
			name: "no optional", parent: head + "          variants:\n" + parentIDVariant + secondVariant,
			params: choiceParamsText, at: "/parent", why: `states no boolean "optional" for choice "parent"`,
		},
		{
			name: "optional that is no boolean", parent: head + "          optional: \"no\"\n          variants:\n" +
				parentIDVariant + secondVariant,
			params: choiceParamsText, at: "/parent/optional", why: `states no boolean "optional" for choice "parent"`,
		},
		{
			name: "one variant", parent: head + "          optional: false\n          variants:\n" + parentIDVariant,
			params: choiceParamsText, at: "/parent/variants", why: `states fewer than two variants for choice "parent"`,
		},
		{
			name: "a variant naming nothing", parent: head + "          optional: false\n          variants:\n" +
				"            by_id: []\n" + secondVariant,
			params: choiceParamsText, at: "/parent/variants/by_id", why: `names no parameter in variant "by_id"`,
		},
		{
			name: "a variant that is no list", parent: head + "          optional: false\n          variants:\n" +
				"            by_id: parent_id\n" + secondVariant,
			params: choiceParamsText, at: "/parent/variants/by_id", why: `names no parameter in variant "by_id"`,
		},
		{
			name: "a name the operation does not declare", parent: head + "          optional: false\n          variants:\n" +
				"            by_id: [nope]\n" + secondVariant,
			params: choiceParamsText, at: "/parent/variants/by_id/0",
			why: `names parameter "nope", which the operation does not declare`,
		},
		{
			name: "a name twice in a variant", parent: head + "          optional: false\n          variants:\n" +
				"            by_id: [parent_id, parent_id]\n" + secondVariant,
			params: choiceParamsText, at: "/parent/variants/by_id/1",
			why: `names parameter "parent_id" twice in variant "by_id"`,
		},
		{
			name: "a required parameter some variant lacks", parent: parentChoiceText,
			params: `        - {name: parent_id, in: query, required: true, schema: {type: string}}
` + choiceParamsText[len("        - {name: parent_id, in: query, schema: {type: string}}\n"):],
			at: "/parent", why: `names parameter "parent_id", which every request carries, in some variants only`,
		},
		{
			name: "a name two locations share", parent: parentChoiceText,
			params: choiceParamsText + "        - {name: parent_id, in: header, schema: {type: string}}\n",
			at:     "/parent/variants/by_id/0", why: "declares in 2 locations",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileChoices(t, choicesSpec(choicesKey, "", tc.params, tc.parent+cursorChoiceText), openapi.Options{})

			op := listResources(t, doc)
			assert.Equal(t, paramChoicesFilled()[:1], op.ParamChoices, "the cursor choice alone is filled")
			assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
			assert.Contains(t, op.Unmodeled, "openapi:"+choicesKey, "the key is kept whole")

			found := unpromoted(diags)
			require.Len(t, found, 1, "one finding for the one choice: %v", diags)
			assert.Equal(t, ir.SeverityInfo, found[0].Severity)
			assert.Equal(t, choicesPointer+tc.at, string(found[0].Provenance.Pointer))
			assert.Contains(t, found[0].Message, tc.why)
			assert.Contains(t, found[0].Message, `choice "parent" is not filled`)
		})
	}
}

// TestParamChoices_ReadsNothingOfAKeyThatIsNoObject holds the whole-key rows:
// a key whose value is a list of lists, which is how the issue wrote it and no
// convention does, fills nothing and is reported at the key; an empty object
// states no choice, fills nothing and is reported nowhere. Neither marks the
// operation, and both keep the key.
func TestParamChoices_ReadsNothingOfAKeyThatIsNoObject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		groups string
		want   int
	}{
		{"a list of lists", "        - [parent_id]\n        - [parent_slug]\n", 1},
		{"an empty object", "        {}\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileChoices(t, choicesSpec(choicesKey, "", choiceParamsText, tc.groups), openapi.Options{})
			op := listResources(t, doc)
			assert.Empty(t, op.ParamChoices)
			assert.Empty(t, op.Provenance.Inferred, "nothing is filled, so nothing is marked")
			assert.Equal(t, declaredChoiceParams(), opParamIDs(op))
			assert.Contains(t, op.Unmodeled, "openapi:"+choicesKey)

			found := unpromoted(diags)
			require.Len(t, found, tc.want, "%v", diags)
			assert.Len(t, diags, tc.want, "no other finding")
			if tc.want == 0 {
				return
			}
			assert.Equal(t, ir.SeverityInfo, found[0].Severity)
			assert.Equal(t, choicesPointer, string(found[0].Provenance.Pointer))
			assert.Contains(t, found[0].Message, "is a list, not an object of named choices")
			assert.Contains(t, found[0].Message, "no choice is filled")
		})
	}
}

// TestParamChoices_FollowsThePolicy holds the target to the promotion policy:
// switched off, or left out of a mapping, it fills nothing and the key stays
// verbatim with no finding; mapped to another key, it reads that key.
func TestParamChoices_FollowsThePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		key  string
		opts openapi.Options
		want []ir.ParamChoice
	}{
		{
			name: "promotion switched off", key: choicesKey,
			opts: openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}},
		},
		{
			name: "a mapping that leaves the target out", key: choicesKey,
			opts: openapi.Options{Promotions: openapi.ExtensionPromotions{
				Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
					openapi.TargetStability: {{Key: "x-stability"}},
				},
			}},
		},
		{
			name: "a mapping naming another key", key: "x-choices",
			opts: openapi.Options{Promotions: openapi.ExtensionPromotions{
				Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
					openapi.TargetParamChoices: {{Key: "x-choices"}},
				},
			}},
			want: paramChoicesFilled(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := choicesSpec(tc.key, "", choiceParamsText, parentChoiceText+cursorChoiceText)
			doc, diags := compileChoices(t, spec, tc.opts)
			assert.Empty(t, diags)

			op := listResources(t, doc)
			assert.Equal(t, tc.want, op.ParamChoices)
			assert.Contains(t, op.Unmodeled, "openapi:"+tc.key, "the key stays verbatim whatever is filled")
			if tc.want == nil {
				assert.Empty(t, op.Provenance.Inferred)
				return
			}
			assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
		})
	}
}

// TestParamChoices_ResolvesAPathItemsParameter declares parent_id on the path
// item instead of the operation. The choice names it all the same, by the ID
// the operation's own parameter has, and the choices are the fixture's.
func TestParamChoices_ResolvesAPathItemsParameter(t *testing.T) {
	t.Parallel()
	const first = "        - {name: parent_id, in: query, schema: {type: string}}\n"
	pathItem := "    parameters:\n      - {name: parent_id, in: query, schema: {type: string}}\n"
	require.Contains(t, choiceParamsText, first)
	spec := choicesSpec(choicesKey, pathItem, choiceParamsText[len(first):], parentChoiceText+cursorChoiceText)

	doc, diags := compileChoices(t, spec, openapi.Options{})
	assert.Empty(t, diags)
	op := listResources(t, doc)
	assert.Equal(t, paramChoicesFilled(), op.ParamChoices)
	assert.Contains(t, opParamIDs(op), choiceParamID("parent_id"), "the path item's parameter is the operation's")
	assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
}
