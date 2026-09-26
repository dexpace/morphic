package irverify_test

import (
	"encoding/json/v2"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// utf8Carrier is one place in the document graph that can hold an ill-formed
// string, and what the repaired shape looks like once it does not.
type utf8Carrier struct {
	// name identifies the carrier in test output.
	name string
	// plant returns a document that is otherwise clean except for s at this
	// carrier's one position, plus the ir/invalid-utf8 paths s produces there.
	plant func(s string) (*ir.Document, []string)
	// repair is a well-formed, neutral replacement for s that also satisfies
	// whatever other rule this position enforces — the naming grammar for
	// Canonical and Hint, agreement with an existing alias list for the alias
	// carrier — so the repaired document has nothing left for any check to
	// report.
	repair string
}

// utf8Carriers is every carrier GitHub #507 named: a type's registry key and
// its own ID, a documentation string, a typed string value, the document's own
// title, a diagnostic's message and provenance pointer, an Unmodeled key, each
// Naming channel, and an alias. Each plant function builds a document that
// verifies clean apart from the one string under test — reusing validDoc,
// modelNamed and friends is what keeps that true without restating it per row.
func utf8Carriers() []utf8Carrier {
	return []utf8Carrier{
		{
			name: "type registry key and its node ID",
			plant: func(s string) (*ir.Document, []string) {
				id := ir.TypeID("t/x/" + s)
				m := &ir.Model{ID: id, Name: ir.Naming{Source: "Model", Canonical: "model"}}
				doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{id: m}}
				path := "doc.Types[" + string(id) + "]"
				return doc, []string{path + ".key", path + ".ID"}
			},
			repair: "M",
		},
		{
			name: "Docs.Description",
			plant: func(s string) (*ir.Document, []string) {
				doc := validDoc()
				doc.Docs.Description = s
				return doc, []string{"doc.Docs.Description"}
			},
			repair: "fine",
		},
		{
			name: "a string Value.Str",
			plant: func(s string) (*ir.Document, []string) {
				doc := validDoc()
				doc.Types["t/x/L"] = &ir.Literal{
					ID:    "t/x/L",
					Name:  ir.Naming{Source: "l", Canonical: "l"},
					Value: ir.Value{Kind: ir.ValueString, Str: s},
				}
				return doc, []string{"doc.Types[t/x/L].Value.Str"}
			},
			repair: "fine",
		},
		{
			name: "Document.Name",
			plant: func(s string) (*ir.Document, []string) {
				doc := validDoc()
				doc.Name = s
				return doc, []string{"doc.Name"}
			},
			repair: "Fine API",
		},
		{
			name: "a diagnostic message",
			plant: func(s string) (*ir.Document, []string) {
				doc := validDoc()
				doc.Diagnostics = []ir.Diagnostic{
					// A leading, clean diagnostic built through the constructor pins
					// that the path addresses this entry by its own index rather than
					// the slice as a whole.
					ir.NewDiagnostic(ir.SeverityWarning, "openapi/validation", "clean", ir.Provenance{}),
					{Severity: ir.SeverityError, Code: "openapi/validation", Message: s},
				}
				return doc, []string{"doc.Diagnostics[1].Message"}
			},
			repair: "also clean",
		},
		{
			name: "a diagnostic's provenance pointer",
			plant: func(s string) (*ir.Document, []string) {
				doc := validDoc()
				doc.Diagnostics = []ir.Diagnostic{
					ir.NewDiagnostic(ir.SeverityError, "openapi/validation", "message", ir.Provenance{Pointer: s}),
				}
				return doc, []string{"doc.Diagnostics[0].Provenance.Pointer"}
			},
			// NewDiagnostic sanitizes Message, not Provenance: a validator-derived
			// pointer reaches the document exactly as handed to it (GitHub #520).
			repair: "/components/schemas/Foo",
		},
		{
			name: "an Unmodeled map key",
			plant: func(s string) (*ir.Document, []string) {
				doc := docWithUnmodeled(ir.Unmodeled{
					"openapi:" + s: {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)},
				})
				return doc, []string{"doc.Types[t/x/Model].Unmodeled[openapi:" + s + "].key"}
			},
			repair: "x-rate-limit",
		},
		{
			name: "Naming.Source",
			plant: func(s string) (*ir.Document, []string) {
				doc := modelNamed(ir.Naming{Source: s, Canonical: ir.CanonicalWords(s)})
				return doc, []string{"doc.Types[t/x/M].Name.Source"}
			},
			repair: "good",
		},
		{
			name: "Naming.Canonical",
			plant: func(s string) (*ir.Document, []string) {
				return canonicalOnly(s), []string{"doc.Types[t/x/M].Name.Canonical"}
			},
			repair: "good",
		},
		{
			name: "Naming.Hint",
			plant: func(s string) (*ir.Document, []string) {
				return hintOnly(s), []string{"doc.Types[t/x/M].Name.Hint"}
			},
			repair: "good",
		},
		{
			name: "a Naming alias",
			plant: func(s string) (*ir.Document, []string) {
				doc := modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: []string{"ok", s}})
				return doc, []string{"doc.Types[t/x/M].Name.Aliases[1]"}
			},
			repair: "good",
		},
	}
}

// TestVerify_InvalidUTF8IsReported drives one ill-formed string through every
// carrier utf8Carriers names and pins the property GitHub #507 is about:
// Verify reports ir/invalid-utf8 at the string's own walk path, the message
// quotes nothing and is itself readable, and the document fails to encode
// until that one string is repaired — at which point Verify has nothing left
// to say about it and the document encodes.
//
// Canonical and Hint each draw further violations from the very rune the
// check exists to catch: the replacement rune is neither lowercase-idempotent
// nor a word character, so ir/naming-cased and ir/naming-not-words both fire
// alongside ir/invalid-utf8 there. That is GitHub #400's concern, not this
// one, so those two rows are filtered to ir/invalid-utf8 before comparing
// paths — every other row has nothing else to filter out.
func TestVerify_InvalidUTF8IsReported(t *testing.T) {
	t.Parallel()
	ill := string([]byte{'c', 'a', 'f', 0xe9})
	require.False(t, utf8.ValidString(ill), "the fixture has to be ill-formed to test anything")

	for _, c := range utf8Carriers() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc, wantPaths := c.plant(ill)
			got := irverify.Verify(doc)

			var utf8Paths []string
			for _, v := range got {
				if v.Code != "ir/invalid-utf8" {
					continue
				}
				utf8Paths = append(utf8Paths, v.Path)
				assert.NotContains(t, v.Message, ill, "the report does not repeat the bad bytes")
				assert.True(t, utf8.ValidString(v.Message), "the report is itself readable")
			}
			assert.ElementsMatch(t, wantPaths, utf8Paths)

			_, err := json.Marshal(doc)
			require.Error(t, err, "a document holding one ill-formed string must not encode")

			clean, _ := c.plant(c.repair)
			assert.Empty(t, irverify.Verify(clean), "repairing the one bad string leaves nothing to report")
			_, err = json.Marshal(clean)
			require.NoError(t, err, "a document holding only well-formed strings must encode")
		})
	}
}

// TestVerify_RawPayloadUTF8IsNotDoublyReported pins the division of labour
// checkUTF8's own doc states: the walk skips byte sequences, so an ill-formed
// byte inside an Unmodeled or RawConfig payload is never reached by it. It is
// ir/invalid-raw-value's to report — jsontext.Value.IsValid already rejects
// invalid UTF-8 inside a payload — and reported once, not twice.
func TestVerify_RawPayloadUTF8IsNotDoublyReported(t *testing.T) {
	t.Parallel()
	doc := docWithUnmodeled(ir.Unmodeled{
		"openapi:x-rate-limit": {Reason: ir.ReasonVendorExtension, Value: badPayloads["invalid UTF-8"]},
	})
	assert.Equal(t, []string{"ir/invalid-raw-value"}, codesOf(irverify.Verify(doc)))
}

// TestVerify_ValidUTF8DiagnosticIsClean confirms a well-formed message — the
// only kind ir.NewDiagnostic can emit — raises no violation, even when its
// source text was ill-formed before coercion. Moved here from the removed
// diagnostics_test.go: it is the one diagnostic-specific claim that survives
// the fold into checkUTF8, since every carrier this file drives repairs by
// replacing the bad string directly rather than by routing it through a
// sanitizing constructor.
func TestVerify_ValidUTF8DiagnosticIsClean(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityError, "openapi/validation", "bad \xe0\xa5 byte", ir.Provenance{}),
	}
	assert.NotContains(t, codesOf(irverify.Verify(doc)), "ir/invalid-utf8")
}
