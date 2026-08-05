package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// canonicalOnly names a model by a canonical with no source beside it. The three
// shape checks exist for exactly that Naming — where there is a Source, the
// grammar check recomputes the whole value and subsumes them — so a fixture
// carrying a Source would measure the wrong check.
func canonicalOnly(canon string) *ir.Document {
	return modelNamed(ir.Naming{Canonical: canon})
}

func modelNamed(n ir.Naming) *ir.Document {
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/M", Name: n}}
	return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
}

func TestVerify_NeutralCanonicalIsClean(t *testing.T) {
	got := irverify.Verify(modelNamed(ir.Naming{Source: "UserID", Canonical: "user_id"}))
	assert.Empty(t, got)
}

func TestVerify_CasedCanonicalIsAViolation(t *testing.T) {
	got := irverify.Verify(modelNamed(ir.Naming{Source: "UserID", Canonical: "userID"}))
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/naming-cased", got[0].Code)
}

// TestVerify_UnsegmentedCanonicalIsAViolation covers the half of invariant 4 the
// casing check cannot see. Every spelling here is lowercase-idempotent, so a
// compiler that passes a source name through unsegmented — a namespaced
// component, a path template, a bracketed parameter — used to satisfy the
// verifier while emitting something no emitter can read as words.
func TestVerify_UnsegmentedCanonicalIsAViolation(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"com.example.user", "get_/pets", "filter[name]", "application/json",
		"_leading", "trailing_", "double__underscore",
	} {
		got := irverify.Verify(canonicalOnly(canon))
		require.NotEmpty(t, got, "canonical %q must be reported", canon)
		assert.Equal(t, "ir/naming-not-words", got[0].Code, "canonical %q", canon)
	}
}

// TestVerify_WordSequencesAreClean pins the other direction: the shapes a
// correct compiler emits — including a canonical with no lowercase form and a
// decomposed accent, which the casing check already documents — are not
// reported, so the segmentation check cannot pass by rejecting everything.
func TestVerify_WordSequencesAreClean(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"user", "user_id", "api_key_2", "count_ℤ", "cafe\u0301_v_2",
	} {
		assert.Empty(t, irverify.Verify(canonicalOnly(canon)),
			"canonical %q is a word sequence", canon)
	}
	// The empty canonical belongs in that list by the shape rules — an anonymous
	// type names itself by its hint, and a source spelled with no word rune in it
	// has no words to report — but it is clean only beside something that does
	// name the entity. Alone it is the absent-name defect
	// (TestVerify_AbsentNameIsAViolation), so it is measured here instead.
	assert.Empty(t, irverify.Verify(modelNamed(ir.Naming{Hint: "connection_domain"})),
		"a hinted naming with no canonical is a word sequence")
}

// TestVerify_LetterDigitRunIsAViolation is the half of segmentation a neutral
// name still carries evidence of: the grammar splits every letter/digit
// boundary, so no word it produces holds a letter next to a digit. Each value
// here is correctly lower-cased and made only of word characters, so the two
// older checks pass it — which is exactly why it went unreported (GitHub #164).
func TestVerify_LetterDigitRunIsAViolation(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"foo2bar",   // the boundary in both directions, in one word
		"api2",      // letter then digit
		"2fa",       // digit then letter
		"ok_v2beta", // in a later word, so the first must not mask it
		"count_ℤ2",  // beside a rune with no lowercase form
		"café2",     // a precomposed accent is a letter, so the boundary is real
	} {
		got := irverify.Verify(canonicalOnly(canon))
		require.NotEmpty(t, got, "canonical %q must be reported", canon)
		assert.Equal(t, "ir/naming-unsegmented", got[0].Code, "canonical %q", canon)
	}
}

// TestVerify_SegmentationCheckDoesNotOverreach guards the other direction. A
// check that rejected every digit, or every word boundary, would satisfy the
// test above while failing every real document: these are shapes
// ir.CanonicalWords genuinely produces.
func TestVerify_SegmentationCheckDoesNotOverreach(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"api_key_2", "v_2", "2", "user_2_id", "oauth_2_token", "a_1_b_2",
		// A decomposed accent puts a combining mark between the letter and the
		// digit, and the grammar does not split after a mark: CanonicalWords leaves
		// "cafe\u0301" + "2" as one word, so reporting it would reject its own output.
		"cafe\u03012",
	} {
		assert.Empty(t, irverify.Verify(canonicalOnly(canon)),
			"canonical %q is what the grammar produces", canon)
	}
}

// TestVerify_UnsplitCamelCaseIsAViolation is the case the grammar check exists
// for, and the one nothing decidable from Canonical alone can see: "userid" is
// lower-cased, is a word sequence, and straddles no letter/digit boundary, so
// every other check here passes it. Only recomputing from the source separates a
// compiler that neutralized "userID" without splitting it from one that had a
// genuine single word (GitHub #164).
func TestVerify_UnsplitCamelCaseIsAViolation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, canon string }{
		{"userID", "userid"},                // a trailing acronym left joined
		{"firstName", "firstname"},          // an ordinary camel boundary
		{"HTTPServer", "httpserver"},        // an acronym tail
		{"com.example.User", "com_example"}, // a word dropped entirely
		{"User", ""},                        // a source that ships no words at all
	} {
		t.Run(tc.source+"/"+tc.canon, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(modelNamed(ir.Naming{Source: tc.source, Canonical: tc.canon}))
			require.NotEmpty(t, got, "canonical %q does not derive from %q", tc.canon, tc.source)
			assert.Equal(t, "ir/naming-not-derived", got[0].Code)
			assert.Contains(t, got[0].Message, ir.CanonicalWords(tc.source),
				"the message names what the grammar would have produced")
		})
	}
}

// TestVerify_UnsplitCamelCasePassesEveryOtherCheck states why the grammar check
// had to be added rather than the existing ones extended: the value it rejects is
// clean by all three of them. Without this, a reader could reasonably assume the
// segmentation check already covered it.
func TestVerify_UnsplitCamelCasePassesEveryOtherCheck(t *testing.T) {
	t.Parallel()
	assert.Empty(t, irverify.Verify(canonicalOnly("userid")),
		"carried without a source, the same value is indistinguishable from one genuine word")
}

// TestVerify_DerivedNamingIsClean is the control: a Naming the grammar produced
// reports nothing, and neither does one that carries a hint instead of a source.
func TestVerify_DerivedNamingIsClean(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"userID", "com.example.User", "get /pets/{petId}", "***"} {
		derived := ir.Naming{Source: source, Canonical: ir.CanonicalWords(source)}
		assert.Empty(t, irverify.Verify(modelNamed(derived)),
			"a canonical the grammar derived from %q is what it expects", source)
	}
	assert.Empty(t, irverify.Verify(modelNamed(ir.Naming{Hint: "connection_domain"})),
		"an anonymous type carries a hint and no source, so there is nothing to derive from")
}

// ok200 is the unremarkable single-status condition set, so a response fixture
// says nothing about conditions it does not mean to test.
func ok200() ir.ResponseConditions {
	return ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}}}
}

// respondingDoc is a named service/group/operation chain holding one response,
// so a fixture about the response itself contributes no violations of its own.
func respondingDoc(r ir.Response) *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:   "s/x/S",
		Name: named("s"),
		Groups: []ir.OperationGroup{{
			Name: named("g"),
			Operations: []ir.Operation{{
				ID:        "o/x/S/op",
				Name:      named("op"),
				Responses: []ir.Response{r},
			}},
		}},
	}}
	return doc
}

// TestVerify_AbsentNameIsAViolation is the case every content rule was
// vacuously true of: an entity whose Naming carries nothing in any channel. The
// empty string is uncased, is a word sequence, and straddles no letter/digit
// boundary, so a nameless entity satisfied all three while leaving an emitter
// nothing to name it by (GitHub #251).
func TestVerify_AbsentNameIsAViolation(t *testing.T) {
	t.Parallel()
	got := irverify.Verify(modelNamed(ir.Naming{}))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/naming-absent", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name", got[0].Path)
}

// TestVerify_AnyOneChannelIsAName pins how little the presence rule asks for.
// It is not a rule about which channel a compiler should fill — the content
// checks and the compilers decide that — only that one of them is.
func TestVerify_AnyOneChannelIsAName(t *testing.T) {
	t.Parallel()
	for _, n := range []ir.Naming{
		{Source: "user", Canonical: "user"},
		{Canonical: "user"},         // a canonical carried without a source
		{Hint: "connection_domain"}, // an anonymous type's generated name
		{Source: "***"},             // a spelling with no word rune in it
	} {
		assert.NotContains(t, codesOf(irverify.Verify(modelNamed(n))), "ir/naming-absent",
			"naming %+v names the entity", n)
	}
}

// primitiveDoc holds the one node nameOptional still exempts, so a fixture about
// the exemption says nothing about the rest of the document.
func primitiveDoc() *ir.Document {
	doc := validDoc()
	doc.Types[ir.PrimTypeID(ir.PrimString)] = &ir.Primitive{
		TypeCommon: ir.TypeCommon{ID: ir.PrimTypeID(ir.PrimString)},
		Prim:       ir.PrimString,
	}
	return doc
}

// TestVerify_OptionalNameOwnersAreClean pins the one exemption left in
// nameOptional: a primitive is identified by its kind, so an empty Naming on one
// is the shape the IR expects. Without this, the rule would fail every document
// in the corpus, all of which intern primitives.
//
// It is also what holds the ".Name" path the exemption is recorded under: a node
// that spelled its Naming as any other field would stop matching and redden here.
func TestVerify_OptionalNameOwnersAreClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, irverify.Verify(primitiveDoc()))
}

// TestVerify_NamelessServerAndResponseAreViolations is the reach the exemptions
// cost while they stood. Both nodes are named entities the OpenAPI compiler now
// names — a server by its URL template, a response by its status-code key — so
// one arriving nameless is a compiler defect and no longer the expected shape
// (GitHub #258, #259). Re-exempting either type reddens this.
func TestVerify_NamelessServerAndResponseAreViolations(t *testing.T) {
	t.Parallel()
	server := validDoc()
	server.Servers = []ir.Server{{URLTemplate: "https://x.example"}}

	for name, tc := range map[string]struct {
		doc  *ir.Document
		path string
	}{
		"server": {server, "doc.Servers[0].Name"},
		"response": {respondingDoc(ir.Response{Conditions: ok200()}),
			"doc.Services[0].Groups[0].Operations[0].Responses[0].Name"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(tc.doc)
			require.Len(t, got, 1)
			assert.Equal(t, "ir/naming-absent", got[0].Code)
			assert.Equal(t, tc.path, got[0].Path)
		})
	}
}

// TestVerify_OptionalOwnerExemptsOnlyItsOwnName is the overreach guard: the
// exemption is recorded against one path, so it silences that node's own Naming
// and no other. A checker that noted "an exempt node was seen" as a document-wide
// flag instead passes every test that measures one node at a time, and goes
// silent on the whole corpus.
//
// The sibling axis is what this can reach. The descendant axis — a nameless node
// *inside* an exempt one — had ir.Response and its headers to measure it with,
// and with ir.Primitive the only exemption left there is no such pair: nothing
// under a primitive carries a Naming at all. Whoever adds the next exemption owns
// that half again.
func TestVerify_OptionalOwnerExemptsOnlyItsOwnName(t *testing.T) {
	t.Parallel()
	doc := primitiveDoc()
	doc.Types["t/x/M"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/M"}}
	got := irverify.Verify(doc)
	require.Len(t, got, 1, "the model, and not the exempt primitive beside it")
	assert.Equal(t, "ir/naming-absent", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name", got[0].Path)
}

// TestVerify_PresenceReachesANamingNoNameFieldOwns pins that the rule holds
// every Naming the walk reaches, not only the ones a node calls its name.
// Service.Renames is a map[TypeID]Naming — the rename target is the value, and
// no ".Name" path addresses it — so a rename to nothing would slip through a
// rule written against name fields. The exemptions are addressed by path; the
// rule is not.
func TestVerify_PresenceReachesANamingNoNameFieldOwns(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:      "s/x/S",
		Name:    named("s"),
		Renames: map[ir.TypeID]ir.Naming{"t/x/Model": {}},
	}}
	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/naming-absent", got[0].Code)
	assert.Equal(t, "doc.Services[0].Renames[t/x/Model]", got[0].Path)
}
