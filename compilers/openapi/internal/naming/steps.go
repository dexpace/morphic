// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package naming

import (
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

// steps composes the hint of the position the schema keywords in rest lead to
// from enclosing, the hint of the position they start at. It reports whether
// that position is a composition branch, which Namer.branch names further,
// and whether its own title named it. title names the position consumed
// tokens of rest lead to by its title (titles.at), asked of each but one a
// step names by the document's own key, and a name it gives stands in for the
// one composed so far, so the positions beneath compose from it. Each step
// consumes a token, bounding the walk.
func steps(enclosing string, rest []string, title titler) (hint string, branch, titled bool) {
	c := chain{base: enclosing}
	if t := title.at(0, false); t != "" {
		c, titled = chain{base: t}, true
	}
	for i := 0; i < len(rest); {
		s := stepOf(c, rest[i:])
		i += s.tokens
		c, branch, titled = s.hint, s.branch, false
		if s.keyed {
			continue
		}
		if t := title.at(i, s.union); t != "" {
			c, branch, titled = chain{base: t}, false, true
		}
	}
	return compile.SubHints(c.base, c.parts...), branch, titled
}

// titler returns the name the title of the position consumed tokens of a walk
// lead to gives it, "" where it gives none; union marks a oneOf or anyOf
// branch, whose siblings' titles it is read beside.
type titler func(consumed int, union bool) string

// at is t's answer, and "" for a nil titler, which reads no title.
func (t titler) at(consumed int, union bool) string {
	if t == nil {
		return ""
	}
	return t(consumed, union)
}

// chain is a hint being composed: base, then each part compile.SubHint would
// append to it, joined once (compile.SubHints) rather than at every step, so a
// position n roles deep costs n parts rather than n² bytes.
type chain struct {
	base  string
	parts []string
}

// then is c with part composed onto it. steps keeps only the chain a step
// returns, so appending in place shares nothing still read.
func (c chain) then(part string) chain {
	c.parts = append(c.parts, part)
	return c
}

// step is what one keyword does to the hint being composed (stepOf): the hint
// of the position it leads to and how many tokens it consumed, two for a
// keyword whose children are keyed or indexed and one otherwise. branch marks
// a composition branch, union a oneOf or anyOf one, and keyed a position named
// as the document names it, by its own key or as the schema holding it, which
// no title renames.
type step struct {
	hint                 chain
	tokens               int
	branch, union, keyed bool
}

// stepOf applies the keyword at rest[0] to the enclosing hint. A keyword the
// lowering does not walk (not, if, then, else, an unknown one) is named after
// its own token: only a reference reaches it, so any stable spelling keeps its
// name independent of declaration order.
func stepOf(enclosing chain, rest []string) step {
	keyword := rest[0]
	if keyword == "items" && len(rest) > 1 && isDecimalIndex(rest[1]) {
		// A tuple's positional item written as a list of items, as the JSON
		// Schema drafts before 2020-12 spell one, named as prefixItems names it.
		return step{hint: enclosing.then(rest[1]), tokens: 2}
	}
	if role, ok := structuralRoles[keyword]; ok {
		return step{hint: enclosing.then(role), tokens: 1}
	}
	if len(rest) < 2 {
		return step{hint: chain{base: keyword}, tokens: 1}
	}
	child := rest[1]
	switch {
	case keyedSchemaMaps[keyword]:
		return step{hint: chain{base: child}, tokens: 2, keyed: definitionMaps[keyword]}
	case keyword == "patternProperties":
		return step{hint: enclosing.then("pattern"), tokens: 2}
	case (keyword == "prefixItems" || keyword == "enum") && isDecimalIndex(child):
		// A tuple's positional item, or the literal a member of an enum no
		// enumeration holds is lowered to.
		return step{hint: enclosing.then(child), tokens: 2}
	case keyword == "allOf" && isDecimalIndex(child):
		// An allOf branch is a conjunct of the schema holding it, so a node built
		// there is that schema's, and named as it is.
		return step{hint: enclosing, tokens: 2, branch: true, keyed: true}
	case compositionKeywords[keyword] && isDecimalIndex(child):
		return step{hint: chain{base: positionalBranch(child)}, tokens: 2, branch: true, union: true}
	case keyword == "type" && isDecimalIndex(child):
		// One variant of a multi-typed schema, built from that schema itself,
		// so it is named as the schema is.
		return step{hint: enclosing, tokens: 2}
	}
	return step{hint: chain{base: keyword}, tokens: 1}
}

// structuralRoles maps each keyword whose one schema the lowering names by role
// to that role: a tuple's rest is an item, whether 2020-12's items or a draft's
// additionalItems holds it.
var structuralRoles = map[string]string{
	"items":                "item",
	"additionalItems":      "item",
	"additionalProperties": "value",
	"contentSchema":        "content",
}

// keyedSchemaMaps are the keywords whose value maps a key to a schema that is
// named by the key alone: a property by its name, and the $defs, definitions,
// dependentSchemas and dependencies entries by theirs. patternProperties is
// keyed too but names by role, so stepOf takes it separately.
var keyedSchemaMaps = map[string]bool{
	"properties": true, "$defs": true, "definitions": true, "dependentSchemas": true, "dependencies": true,
}

// definitionMaps are the keyed maps whose key is the name the document gives
// the schema itself, as a component schema's is, where a property's or a
// dependency's names what holds it: a title documents such a schema rather
// than naming it.
var definitionMaps = map[string]bool{"$defs": true, "definitions": true}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

// positionalBranch names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranch(index string) string { return "variant_" + index }

// isDecimalIndex reports whether s is a non-empty run of ASCII digits — the
// shape a branch's or a variant's pointer segment takes.
func isDecimalIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// unresolvedName returns the name a $ref that lands on no position suggests
// from its text: the position its fragment spells, named from that pointer
// alone (pointerOwner) as a namer with no table names it, so
// `#/components/schemas/Cat~1Dog` suggests "Cat/Dog" (GitHub #505). A $ref
// spelling no pointer — a whole-document URI, a $anchor, a fragment whose
// bytes are not UTF-8 (GitHub #520) — suggests what refHint reads.
func unresolvedName(ref string) string {
	p := fragment.Pointer(ref)
	if !strings.HasPrefix(string(p), "/") || !utf8.ValidString(string(p)) {
		return refHint(ref)
	}
	tokens := slices.Collect(p.Tokens())
	start, hint := pointerOwner(tokens)
	hint, _, _ = steps(hint, tokens[start:], nil)
	return hint
}

// refHint returns what follows the last '/' of ref, percent-decoded when that
// decodes to UTF-8, so `./Fish%2DTank.yaml` suggests "Fish-Tank.yaml".
func refHint(ref string) string {
	name := ref
	if _, after, ok := strings.CutLast(ref, "/"); ok {
		name = after
	}
	if decoded, err := url.PathUnescape(name); err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return name
}
