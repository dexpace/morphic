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

// checkNaming asserts every named entity has a name at all; that the names it
// carries are what invariant #4 promises — neutral lower_snake word sequences,
// carrying no casing an emitter should own and no character that is not part of
// a word. Whether a channel's bytes decode at all is checkUTF8's, which holds
// every string in the document to it. It reuses the shared bounded walk to
// reach every ir.Naming value in the document, and reports whether that walk
// was cut short so a name past the cap cannot go unchecked in silence.
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
// Naming.Aliases is held to none of those and to rules of its own instead,
// because it is a verbatim channel rather than a name the IR decides — see
// appendListViolations, and ir.Naming.Aliases for why.
//
// The Namespace path a node declares is held to the two list-intrinsic rules
// instead — a blank segment and a repeated one are both defects (GitHub #399) —
// on the node types namespaceOwners names, through appendNamespaceViolations.
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
// rather than converting the value back to an ir.Naming because a value the walk
// reached through an unexported field cannot be (see ir.WalkValues) — which is
// also why the aliases are copied out element by element rather than through
// Interface().
//
// Nothing guards the field lookups. A rename of any of these fields is a
// compile-clean change that reddens the naming tests on the next run either way:
// the three String() reads degrade to "<invalid Value>", and Len() on the
// invalid Value panics. Neither is reachable from a document — checkNaming only
// calls this for a value whose type is ir.Naming, so every field is present —
// and a guard for the unreachable one would be a statement no test can cover.
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
// which of the two list-intrinsic defects the field admits, the entity's own
// source name for the redundancy rule, and the words a violation uses. It
// carries the differences between the two lists that have a rule — what a
// message calls an entry, which rules apply, and the Source to compare against —
// so the implementation stays one function rather than one per list.
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

// appendListViolations holds one []string field to the rules its own field
// comment states, reporting each entry that breaks one. It is the one
// implementation behind every such list the verifier checks, so the rules for
// the lists it is *not* called with are recorded here rather than written as a
// second, differently-worded check:
//
//   - Namespace (TypeCommon, Service): blank and repeated segments are both
//     defects — a blank segment names no package or module, and a repeat admits
//     nothing the earlier segment did. Both callers below pass it.
//   - Tags (Operation, TypeCommon, Channel, Message, Server) and Scopes
//     (SchemeUse): a source may legally write a tag or a scope twice, so
//     deduplicating and diagnosing a repeat belongs to the compiler that copied
//     the list through, not to a structural check (GitHub #399 follow-up).
//   - ContentTypes, RequestContentTypes and Encodings: the entries are ordered
//     by priority, so a repeat is redundant rather than ambiguous; whether the
//     IR should hold it at all is undecided (GitHub #399).
//   - Server Enum: "" is a legal value for a server variable, so nothing here
//     has a blank rule; a repeat is the compiler's, as for Tags.
//   - Versions and Added/Removed: entries may legally repeat (a re-add cycle),
//     so neither rule applies.
//   - FieldPath: a path, not a set — a repeated segment is legitimate
//     ("a.b.a") — so no rule applies.
//
// Every rule it does apply is decidable from the list and the entity carrying
// it, with no grammar and no second node: whether an entry has anything visible
// in it (isBlankName), whether it repeats an earlier entry, and whether it
// repeats the entity's own Source. An entry whose bytes do not decode is
// checkUTF8's to report and is judged by nothing here, since the rules after it
// quote the entry and would repeat the bytes into their own message. A repeat is
// reported at its later occurrence, naming the earlier one, so the message says
// which to delete and which to keep. A blank repeat is reported blank: the
// repair is to fill it in or drop it, not to distinguish it from the other
// blank.
//
// Only Source is compared against. Canonical and Hint are names the IR derived
// for an emitter to render, never names a writer schema could have spelled, so
// an entry equal to one of those is not the redundancy this rule is about.
func appendListViolations(vs []Violation, list []string, listPath, codePrefix string, rules listRules) []Violation {
	seen := make(map[string]int, len(list))
	for i, entry := range list {
		at := listPath + "[" + strconv.Itoa(i) + "]"
		switch first, repeated := seen[entry]; {
		case rules.blank && isBlankName(entry):
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
// is what a reader resolves against exactly one entity, so two entities claiming
// it make the match depend on which schema the reader was handed (GitHub #398).
//
// The scope is the type registry — ir.TypeCommon.Name of each Document.Types
// entry — and deliberately not the whole document. A Naming hangs off several
// node types, but an alias is a schema-resolution name and a source that writes
// one scopes it to its own record: an Avro field alias belongs to the record
// that declares it, so two models in different namespaces legitimately stating
// the same short alias are not a collision a document-wide compare could tell
// from one that is. The registry is where a claim is matched across entities,
// and TestVerify_PropertyAliasesAcrossModelsAreClean pins that a Property's
// aliases stay outside this.
//
// A claim is a non-empty Source or one Aliases entry. A blank or ill-formed
// entry is not one: it is already reported by the list rules or by checkUTF8,
// and nothing can match it. Canonical and Hint never claim — they are names the
// IR derived rather than names a writer schema could have spelled. Matching is
// exact string equality, and the first claimant in walk order (sorted registry
// keys) stands: a later alias is reported at itself, naming the first claimant,
// and a later Source meeting an earlier alias is reported at the alias, naming
// the Source. Source against Source is never reported — the IR does not rank two
// declared names — and a repeat inside one Naming stays with -duplicate and
// -redundant, which name the two repairs that belong to one list.
//
// One collision stays out of reach: an alias equal to another type's
// namespace-qualified name (its Namespace path joined with its Source) is not
// caught unless the Source holds that full name, because a namespace is a path
// here rather than a string the comparison ever joins.
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

// isBlankName reports whether s holds no rune a name could be made of. Every
// rune it accepts as invisible is one Unicode itself classifies that way — a
// space, a control, a format character, or a default-ignorable one — so the
// judgement needs no format's grammar and this function decides nothing on its
// own account.
//
// strings.TrimSpace is not that test, and neither is IsSpace-plus-Cf. IsSpace
// reports false for the zero-width joiners, the soft hyphen and the BOM (all
// Cf), and all three predicates report false for U+3164 HANGUL FILLER and its
// two jamo siblings, which are default-ignorable and are the characters
// conventionally used to pass off a name as empty. An alias of nothing but any
// of these names exactly as little as " " does.
//
// It does not reach every rune that renders as whitespace: U+2800 BRAILLE
// PATTERN BLANK is a graphic character Unicode does not call invisible, so it is
// left alone rather than judged here — that is the boundary this test declines
// to cross without knowing the grammar the name is read under.
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
// Mathematical Bold 𝐀, a Roman numeral) is already neutral even though IsUpper
// reports true for it.
func isCased(s string) bool {
	return strings.ToLower(s) != s
}
