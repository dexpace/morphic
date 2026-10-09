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

// typeCommonType is the node whose Name the alias-claim check reads. A type
// declares its aliases once, on the TypeCommon every kind embeds, so the walk
// reaches it at the owner's path and the check asks it rather than each kind.
var typeCommonType = reflect.TypeFor[ir.TypeCommon]()

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

// namespaceOwners are the nodes that carry a Namespace path — TypeCommon for a
// type's declared namespace, Service for a service's — so the one list rule a
// namespace has reaches each of them. It is keyed by node type rather than by
// path for the reason nameOptional is: a node type added to the IR that declares
// a namespace is held the moment it exists, and
// TestNamespaceOwners_CoverEveryNamespaceField reddens if this map and the
// []string Namespace fields the IR declares disagree.
//
// FieldPath is not here: it is a path, not a namespace, and it carries no list
// rule at all (see appendListViolations).
var namespaceOwners = map[reflect.Type]bool{
	reflect.TypeFor[ir.TypeCommon](): true,
	reflect.TypeFor[ir.Service]():    true,
}

// namespaceField is how an owner spells the namespace path the check reads, and
// so the segment before the index of the path a violation about one segment is
// reported at. It follows nameField: an owner renamed out of this spelling stops
// matching rather than reddening, which is why
// TestNamespaceOwners_CoverEveryNamespaceField holds the owner map and the field
// name together.
const namespaceField = ".Namespace"

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
// Aliases and Namespace paths have list rules of their own
// (appendListViolations).
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
		if namespaceOwners[v.Type()] {
			// The owner is not a Naming, so the walk descends into it and still
			// reaches the Naming it carries — that one's rules are checkNaming's
			// own, below.
			vs = appendNamespaceViolations(vs, v, path)
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

// aliasField is how a Naming spells its alias list, and so the segment before
// the index of the path a violation about one entry is reported at. It sits
// beside nameField const for the same reason: a Naming renamed out of this
// spelling stops matching, so the check reports a violation on a document that
// has none rather than going silent, and
// TestVerify_AliasPathIsSpelledAsTheWalkWould is what reddens.
const aliasField = ".Aliases"

// sourceField is how a Naming spells the source name a claim's path ends with.
// It follows nameField and aliasField for the same reason: the path is built by
// hand and the field it names must keep its spelling for the two to agree.
const sourceField = ".Source"

// listRules is how appendListViolations spells and judges one []string field:
// which list-intrinsic defects it admits, the Source for the redundancy rule,
// and a violation's words. Only Aliases and Namespace have rules. A repeat in
// Tags, Scopes or a server variable's Enum is the copying compiler's to report
// (GitHub #399 follow-up), as is that Enum's legal ""; a repeat in
// ContentTypes, RequestContentTypes or Encodings is undecided (GitHub #399);
// Versions, Added/Removed and FieldPath may legitimately repeat.
type listRules struct {
	// noun is how a message names one entry: "alias", "namespace segment".
	noun string
	// blank reports whether an entry with nothing visible in it is a defect.
	blank bool
	// repeat reports whether an entry an earlier one already admitted is a
	// defect.
	repeat bool
	// source is the entity's own name, so an entry equal to it is redundant.
	// It is "" for a field with no such name beside it, which disables the rule
	// rather than leaving it to fire on the empty string — a blank entry is the
	// blank rule's and never reaches this comparison.
	source string
	// blankMessage is the violation a blank entry draws.
	blankMessage string
}

// appendListViolations holds one []string field to the rules listRules selects.
//
// An entry is reported if it is blank (isBlankName) or repeats an earlier entry
// or rules.source; a repeat is reported at its later occurrence, naming the
// earlier. An entry that does not decode is checkUTF8's and skipped, since
// later rules would quote it. Canonical and Hint are never compared: the IR
// derived them, and no writer schema spelled them.
func appendListViolations(vs []Violation, list []string, listPath, codePrefix string, rules listRules) []Violation {
	seen := make(map[string]int, len(list))
	for i, entry := range list {
		at := listPath + "[" + strconv.Itoa(i) + "]"
		switch first, repeated := seen[entry]; {
		case rules.blank && isBlankName(entry):
			// First, so a repeated blank is reported blank: the repair is to
			// fill it in or drop it, not to tell it from the other blank.
			vs = append(vs, Violation{
				Code:    codePrefix + "-blank",
				Message: rules.blankMessage,
				Path:    at,
			})
		case !utf8.ValidString(entry):
			// checkUTF8 reports it.
		case rules.repeat && repeated:
			vs = append(vs, Violation{
				Code:    codePrefix + "-duplicate",
				Message: rules.noun + " " + strconv.Quote(entry) + " is listed here and at index " + strconv.Itoa(first),
				Path:    at,
			})
		case entry == rules.source:
			vs = append(vs, Violation{
				Code:    codePrefix + "-redundant",
				Message: rules.noun + " " + strconv.Quote(entry) + " is the entity's own source name, so it matches nothing more",
				Path:    at,
			})
		default:
			seen[entry] = i
		}
	}
	return vs
}

// appendAliasViolations holds one alias list to the rules ir.Naming.Aliases
// states — that comment is where the argument for them lives, and for why none
// of the neutrality rules above apply — by calling appendListViolations with all
// three: blank, repeat, and redundant with the entity's own Source.
func appendAliasViolations(vs []Violation, source string, aliases []string, path string) []Violation {
	return appendListViolations(vs, aliases, path+aliasField, "ir/naming-alias", listRules{
		noun:         "alias",
		blank:        true,
		repeat:       true,
		source:       source,
		blankMessage: "alias is blank, so it matches no name",
	})
}

// appendNamespaceViolations holds one namespace path to the rules a namespace
// has: a blank segment names no package or module, and a repeat admits nothing
// the earlier segment did. It reads the slice off the walked owner rather than
// converting the value back to its Go type, for the reason namingChannels does —
// a value reached through an unexported field cannot be — and hands the entries
// to appendListViolations. A namespace has no Source beside it to be redundant
// with, so the third rule is off.
func appendNamespaceViolations(vs []Violation, owner reflect.Value, path string) []Violation {
	list := owner.FieldByName("Namespace")
	segments := make([]string, list.Len())
	for i := range list.Len() {
		segments[i] = list.Index(i).String()
	}
	return appendListViolations(vs, segments, path+namespaceField, "ir/namespace", listRules{
		noun:         "namespace segment",
		blank:        true,
		repeat:       true,
		blankMessage: "namespace segment is blank, so it names no package or module",
	})
}

// aliasPath spells one alias entry the way ir.WalkValues would have reached it.
// checkNaming prunes at ir.Naming, so the walk never renders these itself —
// TestVerify_AliasPathIsSpelledAsTheWalkWould is what holds the two spellings
// together.
func aliasPath(path string, i int) string {
	return path + aliasField + "[" + strconv.Itoa(i) + "]"
}

// aliasClaim is one name a Naming claims: the name itself, the path it sits at,
// the owner that claimed it, and whether it is the entity's Source rather than
// an alias entry. The owner identifies the Naming for the same-Naming skip
// below; the path is where a violation about the claim is reported.
type aliasClaim struct {
	owner  string
	name   string
	path   string
	source bool
}

// checkAliasClaims asserts no two type-registry Namings claim one name: an alias
// is what a reader resolves against exactly one entity, so two claiming it make
// the match depend on which schema the reader was handed (GitHub #398).
//
// The scope is each Document.Types entry's TypeCommon.Name, not the whole
// document: a source scopes an alias to its own record, so Property aliases are
// not compared. A claim is a non-empty Source or a valid alias entry; matching
// is exact, and the first claimant in sorted registry order stands. ir-design
// §3.2 records the rule in full, including the collision it cannot reach.
func checkAliasClaims(doc *ir.Document, _ declarations) ([]Violation, bool) {
	first := map[string]aliasClaim{}
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != typeCommonType {
			return true
		}
		vs = appendClaimViolations(vs, first, typeCommonClaims(v, path))
		return true // the check reads the owner and prunes nothing below it
	})
	return vs, truncated
}

// appendClaimViolations folds one Naming's claims into the document's, reporting
// each that a different Naming already made. The map is the names claimed so
// far, first claimant standing; a claim from the Naming that made one already is
// left to the list rules, which report it at the entry with the repair that
// belongs to one list.
func appendClaimViolations(vs []Violation, first map[string]aliasClaim, claims []aliasClaim) []Violation {
	for _, c := range claims {
		held, taken := first[c.name]
		switch {
		case !taken:
			first[c.name] = c
		case held.owner == c.owner:
			// One Naming claiming a name twice is -duplicate's or -redundant's.
		case c.source && !held.source:
			// A later Source meeting an earlier alias is reported at the alias:
			// it is the entry the second entity's name collides with.
			vs = append(vs, sharedAliasViolation(c.name, held.path, c.path))
		case !c.source:
			vs = append(vs, sharedAliasViolation(c.name, c.path, held.path))
		default:
			// Source against Source: the IR does not rank two declared names.
		}
	}
	return vs
}

// typeCommonClaims returns the claims one TypeCommon's Name makes, Source before
// Aliases, each with the path ir.WalkValues reaches it by — aliasPath for an
// alias entry, and the hand-built Source path beside it. Reading the fields
// rather than converting the value back to an ir.TypeCommon is namingChannels'
// reason.
func typeCommonClaims(common reflect.Value, owner string) []aliasClaim {
	namingPath := owner + nameField
	name := common.FieldByName("Name")
	var claims []aliasClaim
	if source := name.FieldByName("Source").String(); isClaim(source) {
		claims = append(claims, aliasClaim{
			owner: owner, name: source, path: namingPath + sourceField, source: true,
		})
	}
	aliases := name.FieldByName("Aliases")
	for i := range aliases.Len() {
		alias := aliases.Index(i).String()
		if !isClaim(alias) {
			continue
		}
		claims = append(claims, aliasClaim{owner: owner, name: alias, path: aliasPath(namingPath, i)})
	}
	return claims
}

// isClaim reports whether one name channel entry is a name a reader could match,
// and so something two Namings can collide on. A blank entry and one whose bytes
// do not decode are both already reported — by the list rules and by checkUTF8,
// respectively — so repeating them here as a shared claim would only report one
// defect twice, under a name nothing can match.
func isClaim(name string) bool {
	return !isBlankName(name) && utf8.ValidString(name)
}

// sharedAliasViolation states the one code this check reports: the name, and the
// path of the other claimant — the first claimant's for a later alias, the
// Source's for a later Source that met an earlier alias. Both name and path are
// quoted, because both are document-derived text (GitHub #400).
func sharedAliasViolation(name, at, other string) Violation {
	return Violation{
		Code:    "ir/naming-alias-shared",
		Message: "alias " + strconv.Quote(name) + " is also claimed by " + strconv.Quote(other),
		Path:    at,
	}
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
			Message: channel + " " + strconv.Quote(name) + " carries casing; store neutral words",
			Path:    path,
		})
	}
	if !isWordSequence(name) {
		vs = append(vs, Violation{
			Code:    "ir/naming-not-words",
			Message: channel + " " + strconv.Quote(name) + " is not a word sequence; split it on every non-word character",
			Path:    path,
		})
	}
	if !isSegmented(name) {
		vs = append(vs, Violation{
			Code: "ir/naming-unsegmented",
			Message: channel + " " + strconv.Quote(name) +
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
		Message: "canonical name " + strconv.Quote(canon) + " is not what the grammar derives from source " +
			strconv.Quote(source) + " (" + strconv.Quote(want) + "); emitters cannot tell which grammar produced it",
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
