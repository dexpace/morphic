// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// dynListOther is the document whose List expands its items to the integer
// anchor it declares itself, unless a resource of another document reaches it
// and declares the name too.
const dynListOther = "List:\n  $id: https://example.com/list\n  type: array\n  items: {$dynamicRef: '#items'}\n" +
	"  $defs:\n    items:\n      $dynamicAnchor: items\n      type: integer\n"

// dynListItems is where that reference is, and the anchor it expands to.
const (
	dynListItems  = ir.TypeID("type/anonymous@other.yaml/List/items")
	dynListAnchor = ir.TypeID("type/anonymous@other.yaml/List/$defs/items")
	dynListPoint  = "/List/items/$dynamicRef"
)

// dynStringList declares the same name in the source and reaches the List.
const dynStringList = "    StrList:\n      $id: https://example.com/strlist\n      $ref: 'list'\n" +
	"      $defs:\n        items:\n          $dynamicAnchor: items\n          type: string\n"

// dynKeptIn returns the infos at pointer of the source table's entry source,
// the document naming it.
func dynKeptIn(diags []ir.Diagnostic, source int) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code == "openapi/degraded-construct" && diagnostic.Provenance.Source == source && string(diagnostic.Provenance.Pointer) == dynListPoint {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestCompile_ADynamicRefAnotherDocumentsResourceReachesIsKept pins that a
// resource of another document declaring the name and reaching the reference
// is in its dynamic scope, outermost, so the target depends on the path: the
// reference is kept, whichever component is written first.
func TestCompile_ADynamicRefAnotherDocumentsResourceReachesIsKept(t *testing.T) {
	t.Parallel()
	plain := "    Plain: {$ref: 'other.yaml#/List'}\n"
	for name, schemas := range map[string]string{"StrList first": dynStringList + plain,
		"Plain first": plain + dynStringList} {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"main.yaml": dynHead + schemas, "other.yaml": dynListOther})
		doc, diags := compileDir(t, dir, openapi.Options{})
		require.NotNil(t, doc, name)
		require.Empty(t, byCode(diags, "openapi/unresolved-ref"), "%s: %+v", name, diags)

		items, ok := doc.Types[dynListItems].(*ir.Scalar)
		require.True(t, ok, "%s: %v", name, doc.Types[dynListItems])
		require.NotNil(t, items.Base, name)
		assert.Equal(t, ir.TypeID("type/primitive/any"), items.Base.Target, name)
		entry := unmodeledEntry(t, items.Unmodeled, "openapi:$dynamicRef")
		assert.JSONEq(t, `"#items"`, string(entry.Value), name)
		assert.Empty(t, byCode(diags, "openapi/dynamic-ref-expanded"), name)

		kept := dynKeptIn(diags, 1)
		require.Len(t, kept, 1, "%s: in other.yaml's entry: %+v", name, diags)
		assert.Equal(t, ir.SeverityInfo, kept[0].Severity, name)
		assert.Contains(t, kept[0].Message,
			"is also declared in a schema resource of another document that reaches this one, so the target depends on the evaluation path", name)
	}
}

// TestCompile_ADynamicRefNoOtherDocumentDeclaresTheNameExpands pins the twin
// of the above with the declaring resource removed, and with it kept but
// reaching nothing: a declaration outside the reference's scope changes
// nothing, so the reference expands to the anchor beside it.
func TestCompile_ADynamicRefNoOtherDocumentDeclaresTheNameExpands(t *testing.T) {
	t.Parallel()
	notReaching := strings.Replace(dynStringList, "      $ref: 'list'\n", "", 1)
	require.NotEqual(t, dynStringList, notReaching)
	plain := "    Plain: {$ref: 'other.yaml#/List'}\n"
	for name, schemas := range map[string]string{"declaration removed": plain,
		"declaration reaches nothing": notReaching + plain} {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"main.yaml": dynHead + schemas, "other.yaml": dynListOther})
		doc, diags := compileDir(t, dir, openapi.Options{})
		require.NotNil(t, doc, name)
		require.Empty(t, byCode(diags, "openapi/unresolved-ref"), "%s: %+v", name, diags)

		items, ok := doc.Types[dynListItems].(*ir.Scalar)
		require.True(t, ok, name)
		require.NotNil(t, items.Base, name)
		assert.Equal(t, dynListAnchor, items.Base.Target, name)
		assert.Empty(t, items.Unmodeled, name)
		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAtSource(diags, "openapi/dynamic-ref-expanded", dynListPoint, 1), name)
		assert.Empty(t, dynKeptIn(diags, 1), name)
	}
}

// diagsAtSource returns the severities of the diagnostics coded code at
// pointer of the source table's entry source.
func diagsAtSource(diags []ir.Diagnostic, code, pointer string, source int) []ir.Severity {
	var out []ir.Severity
	for _, diagnostic := range diags {
		if diagnostic.Code == code && diagnostic.Provenance.Source == source && string(diagnostic.Provenance.Pointer) == pointer {
			out = append(out, diagnostic.Severity)
		}
	}
	return out
}

// TestCompile_ADynamicRefNamesTooManyForTheReachWalkAreKept pins what the
// walk costs when its names are many: n names each declared once in each of two
// documents, the first document's resource reaching each of n resources of the
// second, one per name, cost n walks over n resources, charged to the binding
// budget. At 20,000 the budget ends the table,
// every reference is kept as one read from a table not read whole, and the
// compile reports the budget once and ends. At 100 the same shape fits and
// every reference is kept for what it is, a name two documents declare.
func TestCompile_ADynamicRefNamesTooManyForTheReachWalkAreKept(t *testing.T) {
	t.Parallel()
	gen := func(n int) (spec, other string) {
		var top, low, each strings.Builder
		for i := range n {
			fmt.Fprintf(&top, "        n%d: {$dynamicAnchor: n%d}\n", i, i)
			fmt.Fprintf(&low, "    p%d: {$ref: 'https://x.test/l%d'}\n", i, i)
			fmt.Fprintf(&each, "L%d: {$id: 'https://x.test/l%d', properties: {r: {$dynamicRef: '#n%d'}}, "+
				"$defs: {n%d: {$dynamicAnchor: n%d}}}\n", i, i, i, i, i)
		}
		spec = dynHead + "    Top:\n      $ref: 'other.yaml#/Low'\n      $defs:\n" + top.String()
		other = "Low:\n  $id: https://x.test/low\n  properties:\n" + low.String() + each.String()
		return spec, other
	}

	small, other := gen(100)
	dir := t.TempDir()
	layout(t, dir, map[string]string{"main.yaml": small, "other.yaml": other})
	doc, diags := compileDir(t, dir, openapi.Options{})
	require.NotNil(t, doc)
	require.Empty(t, byCode(diags, "openapi/budget-exceeded"), "%d findings", len(diags))
	require.Empty(t, byCode(diags, "openapi/dynamic-ref-expanded"), "a name two documents declare is kept")
	kept := 0
	for _, diagnostic := range byCode(diags, "openapi/degraded-construct") {
		if strings.HasSuffix(string(diagnostic.Provenance.Pointer), "/$dynamicRef") && strings.Contains(diagnostic.Message, "another document") {
			kept++
		}
	}
	assert.Equal(t, 100, kept, "every reference is kept with the verdict for a name another document declares")

	if testing.Short() {
		t.Skip("the budget case binds to its limit")
	}
	big, bigOther := gen(20000)
	bigDir := t.TempDir()
	layout(t, bigDir, map[string]string{"main.yaml": big, "other.yaml": bigOther})
	_, diags = compileDir(t, bigDir, openapi.Options{})
	exceeded := byCode(diags, "openapi/budget-exceeded")
	require.Len(t, exceeded, 1, "%d findings", len(diags))
	assert.Equal(t, ir.SeverityError, exceeded[0].Severity)
	assert.Empty(t, byCode(diags, "openapi/dynamic-ref-expanded"), "a reference past the budget is never expanded")
	var unread int
	for _, diagnostic := range byCode(diags, "openapi/degraded-construct") {
		if strings.Contains(diagnostic.Message, "not read whole") {
			unread++
		}
	}
	assert.Positive(t, unread, "the references are kept as read from a table not read whole")
}
