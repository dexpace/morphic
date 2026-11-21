// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// dynHead opens an OpenAPI 3.1 document whose components hold schemas, which
// the schemas a test writes follow.
const dynHead = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n"

// dynPet is the schema the reference twins below name by pointer and by anchor.
const dynPet = "    Pet: {$anchor: pet, type: object, properties: {name: {type: string}}}\n"

// dynUse is what the node a component owns over what its reference names
// holds: the Model over an object, declaring no member, or the Scalar alias.
type dynUse struct {
	Base        *ir.TypeRef
	Constraints *ir.Constraints
	Unmodeled   ir.Unmodeled
}

// dynScalar returns the use the component named name lowers to.
func dynScalar(t *testing.T, doc *ir.Document, name string) dynUse {
	t.Helper()
	switch n := doc.Types[componentID(name)].(type) {
	case *ir.Scalar:
		return dynUse{Base: n.Base, Constraints: n.Constraints, Unmodeled: n.Unmodeled}
	case *ir.Model:
		require.Empty(t, n.Properties, "%s declares no member of its own", name)
		return dynUse{Base: n.Base, Constraints: n.Constraints, Unmodeled: n.Unmodeled}
	default:
		require.Failf(t, "no use", "%s lowers to %T, no use of what it names", name, n)
		return dynUse{}
	}
}

// dynBaseChainTerminates walks the base chain of the scalar named from and
// fails if it returns to a type it has passed.
func dynBaseChainTerminates(t *testing.T, doc *ir.Document, from ir.TypeID) {
	t.Helper()
	seen := map[ir.TypeID]bool{}
	for cur := from; ; {
		require.False(t, seen[cur], "the base chain of %s returns to %s", from, cur)
		seen[cur] = true
		sc, ok := doc.Types[cur].(*ir.Scalar)
		if !ok || sc.Base == nil {
			return
		}
		cur = sc.Base.Target
	}
}

// TestCompile_ADynamicRefNamingNoDynamicAnchorReadsAsTheRefItSpells pins that
// a $dynamicRef is dynamic only where it names a $dynamicAnchor: written as a
// pointer or as a plain name nothing declares, it resolves as the $ref of the
// same text does, with nothing kept and nothing reported.
func TestCompile_ADynamicRefNamingNoDynamicAnchorReadsAsTheRefItSpells(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", dynHead+dynPet+
		"    ByPointer: {$dynamicRef: '#/components/schemas/Pet'}\n"+
		"    ByAnchor: {$dynamicRef: '#pet'}\n"+
		"    ByRef: {$ref: '#pet'}\n", openapi.Options{})
	require.NotNil(t, doc)
	assert.Empty(t, diags, "a reference that resolves draws nothing")

	byRef := dynScalar(t, doc, "ByRef")
	require.NotNil(t, byRef.Base)
	require.Equal(t, componentID("Pet"), byRef.Base.Target, "the control resolves")
	for _, name := range []string{"ByPointer", "ByAnchor"} {
		sc := dynScalar(t, doc, name)
		require.NotNil(t, sc.Base, name)
		assert.Empty(t, cmp.Diff(byRef.Base, sc.Base), "%s has the base its $ref twin has", name)
		assert.Empty(t, sc.Unmodeled, "%s keeps nothing", name)
	}
}

// TestCompile_ADynamicRefNamingADynamicAnchorStaysDynamic pins that a
// reference naming a name a $dynamicAnchor declares is still expanded, with
// the info it draws today.
func TestCompile_ADynamicRefNamingADynamicAnchorStaysDynamic(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", dynHead+
		"    Pet: {$dynamicAnchor: pet, type: object}\n    Dyn: {$dynamicRef: '#pet'}\n", openapi.Options{})
	require.NotNil(t, doc)
	sc := dynScalar(t, doc, "Dyn")
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("Pet"), sc.Base.Target)
	assert.Empty(t, sc.Unmodeled)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, "openapi/dynamic-ref-expanded", diags[0].Code)
	assert.EqualValues(t, "/components/schemas/Dyn/$dynamicRef", diags[0].Provenance.Pointer)
	assert.Equal(t, `$dynamicRef expanded to "type/openapi/components/schemas/Pet", the one matching $dynamicAnchor in this document`,
		diags[0].Message)
}

// TestCompile_ADynamicRefResolvingNowhereIsAnUnresolvedRef pins that the
// reference the $ref twin fails is a failure with the keyword it was written
// with, and lowers the top type, with no info and nothing kept.
func TestCompile_ADynamicRefResolvingNowhereIsAnUnresolvedRef(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", dynHead+dynPet+
		"    Miss: {$dynamicRef: '#/components/schemas/Nope'}\n"+
		"    MissRef: {$ref: '#/components/schemas/Nope'}\n", openapi.Options{})
	require.NotNil(t, doc)

	twin := byCode(diags, "openapi/unresolved-ref")
	var ref, dyn []ir.Diagnostic
	for _, diagnostic := range twin {
		switch diagnostic.Provenance.Pointer {
		case "/components/schemas/MissRef":
			ref = append(ref, diagnostic)
		case "/components/schemas/Miss":
			dyn = append(dyn, diagnostic)
		}
	}
	require.Len(t, ref, 1, "the $ref twin draws its error: %+v", diags)
	require.Len(t, dyn, 1, "the $dynamicRef draws the same: %+v", diags)
	assert.Equal(t, ir.SeverityError, dyn[0].Severity)
	assert.Equal(t, strings.Replace(ref[0].Message, `unresolved $ref "`, `unresolved $dynamicRef "`, 1), dyn[0].Message,
		"the reason is the twin's, worded with its own keyword")
	assert.True(t, strings.HasPrefix(dyn[0].Message, `unresolved $dynamicRef "#/components/schemas/Nope": `), dyn[0].Message)

	sc := dynScalar(t, doc, "Miss")
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("type/primitive/any"), sc.Base.Target)
	assert.Empty(t, sc.Unmodeled, "the binding's error stands alone")
	for _, diagnostic := range diags {
		assert.NotEqual(t, "openapi/dynamic-ref-expanded", diagnostic.Code)
		if diagnostic.Code == "openapi/degraded-construct" {
			assert.NotContains(t, string(diagnostic.Provenance.Pointer), "Miss/$dynamicRef", "no info beside the error")
		}
	}
}

// TestCompile_ADynamicRefNamingAnotherDocumentReadsAsARef pins that a value
// the same document does not hold is read as a $ref reads it: read where
// external references are allowed, refused as the $ref twin is where they are
// not.
func TestCompile_ADynamicRefNamingAnotherDocumentReadsAsARef(t *testing.T) {
	t.Parallel()
	const other = "X: {type: string}\n"
	root := dynHead + "    Ext: {$dynamicRef: 'other.yaml#/X'}\n"

	dir := t.TempDir()
	layout(t, dir, map[string]string{"main.yaml": root, "other.yaml": other})
	doc, diags := compileDir(t, dir, openapi.Options{})
	require.NotNil(t, doc)
	assert.Empty(t, diags)
	sc := dynScalar(t, doc, "Ext")
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("type/anonymous@other.yaml/X"), sc.Base.Target)
	assert.Empty(t, sc.Unmodeled)

	// Outside the source's directory, which a read leaves only where external
	// references are allowed.
	refused := t.TempDir()
	layout(t, refused, map[string]string{"other.yaml": other, "in/keep.yaml": "{}\n"})
	_, diags = compileOnDisk(t, filepath.Join(refused, "in", "spec.yaml"),
		dynHead+"    Ext: {$dynamicRef: '../other.yaml#/X'}\n    ExtRef: {$ref: '../other.yaml#/X'}\n", false)
	var ref, dyn []ir.Diagnostic
	for _, diagnostic := range byCode(diags, "openapi/unresolved-ref") {
		switch diagnostic.Provenance.Pointer {
		case "/components/schemas/ExtRef":
			ref = append(ref, diagnostic)
		case "/components/schemas/Ext":
			dyn = append(dyn, diagnostic)
		}
	}
	require.Len(t, ref, 1, "%+v", diags)
	require.Len(t, dyn, 1, "%+v", diags)
	assert.Equal(t, strings.Replace(ref[0].Message, `unresolved $ref "`, `unresolved $dynamicRef "`, 1), dyn[0].Message)
}

// TestCompile_ATwoMemberCycleThroughADynamicRefIsACyclicRef pins that a loop
// through a $dynamicRef that reads as a $ref is the loop the $ref spellings
// make, reported once, however the two are mixed and written first.
func TestCompile_ATwoMemberCycleThroughADynamicRefIsACyclicRef(t *testing.T) {
	t.Parallel()
	const (
		a = "    A: {$ref: '#/components/schemas/B'}\n"
		b = "    B: {$ref: '#/components/schemas/A'}\n"
		x = "    A: {$dynamicRef: '#/components/schemas/B'}\n"
		y = "    B: {$dynamicRef: '#/components/schemas/A'}\n"
	)
	twin, twinDiags := compileText(t, "spec.yaml", dynHead+a+b, openapi.Options{})
	require.NotNil(t, twin)
	require.Len(t, byCode(twinDiags, "openapi/cyclic-ref"), 1, "the $ref loop is reported once: %+v", twinDiags)

	for name, schemas := range map[string][]string{
		"both dynamic":          {x, y},
		"both dynamic, swapped": {y, x},
		"one dynamic":           {a, y},
		"one dynamic, swapped":  {y, a},
	} {
		doc, diags := compileText(t, "spec.yaml", dynHead+strings.Join(schemas, ""), openapi.Options{})
		require.NotNil(t, doc, name)
		cyclic := byCode(diags, "openapi/cyclic-ref")
		require.Len(t, cyclic, 1, "%s: %+v", name, diags)
		assert.Equal(t, ir.SeverityError, cyclic[0].Severity, name)
		assert.EqualValues(t, "/components/schemas/A", cyclic[0].Provenance.Pointer, name)
		for _, diagnostic := range diags {
			assert.NotEqual(t, "openapi/dynamic-ref-expanded", diagnostic.Code, name)
		}
		for _, id := range []ir.TypeID{componentID("A"), componentID("B")} {
			_, inTwin := twin.Types[id]
			got, in := doc.Types[id]
			assert.Equal(t, inTwin, in, "%s: %s is lowered as the $ref loop's member is", name, id)
			if sc, ok := got.(*ir.Scalar); ok && sc.Base != nil {
				assert.Equal(t, ir.TypeID("type/primitive/any"), sc.Base.Target, "%s: %s lowers the top type", name, id)
			}
		}
	}
}

// TestCompile_AnAliasLoopAnExpansionClosesKeepsTheReference pins that a
// $dynamicRef whose expansion would close a loop through a $ref is kept, in
// either order, and that no base chain the document holds returns to itself.
func TestCompile_AnAliasLoopAnExpansionClosesKeepsTheReference(t *testing.T) {
	t.Parallel()
	const (
		a = "    A: {$dynamicAnchor: a, $ref: '#/components/schemas/B'}\n"
		b = "    B: {$dynamicRef: '#a'}\n"
	)
	for name, schemas := range map[string]string{"A first": a + b, "B first": b + a} {
		doc, diags := compileText(t, "spec.yaml", dynHead+schemas, openapi.Options{})
		require.NotNil(t, doc, name)

		sc := dynScalar(t, doc, "B")
		require.NotNil(t, sc.Base, name)
		assert.Equal(t, ir.TypeID("type/primitive/any"), sc.Base.Target, "%s: the loop is not entered", name)
		entry := unmodeledEntry(t, sc.Unmodeled, "openapi:$dynamicRef")
		assert.JSONEq(t, `"#a"`, string(entry.Value), name)

		var found []ir.Diagnostic
		for _, diagnostic := range diags {
			if diagnostic.Code == "openapi/degraded-construct" && diagnostic.Provenance.Pointer == "/components/schemas/B/$dynamicRef" {
				found = append(found, diagnostic)
			}
		}
		require.Len(t, found, 1, "%s: %+v", name, diags)
		assert.Equal(t, ir.SeverityInfo, found[0].Severity, name)
		assert.Contains(t, found[0].Message,
			`closes a cycle of $dynamicRef expansions back onto "/components/schemas/B"`, name)
		assert.Empty(t, byCode(diags, "openapi/dynamic-ref-expanded"), name)
		assert.Empty(t, byCode(diags, "openapi/cyclic-ref"), name, "the loop is not one of $refs")

		aType := dynScalar(t, doc, "A")
		require.NotNil(t, aType.Base, name)
		assert.Equal(t, componentID("B"), aType.Base.Target, name)
		for id := range doc.Types {
			dynBaseChainTerminates(t, doc, id)
		}
	}
}

// TestCompile_AStaticDynamicRefBesideAShapeKeepsItsSibling pins that a
// reference that reads as a $ref, beside a keyword that shapes the type, lowers
// as its $ref twin does: the conjunction of the two, a model over the object
// it names whose type the target already says, and nothing of the reference
// kept, since it resolved.
func TestCompile_AStaticDynamicRefBesideAShapeKeepsItsSibling(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", dynHead+dynPet+
		"    C: {$dynamicRef: '#pet', description: d, type: object}\n", openapi.Options{})
	twin, twinDiags := compileText(t, "spec.yaml", dynHead+dynPet+
		"    C: {$ref: '#pet', description: d, type: object}\n", openapi.Options{})
	require.NotNil(t, doc)
	require.Empty(t, byCode(diags, "openapi/dynamic-ref-expanded"))
	assert.Empty(t, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/C"), "%+v", diags)
	assert.Empty(t, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/C/$dynamicRef"), "%+v", diags)
	assert.Equal(t, jsonOf(t, twin), jsonOf(t, doc))
	assert.Equal(t, twinDiags, diags)
	model, ok := doc.Types[componentID("C")].(*ir.Model)
	require.True(t, ok, "got %T", doc.Types[componentID("C")])
	require.NotNil(t, model.Base)
	assert.Equal(t, componentID("Pet"), model.Base.Target)
	assert.Empty(t, model.Unmodeled)
}

// TestCompile_AReferenceToAStaticDynamicRefEndsWhereItEnds pins that a $ref
// onto a $dynamicRef that reads as a $ref names that component, as one onto a
// $ref alias does, the component landing on the schema the chain ends on, and
// that a property that is one lands there too, as its $ref twin does.
func TestCompile_AReferenceToAStaticDynamicRefEndsWhereItEnds(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", dynHead+dynPet+
		"    ByPointer: {$dynamicRef: '#/components/schemas/Pet'}\n"+
		"    S: {$ref: '#/components/schemas/ByPointer'}\n"+
		"    STwin: {$ref: '#/components/schemas/Pet'}\n"+
		"    Obj:\n      type: object\n      properties:\n        p: {$dynamicRef: '#/components/schemas/Pet'}\n"+
		"        q: {$ref: '#/components/schemas/Pet'}\n", openapi.Options{})
	require.NotNil(t, doc)
	assert.Empty(t, diags)

	dynUse, twin := dynScalar(t, doc, "S"), dynScalar(t, doc, "STwin")
	require.NotNil(t, dynUse.Base)
	require.NotNil(t, twin.Base)
	assert.Equal(t, componentID("ByPointer"), dynUse.Base.Target, "a $ref names the component, as one onto a $ref alias does")
	assert.Equal(t, componentID("Pet"), twin.Base.Target)
	byPointer := dynScalar(t, doc, "ByPointer")
	require.NotNil(t, byPointer.Base)
	assert.Equal(t, componentID("Pet"), byPointer.Base.Target, "and that component lands on Pet")

	obj, ok := doc.Types[componentID("Obj")].(*ir.Model)
	require.True(t, ok)
	property, okP := propertyByWire(obj, "p")
	irProperty, okQ := propertyByWire(obj, "q")
	require.True(t, okP && okQ)
	assert.Equal(t, componentID("Pet"), property.Type.Target)
	assert.Empty(t, cmp.Diff(irProperty.Type, property.Type))
	assert.Empty(t, property.Unmodeled)
}
