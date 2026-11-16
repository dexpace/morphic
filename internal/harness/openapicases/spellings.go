// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"encoding/json/jsontext"
	"slices"
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
	default:
		return "", "", false
	}
}

// spelt returns the schema a case writes at its position: for inline, t's
// body with members merged in; otherwise the use of the target saying members
// in spelling s. It is false where s writes no such use: the construct at index
// i of the construct axis stays where inline writes it (stays), s would write a
// key twice, or an entry spelling saying nothing writes wrap's document.
func (c cases) spelt(i int, s string, t target, members object) (object, bool) {
	if s == inline {
		return t.body.with(members), true
	}
	if len(members) == 0 && (s == entry || s == entryFirst) {
		return nil, false
	}
	switch c.staysAt(i) {
	case inlineAlone:
		return nil, false
	case besideToo:
		if s != beside {
			return nil, false
		}
	default:
	}
	return spell(s, object{{"$ref", "#/components/schemas/" + targetSchema}}, members)
}

// staysAt is where the construct at index i of the construct axis is written;
// none, saying nothing, is moved.
func (c cases) staysAt(i int) stays {
	if i == 0 {
		return moved
	}
	return c.constructs[(i-1)/2].stays
}

// spell returns the use of the target ref names saying k in spelling s, and
// false where s cannot write it: k names the keyword s writes the use with.
// Both products write their uses through it.
func spell(s string, ref, k object) (object, bool) {
	var use object
	switch s {
	case beside:
		use = slices.Concat(ref, k)
	case wrap:
		use = slices.Concat(object{{"allOf", []any{ref}}}, k)
	case entry, entryFirst, nested:
		use = object{{"allOf", entries(s, ref, k)}}
	default:
		return nil, false
	}
	return use, !repeats(use)
}

// entries is the allOf an entry spelling writes: the reference and, when k
// writes a keyword, an entry of it, in s's order and nesting.
func entries(s string, ref, k object) []any {
	out := []any{ref}
	if len(k) > 0 {
		out = append(out, k)
	}
	switch s {
	case entryFirst:
		slices.Reverse(out)
	case nested:
		out = []any{object{{"allOf", out}}}
	default:
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

// equal is what the case at, saying members, must equal: its partner
// spelling, nil for a spelling with none or whose partner writes nothing.
// Beside a $ref at 3.0 each keyword states the warning it draws there.
func (c cases) equal(at []int, members object) *harness.Equal {
	s := c.spellings[at[5]]
	partner, rule, ok := partnerOf(s)
	if !ok {
		return nil
	}
	if partner == entry && len(members) == 0 {
		partner = wrap // an entry saying nothing is wrap's document, which wrap writes
	}
	if _, writes := c.spelt(at[2], partner, c.targets[at[1]], members); !writes {
		return nil
	}
	to := slices.Clone(at)
	to[5] = slices.Index(c.spellings, partner)
	use := c.positions[at[4]].pointer
	eq := &harness.Equal{To: to, Rule: rulePrefix + rule, Relocate: relocation(use)}
	if s != beside || c.versions[at[0]].dialect {
		return eq
	}
	for _, m := range members {
		at := jsontext.Pointer(use).AppendToken(m.name)
		eq.Stated = append(eq.Stated, laterVersion(at))
		// docs/compilers/openapi.md#nullable: beside a $ref the keyword is a later
		// minor's construct, warned of as one, so it draws no second warning
		// there, where beside the allOf it draws validation-no-effect.
		if m.name == "nullable" && m.value == raw("true") {
			eq.Replaced = append(eq.Replaced, harness.Stated{Severity: ir.SeverityWarning,
				Code: "openapi/validation/validation-no-effect", Pointer: at})
		}
	}
	return eq
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
