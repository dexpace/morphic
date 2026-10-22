package schema

import (
	"cmp"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// requireMembers lands each name of required on m, once m holds every own
// property: as Required on the property of that wire name, never cleared once
// set, and otherwise in m.Requires, once, in the order required names it. A
// name no own property declares is one a Base or Mixin declares, or one only
// the catch-all governs, and JSON Schema requires it all the same (ir-design
// §4.3). Each object lowering calls it once, on a model that lists none yet.
func requireMembers(m *ir.Model, required []string) {
	byWire := merge.WireNameIndex(m.Properties)
	listed := make(map[string]bool, len(required))
	for _, name := range required {
		if i, ok := byWire[name]; ok {
			m.Properties[i].Required = true
			continue
		}
		if !listed[name] {
			listed[name] = true
			m.Requires = append(m.Requires, name)
		}
	}
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(c lowering.Ctx, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	e, ok := c.Bindings().Entry(b)
	return ok && e.Chain.Member
}

// branchHint derives a composition branch's naming hint from its $ref target's
// name, falling back to a positional hint for inline branches. It names both a
// union variant and an allOf branch, which reach it as the same question.
//
// A $ref branch's answer is also subSchemaHint's, and must stay so: an outside
// $ref can name the branch pointer, and hoistSubSchema names it by
// subSchemaHint where no lowering of the composition recorded this hint, so
// the branch takes one name whether or not its composition is lowered.
func branchHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], i int) string {
	return branchHintAt(refs, b, strconv.Itoa(i))
}

// branchHintAt is branchHint for a branch whose index is already a token, as the
// last one of its pointer is.
func branchHintAt(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], index string) string {
	// Only a true reference carries a target name: IsReference() is precisely
	// GetSchema().Ref != "" for a non-bool branch, so a schema whose Ref pointer is
	// set but empty (IsReference() false) suggests no name.
	if b != nil && b.IsReference() {
		if name := targetHint(refs, b); name != "" {
			return name
		}
	}
	return positionalBranchHint(index)
}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

// positionalBranchHint names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranchHint(index string) string { return "variant_" + index }

// isDecimalIndex reports whether s is a non-empty run of ASCII digits — the
// shape a composition branch's pointer segment takes.
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

// maxTargetHintHops bounds targetHint's walk along composition branches that
// hold a $ref: a loop of them, and any longer chain, stops there. An acyclic
// chain longer than the cap is legal, though no real document writes one: past
// it the walk keeps the name it has reached, so a union variant naming the
// chain's first branch can be named apart from the node it holds.
const maxTargetHintHops = 64

// targetHint returns the hint of the node a $ref resolves to, so a union
// variant or allOf branch is named as the type it holds is (GitHub #521).
//
// Each position on the way is read as subSchemaHint names it (ownHint); a
// composition branch holding a $ref moves the walk on to its target, keeping
// the last name found. The fragment is percent-decoded, then RFC 6901
// unescaped, so `#/components/schemas/Cat~1Dog` suggests "Cat/Dog" (GitHub
// #505); one spelling no pointer ends the walk with refHint's name. A position
// under /paths keeps ownHint's guess, which the declaration replaces on the
// node (GitHub #729).
func targetHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) string {
	hint := ""
	for range maxTargetHintHops {
		ref := b.GetRef().String()
		pointer, ok := resolve.FragmentPointer(ref)
		if !ok {
			return cmp.Or(refHint(ref), hint)
		}
		decl := refs.Declared(b)
		own, follow := ownHint(decl, pointer)
		hint = cmp.Or(own, hint)
		if !follow {
			return hint
		}
		b = decl
	}
	return hint
}

// refHint returns the name a reference suggests when its fragment spells no
// pointer — a whole-document URI, a $anchor, a fragment that decodes to bytes
// that are not UTF-8 (GitHub #520): what follows its last '/', percent-decoded
// when that decodes to UTF-8, so `./Fish%2DTank.yaml` suggests "Fish-Tank.yaml".
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
