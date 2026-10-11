package irverify

import (
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
// Naming on one is expected rather than the missing-name defect below.
//
// ir.Primitive is the only one, exempt by design: its PrimKind identifies it,
// so there is no source name to record. An entry added for a gap a compiler has
// yet to fill hides every genuinely nameless node of that type while it stands,
// so it belongs on a tracked issue too.
//
// Keyed by node type rather than path so a new node type is held to the rule at
// once.
var nameOptional = map[reflect.Type]bool{
	reflect.TypeFor[ir.Primitive](): true,
}

// checkNaming asserts every named entity has a name, and that the names it
// carries are what invariant #4 promises: neutral lower_snake word sequences.
// Bytes that do not decode are checkUTF8's. It reports whether the bounded walk
// was cut short.
//
// Presence is separate because each content rule is vacuously true of the empty
// string (GitHub #251).
//
// Canonical and Hint are both held, since a hint derives from a source string
// and would carry its casing (GitHub #54). Only the grammar rule is
// canonical-only: a hint has no source spelling to recompute from.
//
// Naming.Aliases has rules of its own: see appendAliasViolations.
func checkNaming(doc *ir.Document, _ declarations) ([]Violation, bool) {
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
		source, canon, hint, aliases := namingChannels(v)
		if !optional[path] {
			vs = appendAbsentViolation(vs, source, canon, hint, path)
		}
		vs = appendNamingViolations(vs, source, canon, hint, path)
		vs = appendAliasViolations(vs, source, aliases, path)
		return false // Naming holds no references or nested Naming to descend into
	})
	return vs, truncated
}

// namingChannels reads every name channel off one Naming. It reads fields
// rather than converting back to an ir.Naming because a value the walk reached
// through an unexported field cannot be (see ir.WalkValues), which is also why
// aliases are copied element by element.
//
// Nothing guards the field lookups: renaming a field compiles clean and reddens
// the naming tests. A guard would be a statement no test can cover, since
// checkNaming calls this only for an ir.Naming.
func namingChannels(naming reflect.Value) (source, canon, hint string, aliases []string) {
	list := naming.FieldByName("Aliases")
	aliases = make([]string, list.Len())
	for i := range list.Len() {
		aliases[i] = list.Index(i).String()
	}
	return naming.FieldByName("Source").String(),
		naming.FieldByName("Canonical").String(),
		naming.FieldByName("Hint").String(),
		aliases
}

// appendAliasViolations holds one alias list to the rules ir.Naming.Aliases
// states, which also says why the neutrality rules do not apply.
//
// An entry is reported if it is blank (isBlankName) or repeats an earlier entry
// or the entity's own Source; a repeat is reported at its later occurrence,
// naming the earlier. An entry that does not decode is checkUTF8's and skipped,
// since later rules would quote it. Canonical and Hint are never compared: the
// IR derived them, and no writer schema spelled them.
//
// Out of scope: repeats across Namings (GitHub #398, pinned unreported by
// TestVerify_AliasSharedByTwoNamings) and other []string fields (GitHub #399).
func appendAliasViolations(vs []Violation, source string, aliases []string, path string) []Violation {
	seen := make(map[string]int, len(aliases))
	for i, alias := range aliases {
		switch first, repeated := seen[alias]; {
		case isBlankName(alias):
			// First, so a repeated blank is reported blank: the repair is to
			// fill it in or drop it, not to tell it from the other blank.
			vs = append(vs, Violation{
				Code:    "ir/naming-alias-blank",
				Message: "alias is blank, so it matches no name",
				Path:    aliasPath(path, i),
			})
		case !utf8.ValidString(alias):
			// checkUTF8 reports it.
		case repeated:
			vs = append(vs, Violation{
				Code:    "ir/naming-alias-duplicate",
				Message: "alias " + alias + " is listed here and at index " + strconv.Itoa(first),
				Path:    aliasPath(path, i),
			})
		case alias == source:
			vs = append(vs, Violation{
				Code:    "ir/naming-alias-redundant",
				Message: "alias " + alias + " is the entity's own source name, so it matches nothing more",
				Path:    aliasPath(path, i),
			})
		default:
			seen[alias] = i
		}
	}
	return vs
}

// aliasPath spells one alias entry the way ir.WalkValues would have reached it.
// checkNaming prunes at ir.Naming, so the walk never renders these itself —
// TestVerify_AliasPathIsSpelledAsTheWalkWould is what holds the two spellings
// together.
func aliasPath(path string, i int) string {
	return path + ".Aliases[" + strconv.Itoa(i) + "]"
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

// appendNamingViolations reports the ways one Naming can break neutrality: the
// grammar rule over the canonical, and the content rules over each channel that
// carries a name for an emitter to render.
func appendNamingViolations(vs []Violation, source, canon, hint, path string) []Violation {
	vs = appendGrammarViolation(vs, source, canon, path)
	vs = appendContentViolations(vs, "canonical name", canon, path)
	return appendContentViolations(vs, "name hint", hint, path)
}

// appendContentViolations reports the ways the name in one channel can break
// neutrality. channel is how the message spells which channel was wrong: the
// two share a Path and a defect class, so the message is what tells them
// apart.
//
// The three are checked separately because each can hold without the others —
// "userID" is segmented but cased, "com.example.user" is lowercase but
// unsegmented, "foo2bar" is both lowercase and made of word characters yet runs
// two words together — and each names a different repair.
func appendContentViolations(vs []Violation, channel, name, path string) []Violation {
	if isCased(name) {
		vs = append(vs, Violation{
			Code:    "ir/naming-cased",
			Message: channel + " " + name + " carries casing; store neutral words",
			Path:    path,
		})
	}
	if !isWordSequence(name) {
		vs = append(vs, Violation{
			Code:    "ir/naming-not-words",
			Message: channel + " " + name + " is not a word sequence; split it on every non-word character",
			Path:    path,
		})
	}
	if !isSegmented(name) {
		vs = append(vs, Violation{
			Code: "ir/naming-unsegmented",
			Message: channel + " " + name +
				" runs a letter and a digit together in one word; the grammar splits that boundary",
			Path: path,
		})
	}
	return vs
}

// appendGrammarViolation reports a canonical that is not what the grammar
// derives from the source beside it.
//
// It is the only check here that can see a camel-case boundary: lowercasing
// erases the case change that marked one, so nothing in Canonical alone
// separates a "userID" neutralized without splitting from a genuine single word
// (GitHub #164).
//
// Asked only of a Naming that carries a Source. The content rules are not
// subsumed: they hold a Canonical carried without one, and name the specific
// way a value is wrong.
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
	for word := range strings.SplitSeq(s, "_") {
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

// isWordSequence reports whether s is the shape a neutral name channel
// promises: words joined by single underscores, each word made of letters,
// digits and the combining marks that belong to them. The empty string
// qualifies — a Naming fills one channel or the other, and a source name with no
// word rune in it has no words to report.
//
// It says nothing about where the boundaries fall inside a run of word
// characters: "foo2bar" and "foo_2_bar" are both word sequences by this test.
// isSegmented is what holds that line.
func isWordSequence(s string) bool {
	if s == "" {
		return true
	}
	for word := range strings.SplitSeq(s, "_") {
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

// isBlankName reports whether s holds no rune a name could be made of. It takes
// as invisible only what Unicode itself classifies so, needing no format's
// grammar.
//
// strings.TrimSpace is not that test: IsSpace misses the zero-width joiners,
// the soft hyphen and the BOM (all Cf), and space, control and Cf all miss
// U+3164 HANGUL FILLER and its two jamo siblings, default-ignorable characters
// used to pass off a name as empty.
//
// U+2800 BRAILLE PATTERN BLANK is not so classified, so it is left alone:
// judging it would need the grammar the name is read under.
func isBlankName(s string) bool {
	for _, r := range s {
		if !isInvisible(r) {
			return false
		}
	}
	return true
}

// isInvisible reports whether Unicode classifies r as carrying no visible mark.
func isInvisible(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r)
}

// isCased reports whether s still carries casing an emitter should own. The test
// is lowercase-idempotence, not unicode.IsUpper: a compiler neutralizes names
// with strings.ToLower, so a rune that has no lowercase form (double-struck ℤ,
// Mathematical Bold 𝐀) is already neutral though IsUpper reports true for it.
func isCased(s string) bool {
	return strings.ToLower(s) != s
}
