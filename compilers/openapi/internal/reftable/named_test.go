// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// namedSteps is the bound the totals below are taken within, far above any
// document here.
const namedSteps = 1 << 20

// namedHead opens each document.
const namedHead = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"

// namedSpine is a schema three levels deep under long names, so the pointers of
// its nodes are most of what a total counts.
const namedSpine = "{type: object, properties: {kkkkkkkkkkkk: {type: object, properties: {llllllllllll: {type: string}}}}}"

// namedTotal reads text and returns the total the grammar names, and whether it
// is complete.
func namedTotal(t *testing.T, text string) (int, bool) {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &doc))
	return reftable.NamedPointerBytes(t.Context(), nodeview.DocumentRoot(&doc), namedSteps)
}

// TestNamedPointerBytes_AValueKeptRawAddsItsRootAlone holds an anchored spine
// mounted as a schema and as an example: read as a schema every node of it is
// named, read as an example only the root of the mount is, so mounting it as an
// example adds one pointer and mounting it as a schema adds the spine's.
func TestNamedPointerBytes_AValueKeptRawAddsItsRootAlone(t *testing.T) {
	t.Parallel()
	const base = namedHead + "components:\n  schemas:\n    S: &s " + namedSpine + "\n"
	without, complete := namedTotal(t, base+"    E: {type: string}\n")
	require.True(t, complete)

	asExample, complete := namedTotal(t, base+"    E: {type: string, example: *s}\n")
	require.True(t, complete)
	asSchema, complete := namedTotal(t, base+"    E: {type: object, properties: {x: *s}}\n")
	require.True(t, complete)
	asSchemaBase, _ := namedTotal(t, base+"    E: {type: object, properties: {}}\n")

	assert.Equal(t, 2*len("/components/schemas/E/example"), asExample-without,
		"only the root of the mount is named, with its key, each at the pointer it is mounted at")
	assert.Greater(t, asSchema-asSchemaBase, 5*(asExample-without), "mounted as a schema, every node of it is named")
}

// TestNamedPointerBytes_AKeyARawValueWritesTwiceIsNamed holds a value kept raw
// to its root and each key a mapping in it writes more than once, where the
// rule table places a finding: a mapping writing two keys twice each, mounted
// at two positions of a list inside an extension, adds four pointers, and the
// same mapping writing each key once adds none.
func TestNamedPointerBytes_AKeyARawValueWritesTwiceIsNamed(t *testing.T) {
	t.Parallel()
	doc := func(m string) string { return namedHead + "x-m: &m " + m + "\nx-e: {kkkkkkkkkkkk: [*m, [*m]]}\n" }
	once, complete := namedTotal(t, doc("{a: 1, b: 2, c: 3, d: 4}"))
	require.True(t, complete)
	twice, complete := namedTotal(t, doc("{a: 1, b: 2, a: 3, b: 4}"))
	require.True(t, complete)

	// The mapping is raw at its anchor too, where its two keys are named once more.
	want := 2*len("/x-m/a") + 2*len("/x-e/kkkkkkkkkkkk/0/a") + 2*len("/x-e/kkkkkkkkkkkk/1/0/a")
	assert.Equal(t, want, twice-once, "each key written twice is named at each position its mapping is read at")
}

// TestNamedPointerBytes_AKeywordJudgedByEntryIsNamedNodeByNode holds
// `$vocabulary` and `dependentRequired` to a total of every node they hold:
// the rule table judges each entry of one, and each name a list of the latter
// repeats, and places a finding there, where an extension of the same content
// adds its root alone.
func TestNamedPointerBytes_AKeywordJudgedByEntryIsNamedNodeByNode(t *testing.T) {
	t.Parallel()
	doc := func(key string) string {
		return namedHead + "components: {schemas: {S: {" + key + ": {kkkkkkkkkkkk: [a, a], llllllllllll: [b]}}}}\n"
	}
	for _, key := range []string{"$vocabulary", "dependentRequired"} {
		raw, complete := namedTotal(t, doc("x-"+key))
		require.True(t, complete)
		named, complete := namedTotal(t, doc(key))
		require.True(t, complete)

		at := len("/components/schemas/S/") + len(key)
		entries := 2 * (at + len("/kkkkkkkkkkkk") + at + len("/llllllllllll"))
		items := 2*(at+len("/kkkkkkkkkkkk/0")) + at + len("/llllllllllll/0")
		assert.Equal(t, entries+items-2*len("x-"), named-raw, "%s: each entry and each name it lists is named", key)
	}
}

// TestNamedPointerBytes_AMemberAMergeKeyBringsInIsNamedAtTheMapping holds a
// schema merged in through `<<` to the total of the same schema mounted by an
// alias, as the binder and the parser read the merged members as the mapping's
// own, and a merge chain past nodeview.MergeDepthLimit to no total.
func TestNamedPointerBytes_AMemberAMergeKeyBringsInIsNamedAtTheMapping(t *testing.T) {
	t.Parallel()
	const base = namedHead + "x-s: &s " + namedSpine + "\ncomponents:\n  schemas:\n"
	aliased, complete := namedTotal(t, base+"    E: {type: object, properties: {x: *s}}\n")
	require.True(t, complete)
	merged, complete := namedTotal(t, base+"    E: {type: object, properties: {x: {<<: *s}}}\n")
	require.True(t, complete)
	raw, complete := namedTotal(t, base+"    E: {type: object, properties: {x: {}}}\n")
	require.True(t, complete)

	assert.Equal(t, aliased, merged, "a merged member is named at the mapping, as an alias's is at its mount")
	assert.Greater(t, merged-raw, 5*len("/components/schemas/E/properties/x"), "every node of the spine is named")

	var chain strings.Builder
	chain.WriteString(namedHead + "x-chain:\n  a0: &a0 {type: string}\n")
	for i := 1; i <= nodeview.MergeDepthLimit+1; i++ {
		fmt.Fprintf(&chain, "  a%d: &a%d {<<: *a%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&chain, "components:\n  schemas:\n    E: {<<: *a%d}\n", nodeview.MergeDepthLimit+1)
	_, complete = namedTotal(t, chain.String())
	assert.False(t, complete, "a merge chain the binder cannot expand leaves the total untaken")
}

// TestNamedPointerBytes_AFieldNoModelHoldsIsNamedNodeByNode holds a schema
// beneath an Encoding Object another nests to being named node by node, where
// its twin under an extension key of the same length adds its root alone: the
// lowering keeps the field verbatim, but the binder reads each node beneath it,
// spelling its pointer, to bind the references written there. A $ref reaching
// the schema names nothing the walk has not, and one written beneath the field
// names the raw value it lands in.
func TestNamedPointerBytes_AFieldNoModelHoldsIsNamedNodeByNode(t *testing.T) {
	t.Parallel()
	doc := func(key, in, out string) string {
		return namedHead + "x-data: " + namedSpine + "\ncomponents:\n  schemas:\n    T: {type: string}\n    In: {$ref: '" + in + "'}\n" +
			"  requestBodies:\n    B: {content: {m/x: {encoding: {f: {" + key + ": {g: {headers: {H: {schema: " +
			"{type: object, properties: {o: {$ref: '" + out + "'}, k: " + namedSpine + "}}}}}}}}}}}\n"
	}
	const at = "#/components/requestBodies/B/content/m~1x/encoding/f/"
	totals := map[string]int{}
	for name, text := range map[string]string{
		"nested":         doc("encoding", "#/components/schemas/T", "#/x-data"),
		"nested, no raw": doc("encoding", "#/components/schemas/T", "#/components/schemas/T"),
		"reached":        doc("encoding", at+"encoding/g/headers/H/schema", "#/x-data"),
		"reached twin":   doc("x-coding", at+"x-coding/g/headers/H/schema", "#/x-data"),
	} {
		total, complete := namedTotal(t, text)
		require.True(t, complete, name)
		totals[name] = total
	}

	assert.Greater(t, totals["nested"], totals["reached twin"],
		"every node beneath the field is named, the schema the twin's $ref names among them")
	assert.Equal(t, totals["nested"], totals["reached"], "a $ref reaching the schema names nothing more")
	assert.Greater(t, totals["nested"], totals["nested, no raw"]+100, "the $ref beneath the field names the raw value it lands in")
}

// TestNamedPointerBytes_TheTotalDoesNotDependOnWhichMountIsWrittenFirst holds a
// node mounted as a schema and as an example to one total in both declaration
// orders: a memo keyed by the node alone would count whichever came first.
func TestNamedPointerBytes_TheTotalDoesNotDependOnWhichMountIsWrittenFirst(t *testing.T) {
	t.Parallel()
	schemaFirst, c1 := namedTotal(t, namedHead+"components:\n  schemas:\n    S: &s "+namedSpine+"\n    E: {type: string, example: *s}\n")
	exampleFirst, c2 := namedTotal(t, namedHead+"components:\n  schemas:\n    E: {type: string, example: &s "+namedSpine+"}\n    S: *s\n")

	assert.True(t, c1)
	assert.True(t, c2)
	assert.Equal(t, schemaFirst, exampleFirst)
}

// TestNamedPointerBytes_AReferenceIntoARawValueNamesItNodeByNode holds a raw
// value a $ref or a discriminator mapping lands in to being counted as the kind
// the reference expects, and a $ref written inside an example to being data.
func TestNamedPointerBytes_AReferenceIntoARawValueNamesItNodeByNode(t *testing.T) {
	t.Parallel()
	const raw = namedHead + "x-data: " + namedSpine + "\n"
	alone, complete := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object}\n")
	require.True(t, complete)

	byRef, c1 := namedTotal(t, raw+"components:\n  schemas:\n    A: {$ref: \"#/x-data\"}\n")
	byMapping, c2 := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object, discriminator: {propertyName: t, mapping: {a: \"#/x-data\"}}}\n")
	inExample, c3 := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object, example: {$ref: \"#/x-data\"}}\n")
	elsewhere, c4 := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object, example: {$ref: \"#/x-dxta\"}}\n")

	assert.True(t, c1 && c2 && c3 && c4)
	assert.Greater(t, byRef, alone+100, "the spine is named once a $ref lands in it")
	assert.Greater(t, byMapping, alone+100, "and once a discriminator mapping does")
	assert.Equal(t, elsewhere, inExample, "a $ref inside an example is data")
}

// TestNamedPointerBytes_IsIncompleteWhereTheDocumentIsNotClosed holds the
// answer's completeness: a reference that leaves the document, one whose
// pointer the binder reads by a rule of its own, a bound spent before the
// document is read, and a closed document tell it apart.
func TestNamedPointerBytes_IsIncompleteWhereTheDocumentIsNotClosed(t *testing.T) {
	t.Parallel()
	closed := namedHead + "components:\n  schemas:\n    A: {type: string}\n"
	leaving := namedHead + "components:\n  schemas:\n    A: {$ref: \"other.yaml#/x\"}\n"
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(closed), &doc))

	_, whole := namedTotal(t, closed)
	_, left := namedTotal(t, leaving)
	_, nowhere := namedTotal(t, namedHead+"components:\n  schemas:\n    A: {$ref: \"#/x-nowhere\"}\n")
	_, underID := namedTotal(t, namedHead+"x-data: {}\ncomponents:\n  schemas:\n    A: {$id: \"https://e.example/a\", properties: {p: {$ref: \"#/x-data\"}}}\n")
	_, through := namedTotal(t, namedHead+"x-p: {name: n, in: query, x-data: "+namedSpine+"}\ncomponents:\n  parameters:\n"+
		"    P: {$ref: \"#/x-p\", x-data: {type: string}}\n  schemas:\n    A: {$ref: \"#/components/parameters/P/x-data\"}\n")
	_, beside := namedTotal(t, namedHead+"components:\n  parameters:\n    P: {name: n, in: query, x-data: {type: string}}\n"+
		"  schemas:\n    A: {$ref: \"#/components/parameters/P/x-data\"}\n")
	_, spent := reftable.NamedPointerBytes(t.Context(), nodeview.DocumentRoot(&doc), 1)

	assert.True(t, whole)
	assert.True(t, beside, "a pointer through an object that writes no $ref reads what the object writes")
	assert.False(t, through, "the binder reads on in the object a Reference Object names, not beside its $ref")
	assert.False(t, left, "another document could route back into a raw value")
	assert.False(t, nowhere, "the binder may answer a pointer naming nothing by a rule of its own")
	assert.False(t, underID, "an $id moves where a $ref under it lands")
	assert.False(t, spent, "past the step bound")
}

// TestNamedPointerBytes_ALandingIsNamedAsTheKindItsReferenceExpects holds a
// position the grammar reads as one kind and a reference lands on as another
// to the reference's reading: a schema $ref onto a parameter reads its
// `properties`, raw to the parameter, as a schema's, node by node.
func TestNamedPointerBytes_ALandingIsNamedAsTheKindItsReferenceExpects(t *testing.T) {
	t.Parallel()
	const base = namedHead + "components:\n  parameters:\n    P: {name: n, in: query, properties: {q: " + namedSpine +
		"}}\n  schemas:\n    A: "
	unread, c1 := namedTotal(t, base+"{type: string}\n")
	landed, c2 := namedTotal(t, base+"{$ref: \"#/components/parameters/P\"}\n")
	inPlace, c3 := namedTotal(t, namedHead+"components:\n  schemas:\n    P: {name: n, in: query, properties: {q: "+namedSpine+"}}\n")
	empty, c4 := namedTotal(t, namedHead+"components:\n  schemas:\n    P: {name: n, in: query, properties: {}}\n")

	assert.True(t, c1 && c2 && c3 && c4)
	assert.Greater(t, landed-unread, inPlace-empty, "the spine is named as a schema's property, under the parameter's pointer")
}

// TestNamedPointerBytes_AFragmentLandsWhereItsDecodingNames holds a fragment
// written with a percent escape to the node its decoding names, as the binder
// reads it: the same total as the fragment written plain.
func TestNamedPointerBytes_AFragmentLandsWhereItsDecodingNames(t *testing.T) {
	t.Parallel()
	const raw = namedHead + "x-data: " + namedSpine + "\n"
	plain, c1 := namedTotal(t, raw+"components:\n  schemas:\n    A: {$ref: \"#/x-data\"}\n")
	escaped, c2 := namedTotal(t, raw+"components:\n  schemas:\n    A: {$ref: \"#/x%2Ddata\"}\n")

	assert.True(t, c1)
	assert.True(t, c2)
	assert.Equal(t, plain, escaped)
}

// TestNamedPointerBytes_ADynamicRefLandsAsARefDoes holds a $dynamicRef whose
// fragment is a JSON pointer, which the compile reads as the $ref it spells, to
// adding what its $ref twin adds to a key of its length the grammar keeps raw:
// the raw value it lands in, named node by node. One naming an anchor by a
// plain name leaves the total untaken, as a $ref so written does.
func TestNamedPointerBytes_ADynamicRefLandsAsARefDoes(t *testing.T) {
	t.Parallel()
	const raw = namedHead + "x-data: " + namedSpine + "\ncomponents:\n  schemas:\n"
	total := func(key, ref string) int {
		t.Helper()
		got, complete := namedTotal(t, raw+"    A: {"+key+": \""+ref+"\"}\n")
		require.True(t, complete, key)
		return got
	}
	byRef := total("$ref", "#/x-data") - total("$reg", "#/x-data")
	byDynamic := total("$dynamicRef", "#/x-data") - total("$dynamicReg", "#/x-data")
	_, named := namedTotal(t, raw+"    A: {$dynamicRef: \"#meta\"}\n")

	assert.Positive(t, byRef, "the raw value is named node by node")
	assert.Equal(t, byRef, byDynamic)
	assert.False(t, named, "a plain name is no JSON pointer")
}

// TestNamedPointerBytes_ADiscriminatorValueLandsAsTheBinderReadsIt holds a
// discriminator's mapping value and its defaultMapping to the binder's reading:
// one spelling a schema component's key names that component, which is read
// where it is declared and leaves the total whole, one written as a pointer
// into a raw value adds that value node by node, tagged null or not, since the
// binder reads a value's text, and one naming neither leaves the document.
func TestNamedPointerBytes_ADiscriminatorValueLandsAsTheBinderReadsIt(t *testing.T) {
	t.Parallel()
	total := func(mapped, fallback string) (int, bool) {
		t.Helper()
		return namedTotal(t, namedHead+"x-data: "+namedSpine+"\ncomponents:\n  schemas:\n    Dog: {type: object}\n"+
			"    Pet: {type: object, discriminator: {propertyName: t, mapping: {dog: "+mapped+"}, defaultMapping: "+
			fallback+"}}\n")
	}
	byKey, whole := total("Dog", "Dog")
	byPointer, alsoWhole := total("'#/components/schemas/Dog'", "'#/components/schemas/Dog'")
	mapped, c1 := total("'#/x-data'", "Dog")
	fallback, c2 := total("Dog", "'#/x-data'")
	tagged, c3 := total("!!null '#/x-data'", "Dog")
	taggedFallback, c4 := total("Dog", "!!null '#/x-data'")
	_, undeclared := total("Dog", "Cat")
	_, null := total("~", "Dog")

	assert.True(t, whole && alsoWhole && c1 && c2 && c3 && c4)
	assert.Equal(t, byPointer, byKey, "a component is read where it is declared")
	assert.Greater(t, mapped, byKey, "the raw value a mapping value lands in is named node by node")
	assert.Equal(t, mapped, fallback, "a defaultMapping lands as a mapping value does")
	assert.Equal(t, mapped, tagged, "a mapping value tagged null lands where its text names")
	assert.Equal(t, mapped, taggedFallback, "and so does a defaultMapping")
	assert.False(t, undeclared, "a name no component declares reads as another document's")
	assert.False(t, null, "and so does a null's text")
}

// TestNamedPointerBytes_ARequirementKeyLandsAsTheBinderReadsIt holds a Security
// Requirement Object's key to the binder's reading: one spelling a security
// scheme component's key names that component, which is read where it is
// declared, one written as a pointer into a raw value adds that value read as
// a Security Scheme Object, as a security scheme's $ref naming it does, and
// one naming neither leaves the document. A minor before 3.2 reads no key as a
// URI, so there such a key is a name and lands nowhere.
func TestNamedPointerBytes_ARequirementKeyLandsAsTheBinderReadsIt(t *testing.T) {
	t.Parallel()
	const declared, raw = "'#/components/securitySchemes/Aaaaaaaa'", "'#/x-data'"
	total := func(minor, key, ref string) (int, bool) {
		t.Helper()
		return namedTotal(t, "openapi: "+minor+"\ninfo: {title: t, version: \"1\"}\npaths: {}\n"+
			"x-data: {type: http, scheme: basic, kkkkkkkkkkkk: 1, llllllllllll: 1}\n"+
			"security: [{"+key+": []}]\ncomponents:\n  securitySchemes:\n    Aaaaaaaa: {type: http, scheme: basic}\n"+
			"    Bbbbbbbb: {$ref: "+ref+"}\n")
	}
	byName, c1 := total("3.2.0", "Aaaaaaaa", declared)
	byKey, c2 := total("3.2.0", raw, declared)
	byRef, c3 := total("3.2.0", "Aaaaaaaa", raw)
	_, undeclared := total("3.2.0", "Cccccccc", declared)
	earlier, c4 := total("3.1.2", raw, declared)
	_, earlierName := total("3.1.2", "Cccccccc", declared)

	assert.True(t, c1 && c2 && c3 && c4)
	assert.Greater(t, byKey, byName, "the raw value a key lands in is named as a Security Scheme Object")
	assert.Equal(t, byRef, byKey, "as one a security scheme's $ref lands in is, the two keys of one length")
	assert.False(t, undeclared, "a name no component declares reads as another document's")
	assert.Equal(t, byName, earlier, "before 3.2 a key is no URI, so it lands nowhere")
	assert.True(t, earlierName, "and a name no component declares names no document")
}

// TestNamedPointerBytes_CountsAKeyAsItIsWritten holds a key holding a `/` and
// a `~`, which a JSON pointer escapes as two bytes each, to the total of a key
// of its length that holds neither, as the grammar-free total it is compared
// beside counts one (sourcetreestats.Measurements.PointerBytes).
func TestNamedPointerBytes_CountsAKeyAsItIsWritten(t *testing.T) {
	t.Parallel()
	const schema = namedHead + "components:\n  schemas:\n    A: {type: object, properties: {%q: {type: string}}}\n"
	escaped, c1 := namedTotal(t, fmt.Sprintf(schema, "/a~b"))
	plain, c2 := namedTotal(t, fmt.Sprintf(schema, "xaxb"))

	require.True(t, c1)
	require.True(t, c2)
	assert.Equal(t, plain, escaped)
}

// TestNamedPointerBytes_ALandingNamedInPlaceIsCountedOnce holds a $ref whose
// target the walk already names where it is written to adding nothing past the
// $ref itself: the same total as a key of the same length the grammar keeps
// raw.
func TestNamedPointerBytes_ALandingNamedInPlaceIsCountedOnce(t *testing.T) {
	t.Parallel()
	const base = namedHead + "components:\n  schemas:\n    A: " + namedSpine + "\n"
	byRef, c1 := namedTotal(t, base+"    B: {$ref: \"#/components/schemas/A\"}\n")
	byRaw, c2 := namedTotal(t, base+"    B: {$reg: \"#/components/schemas/A\"}\n")

	assert.True(t, c1)
	assert.True(t, c2)
	assert.Equal(t, byRaw, byRef)
}

// TestNamedPointerBytes_AKeyWrittenTwiceIsNamedAtTheCopyAMergeReads holds a
// spine under one copy of a key written twice to being named only where the
// binder reads that copy (docs/parser-library.md#duplicate-key): a mapping's
// own repeated key at its last copy, and a `<<` source's at its first.
func TestNamedPointerBytes_AKeyWrittenTwiceIsNamedAtTheCopyAMergeReads(t *testing.T) {
	t.Parallel()
	const base = namedHead + "components:\n  schemas:\n"
	const leaf = "{type: string}"
	once, c1 := namedTotal(t, base+"    A: {type: object, properties: {k: "+leaf+"}}\n")
	spineFirst, c2 := namedTotal(t, base+"    A: {type: object, properties: {k: "+namedSpine+", k: "+leaf+"}}\n")
	spineLast, c3 := namedTotal(t, base+"    A: {type: object, properties: {k: "+leaf+", k: "+namedSpine+"}}\n")
	const merged = namedHead + "x-s: &s {k: %s, k: %s}\ncomponents:\n  schemas:\n    A: {type: object, properties: {<<: *s}}\n"
	mergedFirst, c4 := namedTotal(t, fmt.Sprintf(merged, namedSpine, leaf))
	mergedLast, c5 := namedTotal(t, fmt.Sprintf(merged, leaf, namedSpine))

	require.True(t, c1 && c2 && c3 && c4 && c5)
	assert.Equal(t, once, spineFirst, "a spine under a mapping's first copy is not named")
	assert.Greater(t, spineLast, once+100, "a spine under its last copy is")
	assert.Greater(t, mergedFirst, mergedLast+100, "a spine under a merged source's first copy is named")
}

// TestNamedPointerBytes_ALandingIsNamedAtEachPositionItIsReachedAt holds a raw
// spine an anchor mounts under two extension keys of one length to being named
// once for each mount a $ref lands at: the binder walks a landing from where
// it lands, so the second mount costs what the first does, though both hold
// one node. Two references to one mount name it once.
func TestNamedPointerBytes_ALandingIsNamedAtEachPositionItIsReachedAt(t *testing.T) {
	t.Parallel()
	const base = namedHead + "x-s: &s " + namedSpine + "\nx-a: *s\nx-b: *s\ncomponents:\n  schemas:\n"
	none, c0 := namedTotal(t, base+"    A: {type: object}\n    B: {type: object}\n")
	once, c1 := namedTotal(t, base+"    A: {$ref: \"#/x-a\"}\n    B: {type: object}\n")
	twice, c2 := namedTotal(t, base+"    A: {$ref: \"#/x-a\"}\n    B: {$ref: \"#/x-b\"}\n")
	again, c3 := namedTotal(t, base+"    A: {$ref: \"#/x-a\"}\n    B: {$ref: \"#/x-a\"}\n")

	require.True(t, c0 && c1 && c2 && c3)
	assert.Greater(t, once-none, 5*len("/x-a"), "the spine is named once a $ref lands in it")
	assert.Equal(t, once-none, twice-once, "a second mount of the spine is named as the first is")
	assert.Equal(t, once, again, "a mount two references land at is named once")
}

// TestNamedPointerBytes_NamesTheEntriesOfAListAndTheReferenceOfAnObject holds
// the nodes of a list, an `allOf` entry and a parameter list item among
// them, to the pointers they are named by: the total grows by what a list adds, a
// parameter that is a reference object among them.
func TestNamedPointerBytes_NamesTheEntriesOfAListAndTheReferenceOfAnObject(t *testing.T) {
	t.Parallel()
	const head = namedHead + "components:\n  parameters:\n    p: {name: p, in: query, schema: {type: string}}\n  schemas:\n"
	bare, complete := namedTotal(t, head+"    S: {type: object}\n")
	require.True(t, complete)

	withList, complete := namedTotal(t, head+"    S: {type: object, allOf: [{type: string}, {type: integer}]}\n")
	require.True(t, complete)
	const operation = namedHead + "paths:\n  /a:\n    get:\n      parameters: %s\n" +
		"components:\n  parameters:\n    p: {name: p, in: query, schema: {type: string}}\n"
	noParameters, complete := namedTotal(t, fmt.Sprintf(operation, "[]"))
	require.True(t, complete)
	withReference, complete := namedTotal(t, fmt.Sprintf(operation, `[{$ref: "#/components/parameters/p"}]`))
	require.True(t, complete)

	// Each pointer counts once for the key and once for the value that holds
	// it, under /components/schemas/S (21 bytes): the allOf key and its list,
	// 27 each, then each of the two entries, 29, with its type key and value, 34
	// each.
	assert.Equal(t, 27+27+2*(29+34+34), withList-bare, "the entries of a list are named, each at its index")
	assert.Greater(t, withReference, noParameters, "a parameter a list holds is named")
}

// TestNamedPointerBytes_NamesNothingForANodeThatIsAbsent holds a tree no parse
// produces, a nil root, a nil member and an alias naming no node, to a total
// that is complete and counts nothing of them: where an empty schema names a
// node, an absent one names none.
func TestNamedPointerBytes_NamesNothingForANodeThatIsAbsent(t *testing.T) {
	t.Parallel()
	key := func(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
	mapping := func(content ...*yaml.Node) *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Content: content} }
	schemas := func(entry *yaml.Node) *yaml.Node {
		return mapping(key("components"), mapping(key("schemas"), mapping(key("S"), entry)))
	}
	total := func(root *yaml.Node) (int, bool) { return reftable.NamedPointerBytes(t.Context(), root, namedSteps) }
	empty, _ := total(schemas(mapping()))

	nilRoot, completeNil := total(nil)
	nilMember, completeMember := total(schemas(nil))
	danglingAlias, completeAlias := total(schemas(&yaml.Node{Kind: yaml.AliasNode}))

	assert.Zero(t, nilRoot)
	assert.True(t, completeNil)
	assert.True(t, completeMember)
	assert.True(t, completeAlias)
	assert.Less(t, nilMember, empty, "an absent schema names no node, an empty one names one")
	assert.Equal(t, nilMember, danglingAlias, "an alias naming no node names none either")
}

// TestSat_HoldsASumAndAProductAtTheLargestInt holds the counts the total adds
// and multiplies to the largest int, where they would wrap.
func TestSat_HoldsASumAndAProductAtTheLargestInt(t *testing.T) {
	t.Parallel()
	const most = int(^uint(0) >> 1)
	assert.Equal(t, 5, reftable.Sat(2, 3))
	assert.Equal(t, most, reftable.Sat(most, 1))
	assert.Equal(t, most, reftable.Sat(1, most))
	assert.Equal(t, 6, reftable.SatMul(2, 3))
	assert.Zero(t, reftable.SatMul(0, most))
	assert.Equal(t, most, reftable.SatMul(most, 2))
	assert.Equal(t, most, reftable.SatMul(2, most))
}
