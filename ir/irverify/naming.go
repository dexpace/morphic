package irverify

import (
	"reflect"
	"strings"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

var namingType = reflect.TypeFor[ir.Naming]()

// nameField is how a node spells the ir.Naming that names it, and so the last
// segment of the path the walk reaches that one by. A node in nameOptional
// renamed out of this spelling stops matching, which reports a violation on a
// document that has none rather than going silent —
// TestVerify_OptionalNameOwnersAreClean is what reddens.
//
// Only the exemptions are addressed this way. The rule itself holds every
// ir.Naming the walk reaches, including the ones no Name field owns, such as the
// values of Service.Renames.
const nameField = ".Name"

// nameOptional are the nodes that carry no name of their own, so an empty
// Naming on one is what the IR says to expect rather than the missing-name
// defect below.
//
// ir.Primitive is the only one, and it is exempt by design rather than pending
// work: it is identified by its PrimKind, so there is no source name to record
// and nothing for a hint to disambiguate — an emitter renders "string" from the
// kind. ir.Server and ir.Response were here for the other reason, as gaps the
// OpenAPI compiler had yet to fill, and each came off with the lowering that
// named it (GitHub #258, #259). An entry added for that reason is a debt: it
// makes every genuinely nameless node of that type invisible to this check for
// as long as it stands, so it belongs on a tracked issue and not in this map
// alone.
//
// Keyed by node type rather than by path so a new node type is held to the rule
// the moment it exists — the direction that fails loudly.
var nameOptional = map[reflect.Type]bool{
	reflect.TypeFor[ir.Primitive](): true,
}

// checkNaming asserts every named entity has a name at all, and that every
// Naming.Canonical is what invariant #4 promises: a neutral lower_snake word
// sequence, carrying no casing an emitter should own and no character that is
// not part of a word. It reuses the shared bounded walk to reach every ir.Naming
// value in the document, and reports whether that walk was cut short so a name
// past the cap cannot go unchecked in silence.
//
// Presence is separate from those content rules because each of them is
// vacuously true of the empty string: an entirely empty Naming satisfied all
// three while leaving an emitter nothing to name the entity by (GitHub #251).
//
// Only Canonical is checked for content. Naming.Hint — the generated-name
// channel — is held to none of the content rules, so casing
// and punctuation still reach the IR through it. That is GitHub #54, left open
// deliberately: closing it means changing how the compilers derive hints and
// regenerating every golden, which is a different change from tightening this
// checker.
func checkNaming(doc *ir.Document) ([]Violation, bool) {
	var vs []Violation
	optional := map[string]bool{}
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct {
			return true
		}
		if nameOptional[v.Type()] {
			// The walk visits a struct before its fields, so this is recorded
			// before the Naming it exempts is reached.
			optional[path+nameField] = true
			return true
		}
		if v.Type() != namingType {
			return true
		}
		source, canon, hint := namingChannels(v)
		if !optional[path] {
			vs = appendAbsentViolation(vs, source, canon, hint, path)
		}
		vs = appendNamingViolations(vs, source, canon, path)
		return false // Naming holds no references or nested Naming to descend into
	})
	return vs, truncated
}

// namingChannels reads the three name channels off one Naming. It reads fields
// rather than converting the value back to an ir.Naming because a value the walk
// reached through an unexported field cannot be (see ir.WalkValues).
func namingChannels(naming reflect.Value) (source, canon, hint string) {
	return naming.FieldByName("Source").String(),
		naming.FieldByName("Canonical").String(),
		naming.FieldByName("Hint").String()
}

// appendAbsentViolation reports an entity that no channel names.
//
// Which channel is filled is not this rule's business — a declared name goes in
// Source with its words beside it, a generated one in Hint, and the checks below
// hold whichever is there. This asks only that one of them is, because nothing
// downstream can render an identifier from three empty strings.
func appendAbsentViolation(vs []Violation, source, canon, hint, path string) []Violation {
	if source != "" || canon != "" || hint != "" {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/naming-absent",
		Message: "named entity carries no name at all; set a source name, canonical words or a hint",
		Path:    path,
	})
}

// appendNamingViolations reports the ways canon can break neutrality. They are
// checked separately because each can hold without the others — "userID" is
// segmented but cased, "com.example.user" is lowercase but unsegmented,
// "foo2bar" is both lowercase and made of word characters yet runs two words
// together — and each names a different repair.
func appendNamingViolations(vs []Violation, source, canon, path string) []Violation {
	vs = appendGrammarViolation(vs, source, canon, path)
	if isCased(canon) {
		vs = append(vs, Violation{
			Code:    "ir/naming-cased",
			Message: "canonical name " + canon + " carries casing; store neutral words",
			Path:    path,
		})
	}
	if !isWordSequence(canon) {
		vs = append(vs, Violation{
			Code:    "ir/naming-not-words",
			Message: "canonical name " + canon + " is not a word sequence; split it on every non-word character",
			Path:    path,
		})
	}
	if !isSegmented(canon) {
		vs = append(vs, Violation{
			Code: "ir/naming-unsegmented",
			Message: "canonical name " + canon +
				" runs a letter and a digit together in one word; the grammar splits that boundary",
			Path: path,
		})
	}
	return vs
}

// appendGrammarViolation reports a canonical that is not what the grammar
// derives from the source beside it.
//
// This is the complete statement of invariant 4's second half, and it is the only
// check here that can see a camel-case boundary. Lowercasing erases the case
// change that marked one, so "userid" and "user_id" are both lower-cased word
// sequences with no letter/digit straddle: nothing decidable from Canonical alone
// separates a compiler that neutralized "userID" without splitting it from one
// that had a genuine single word. Recomputing from Source separates them
// (GitHub #164).
//
// Asked only of a Naming that carries a Source. An anonymous type has none — it
// carries a Hint instead — and a Naming with neither is the zero value, which no
// grammar produced and none should be measured against.
//
// The three checks below still stand on their own rather than being subsumed:
// they hold a Canonical carried without a Source, which this one cannot ask
// about, and they name the specific way a value is wrong where this one can only
// say it disagrees.
func appendGrammarViolation(vs []Violation, source, canon, path string) []Violation {
	if source == "" {
		return vs
	}
	want := ir.CanonicalWords(source)
	if canon == want {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/naming-not-derived",
		Message: "canonical name " + canon + " is not what the grammar derives from source " +
			source + " (" + want + "); emitters cannot tell which grammar produced it",
		Path: path,
	})
}

// isSegmented reports whether canon puts a boundary everywhere the canonical
// grammar requires one *inside* a run of word characters. Only the letter/digit
// boundary is decidable from the value alone: the grammar splits "APIKey2" into
// api|key|2, so no word it produces holds a letter next to a digit.
//
// It overlaps appendGrammarViolation wherever a Source is present, and is kept
// because it does not need one: a Canonical carried without a Source is measured
// by this and by nothing else.
func isSegmented(s string) bool {
	for _, word := range strings.Split(s, "_") {
		var prev rune
		for i, r := range word {
			if i > 0 && straddlesLetterDigit(prev, r) {
				return false
			}
			prev = r
		}
	}
	return true
}

// straddlesLetterDigit reports whether prev and r sit either side of a boundary
// the grammar splits on. A combining mark is transparent: it belongs to the
// letter it follows rather than starting a run of its own.
func straddlesLetterDigit(prev, r rune) bool {
	return (unicode.IsLetter(prev) && unicode.IsDigit(r)) ||
		(unicode.IsDigit(prev) && unicode.IsLetter(r))
}

// isWordSequence reports whether s is the shape Naming.Canonical promises: words
// joined by single underscores, each word made of letters, digits and the
// combining marks that belong to them. The empty string qualifies — a Naming may
// carry only a Hint, and a source name with no word rune in it has no words to
// report.
//
// It says nothing about where the boundaries fall inside a run of word
// characters: "foo2bar" and "foo_2_bar" are both word sequences by this test.
// isSegmented is what holds that line.
func isWordSequence(s string) bool {
	if s == "" {
		return true
	}
	for _, word := range strings.Split(s, "_") {
		if word == "" {
			return false // a leading, trailing, or doubled separator
		}
		for _, r := range word {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
				return false
			}
		}
	}
	return true
}

// isCased reports whether s still carries casing an emitter should own. The test
// is lowercase-idempotence, not unicode.IsUpper: a compiler neutralizes names
// with strings.ToLower, so a rune that has no lowercase form (double-struck ℤ,
// Mathematical Bold 𝐀, a Roman numeral) is already neutral even though IsUpper
// reports true for it.
func isCased(s string) bool {
	return strings.ToLower(s) != s
}
