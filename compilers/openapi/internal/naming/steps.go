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
// from enclosing, the hint of the position they start at, and reports whether
// that position is a composition branch, which branch names further. Each
// step consumes a token, bounding the walk.
func steps(enclosing string, rest []string) (hint string, branch bool) {
	c := chain{base: enclosing}
	for i := 0; i < len(rest); {
		var step int
		c, branch, step = stepOf(c, rest[i:])
		i += step
	}
	return compile.SubHints(c.base, c.parts...), branch
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

// stepOf applies the keyword at rest[0] to the enclosing hint, returning the
// hint of the position it leads to, whether that position is a composition
// branch, and how many tokens the step consumed: two for a keyword whose
// children are keyed or indexed, one otherwise. A keyword the lowering does not
// walk (not, if, then, else, an unknown one) is named after its own token: only
// a reference reaches it, so any stable spelling keeps its name independent of
// declaration order.
func stepOf(enclosing chain, rest []string) (hint chain, branch bool, step int) {
	keyword := rest[0]
	if role, ok := structuralRoles[keyword]; ok {
		return enclosing.then(role), false, 1
	}
	if len(rest) < 2 {
		return chain{base: keyword}, false, 1
	}
	child := rest[1]
	switch {
	case keyedSchemaMaps[keyword]:
		return chain{base: child}, false, 2
	case keyword == "patternProperties":
		return enclosing.then("pattern"), false, 2
	case (keyword == "prefixItems" || keyword == "enum") && isDecimalIndex(child):
		// A tuple's positional item, or the literal a member of an enum no
		// enumeration holds is lowered to.
		return enclosing.then(child), false, 2
	case compositionKeywords[keyword] && isDecimalIndex(child):
		return chain{base: positionalBranch(child)}, true, 2
	case keyword == "type" && isDecimalIndex(child):
		// One variant of a multi-typed schema, built from that schema itself,
		// so it is named as the schema is.
		return enclosing, false, 2
	}
	return chain{base: keyword}, false, 1
}

// structuralRoles maps each keyword whose one schema the lowering names by role
// to that role.
var structuralRoles = map[string]string{
	"items":                "item",
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
	hint, _ = steps(hint, tokens[start:])
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
