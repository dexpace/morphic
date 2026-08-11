package irverify

import (
	"reflect"
	"strconv"
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

// checkNaming asserts every named entity has a name at all, and that the names
// it carries are what invariant #4 promises: neutral lower_snake word
// sequences, carrying no casing an emitter should own and no character that is
// not part of a word. It reuses the shared bounded walk to reach every ir.Naming
// value in the document, and reports whether that walk was cut short so a name
// past the cap cannot go unchecked in silence.
//
// Presence is separate from those content rules because each of them is
// vacuously true of the empty string: an entirely empty Naming satisfied all
// three while leaving an emitter nothing to name the entity by (GitHub #251).
//
// Canonical and Hint are both held. They differ in where the name came from —
// one from a spelling the source wrote, the other from the position the entity
// occupies — and not in what it has to be: a hint is the only name an anonymous
// type has, so it is exactly what an emitter renders that type's identifier
// from. A hint is still derived from a string the source spelled, though — a
// component key, an operationId, a header name — so while nothing held it, that
// spelling's casing and punctuation reached the IR through it (GitHub #54).
// Only the grammar rule stays canonical-only, because a hint has no source
// spelling beside it to be recomputed from.
//
// Naming.Aliases is held to none of those and to two rules of its own instead,
// because it is a verbatim channel rather than a name the IR decides — see
// appendAliasViolations, and ir.Naming.Aliases for why.
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
		vs = appendAliasViolations(vs, aliases, path)
		return false // Naming holds no references or nested Naming to descend into
	})
	return vs, truncated
}

// namingChannels reads every name channel off one Naming. It reads fields
// rather than converting the value back to an ir.Naming because a value the walk
// reached through an unexported field cannot be (see ir.WalkValues) — which is
// also why the aliases are copied out element by element rather than through
// Interface().
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

// appendAliasViolations holds one alias list to the two rules ir.Naming.Aliases
// states: every entry names something, and no entry repeats. That comment is
// where the argument lives for why those are the rules and why none of the
// neutrality rules above are — an alias is a spelling the IR records rather than
// one it decides.
//
// Blank rather than empty is the line, and isBlankName is how wide it goes.
// Deciding whether a space *inside* "com.example. User" is legal would need the
// grammar of the format the alias is matched under, which the IR does not know;
// deciding that an entry has nothing visible in it at all needs no grammar.
//
// A repeat is reported at its later entry, so the path names the one to delete
// rather than the one to keep. A blank repeat is reported blank: the repair is
// to fill it in or drop it, not to distinguish it from the other blank.
//
// Two neighbouring defects are deliberately left out of scope here. Repeats
// across two Namings — the ambiguity that actually changes what a reader
// resolves — need the whole document rather than one list, and land in
// checkDuplicateIDs' shape (GitHub #398). And every other []string in the IR
// (Namespace, Tags, Scopes, ContentTypes …) admits the same blank and repeated
// entries this rule rejects, held by nothing (GitHub #399).
func appendAliasViolations(vs []Violation, aliases []string, path string) []Violation {
	seen := make(map[string]bool, len(aliases))
	for i, alias := range aliases {
		at := path + ".Aliases[" + strconv.Itoa(i) + "]"
		switch {
		case isBlankName(alias):
			vs = append(vs, Violation{
				Code:    "ir/naming-alias-blank",
				Message: "alias is blank, so it matches no name",
				Path:    at,
			})
		case seen[alias]:
			vs = append(vs, Violation{
				Code:    "ir/naming-alias-duplicate",
				Message: "alias " + alias + " is listed more than once",
				Path:    at,
			})
		default:
			seen[alias] = true
		}
	}
	return vs
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

// isBlankName reports whether s holds no rune a name could be made of — the
// widest emptiness test there is that needs no format's grammar. A space, a
// control character and a zero-width format character are invisible under every
// grammar, so a string of nothing but those names nothing anywhere.
//
// strings.TrimSpace is not that test. unicode.IsSpace reports false for the
// zero-width joiners, the soft hyphen and the BOM — all category Cf — so an
// alias of nothing but U+200B or U+FEFF passes a check built on it while naming
// exactly as little as " " does, and so does one of nothing but U+0000, which is
// neither a space nor Cf.
func isBlankName(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) && !unicode.IsControl(r) && !unicode.Is(unicode.Cf, r) {
			return false
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
