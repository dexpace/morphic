// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// The spellings of a use of a named target T saying K, every keyword the case
// writes beside the use, moved as one unit.
const (
	// beside writes {$ref: T, K}.
	beside = "beside"
	// wrap writes {allOf: [{$ref: T}], K}, the spelling the others equal.
	wrap = "wrap"
	// entry writes {allOf: [{$ref: T}, {K}]}.
	entry = "entry"
	// entryFirst writes {allOf: [{K}, {$ref: T}]}.
	entryFirst = "entryFirst"
	// nested writes {allOf: [{allOf: [{$ref: T}, {K}]}]}.
	nested = "nested"
	// besideEntry writes {allOf: [{$ref: T, K}]}, beside as an allOf's entry.
	besideEntry = "besideEntry"
)

// targetSchema is the component every spelling but inline writes the target
// as, and the use names.
const targetSchema = "T"

// rulePrefix is how a rule making two spellings one is cited.
const rulePrefix = "docs/compilers/openapi.md#"

// partnerOf returns the spelling s must equal and the rule making the two one,
// and false for wrap, the spelling the others equal, and inline, which names no
// target.
func partnerOf(s string) (partner, rule string, ok bool) {
	switch s {
	case beside:
		return wrap, "ref-use-site", true
	case entry:
		return wrap, "allof-inline-residue", true
	case entryFirst, nested:
		return entry, "allof-classification", true
	case besideEntry:
		return entry, "ref-use-site", true
	default:
		return "", "", false
	}
}

// spelt returns the schema a case writes at its position: for inline, t's
// body with u's members and entries merged in; otherwise the use of the target
// saying u in spelling s. It is false where s writes no such use: the unit
// stays where inline writes it, s would write a key twice, or an entry
// spelling saying nothing writes wrap's document.
func (c cases) spelt(u written, s string, t target) (object, bool) {
	if s == inline {
		return t.body.with(slices.Concat(u.members, allOfOf(u.entries))), true
	}
	if len(u.members) == 0 && writesWrapEmpty(s) {
		return nil, false
	}
	switch u.stays {
	case inlineAlone:
		return nil, false
	case besideToo:
		if s != beside {
			return nil, false
		}
	default:
	}
	return spell(s, object{{"$ref", "#/components/schemas/" + targetSchema}}, u.members, u.entries)
}

// writesWrapEmpty reports whether s, given no keyword, writes wrap's document,
// which wrap writes: the spellings writing the keywords in an entry of their
// own or beside the reference's entry.
func writesWrapEmpty(s string) bool { return s == entry || s == entryFirst || s == besideEntry }

// staysAt is where the construct at index i of the construct axis is written;
// none, saying nothing, is moved.
func (c cases) staysAt(i int) stays {
	if i == 0 {
		return moved
	}
	return c.constructs[(i-1)/2].stays
}

// spell returns the use of the target ref names saying k in spelling s, the
// allOf entries follow right after ref, and false where s cannot write it: k
// names the keyword s writes the use with. Both products write their uses
// through it.
func spell(s string, ref, k object, follow []any) (object, bool) {
	var use object
	switch s {
	case beside:
		use = slices.Concat(ref, k, allOfOf(follow))
	case wrap:
		use = slices.Concat(object{{"allOf", slices.Concat([]any{ref}, follow)}}, k)
	case entry, entryFirst, nested:
		use = object{{"allOf", allOfEntries(s, ref, k, follow)}}
	case besideEntry:
		first := slices.Concat(ref, k)
		if repeats(first) {
			return nil, false
		}
		use = object{{"allOf", slices.Concat([]any{first}, follow)}}
	default:
		return nil, false
	}
	return use, !repeats(use)
}

// allOfOf returns the member writing entries as an allOf, and none for no
// entry.
func allOfOf(entries []any) object {
	if len(entries) == 0 {
		return nil
	}
	return object{{"allOf", entries}}
}

// allOfEntries is the allOf an entry spelling writes: the reference, then
// follow, and, when k writes a keyword, an entry of it, last or, for
// entryFirst, first; nested writes them one allOf deeper.
func allOfEntries(s string, ref, k object, follow []any) []any {
	out := slices.Concat([]any{ref}, follow)
	switch {
	case len(k) == 0:
	case s == entryFirst:
		out = slices.Concat([]any{k}, out)
	default:
		out = append(out, k)
	}
	if s == nested {
		out = []any{object{{"allOf", out}}}
	}
	return out
}

// repeats reports whether o names one member twice.
func repeats(o object) bool {
	seen := make(map[string]bool, len(o))
	for _, m := range o {
		if seen[m.name] {
			return true
		}
		seen[m.name] = true
	}
	return false
}

// equal is what the case at, writing u, must equal: its partner spelling, nil
// for a spelling with none or whose partner writes nothing. Beside a $ref at
// 3.0 each keyword states the warning it draws there.
func (c cases) equal(at []int, u written) *harness.Equal {
	s := c.spellings[at[5]]
	partner, rule, ok := partnerOf(s)
	if !ok {
		return nil
	}
	if partner == entry && len(u.members) == 0 {
		partner = wrap // an entry saying nothing is wrap's document, which wrap writes
	}
	if readApart(s, u.members) {
		return nil
	}
	if _, writes := c.spelt(u, partner, c.targets[at[1]]); !writes {
		return nil
	}
	to := slices.Clone(at)
	to[5] = slices.Index(c.spellings, partner)
	use := c.positions[at[4]].pointer
	eq := &harness.Equal{To: to, Rule: rulePrefix + rule, Relocate: relocation(use)}
	if c.versions[at[0]].dialect {
		return eq
	}
	switch s {
	case beside:
		eq.Stated, eq.Replaced = besideRef(use, slices.Concat(u.members, allOfOf(u.entries)), use, u.members)
	case besideEntry:
		eq.Stated, eq.Replaced = besideRef(use+"/allOf/0", u.members, use+"/allOf/"+strconv.Itoa(1+len(u.entries)), u.members)
	default:
	}
	return eq
}

// besideRef returns the reports a 3.0 use states where it writes members
// beside a $ref in the schema at, its partner writing them in the schema at
// theirs: each draws the warning a later minor's keyword draws, and a
// nullable: true, which the partner warns has no effect where no type is
// written with it (docs/compilers/openapi.md#nullable), draws no second there.
func besideRef(at string, members object, theirs string, unit object) (stated, replaced []harness.Stated) {
	for _, m := range members {
		stated = append(stated, laterVersion(jsontext.Pointer(at).AppendToken(m.name)))
		if m.name == "nullable" && m.value == raw("true") && !typed(unit) {
			replaced = append(replaced, harness.Stated{Severity: ir.SeverityWarning,
				Code: "openapi/validation/validation-no-effect", Pointer: jsontext.Pointer(theirs).AppendToken(m.name)})
		}
	}
	return stated, replaced
}

// readApart reports whether the nullable-inference policy reads a 3.0
// nullable in members apart in spelling s and its partner, so the two are not
// one: on an allOf entry it folds into the position only from an entry writing
// no properties (docs/compilers/openapi.md#allof-inline-residue), and a
// nullable: true beside a $ref admits null for the whole reference, inferred,
// where beside a type on an entry it is declared
// (docs/compilers/openapi.md#nullable).
func readApart(s string, members object) bool {
	i := slices.IndexFunc(members, named("nullable"))
	if i < 0 {
		return false
	}
	properties := slices.ContainsFunc(members, named("properties"))
	switch s {
	case entry:
		return properties
	case besideEntry:
		return members[i].value == raw("true") && (properties || typed(members))
	default:
		return false
	}
}

// typed reports whether members write a type, a type written null being none:
// beside one a 3.0 nullable: true has an effect
// (docs/compilers/openapi.md#nullable).
func typed(members object) bool {
	return slices.ContainsFunc(members, func(m member) bool {
		return m.name == "type" && m.value != "null" && m.value != raw(`"null"`)
	})
}

// laterVersion is the warning a 3.0 keyword beside a $ref draws at at.
func laterVersion(at jsontext.Pointer) harness.Stated {
	return harness.Stated{Severity: ir.SeverityWarning, Code: "openapi/later-version-construct", Pointer: at}
}

// relocation returns the Relocate of a use at pointer use, or of every use
// beneath it where use ends in "/": each allOf index following use in a string
// holding it, and following an unmodeled name's "openapi:" prefix, erased.
func relocation(use string) func(string) string {
	if use == "" {
		panic("openapicases: a relocation of no use")
	}
	return func(s string) string {
		if rest, ok := strings.CutPrefix(s, "openapi:"); ok {
			return "openapi:" + strings.TrimPrefix(eraseAllOf("/"+rest), "/")
		}
		from := 0
		for range strings.Count(s, use) {
			end := from + strings.Index(s[from:], use) + len(use)
			if end == len(s) || s[end] == '/' || strings.HasSuffix(use, "/") {
				return s[:end] + eraseAllOf(s[end:])
			}
			from = end
		}
		return s
	}
}

// eraseAllOf returns path, a pointer, with each "/allOf/<index>" pair of its
// tokens left out.
func eraseAllOf(path string) string {
	tokens := strings.Split(path, "/")
	out := tokens[:0:0]
	for i := 0; i < len(tokens); i++ {
		if tokens[i] == "allOf" && i+1 < len(tokens) && isIndex(tokens[i+1]) {
			i++
			continue
		}
		out = append(out, tokens[i])
	}
	return strings.Join(out, "/")
}

// isIndex reports whether token is an array index: digits alone.
func isIndex(token string) bool {
	return token != "" && strings.Trim(token, "0123456789") == ""
}
