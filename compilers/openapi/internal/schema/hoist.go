package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// maxSchemaDepth caps schema-lowering recursion (styleguide bounded-recursion
// rule). Interning by pointer is what terminates recursive and diamond schemas;
// this bound only guards pathologically deep inline nesting. It counts from where
// the lowering owning a position enters, and from TopLevelDepth for a node only
// a $ref's promise builds (hoistSubSchema): a $ref never lowers what it names
// (resolvePointer), so a position meets the bound at one depth, whichever
// reaches it first (GitHub #748).
const maxSchemaDepth = 256

// TopLevelDepth is the nesting a schema position outside the walk starts at.
// The walk counts its own frames, so every entry point into it begins at zero;
// naming it keeps a bare 0 out of the call sites that only pass it through.
const TopLevelDepth = 0

// registeredNode returns the node interning registered under id, and ok=false
// when there is none yet.
//
// A node the registry reports as Building is reached again only by a walk
// running below the frame building it, and that frame finishes the node, this
// declaration included, once its build returns (GitHub #749). Any other miss
// is a compiler bug no source can provoke, so it is reported as one: the caller
// was about to attach docs, examples or preserved constructs, which would
// otherwise vanish.
func registeredNode(c lowering.Ctx, ts *compile.Types, id ir.TypeID, pointer jsontext.Pointer) (ir.TypeDef, bool, []ir.Diagnostic) {
	td, ok := ts.Node(id)
	if ok {
		return td, true, nil
	}
	if ts.Building(id) {
		return nil, false, nil
	}
	return nil, false, []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
		"internal: type %q is named at this pointer but absent from the registry; its source constructs are dropped", id)}
}

// internNode is the single hoisting entry point: it derives pointer's stable
// TypeID (ids.Document.ForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID. build receives the built TypeCommon, so
// it never re-derives pointer, hint or id.
//
// A $ref promises the node rather than building it (resolvePointer), so every
// lowering reaching pointer does so in the context its position gives it, and
// the first build stands for all. A node a $ref hoists where no lowering
// interned one takes the hint the position's own lowering recorded
// (hoistSubSchema), so hint is always the position's.
func internNode(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, hint string,
	build func(common ir.TypeCommon) ir.TypeDef,
) ir.TypeID {
	id := c.IDs().ForPointer(pointer)
	return ts.Intern(c.IDs().Coordinate(pointer), id, func() ir.TypeDef { return build(commonFor(c, id, pointer, hint)) })
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the context-derived hint.
func commonFor(c lowering.Ctx, id ir.TypeID, pointer jsontext.Pointer, hint string) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: c.ProvenanceAt(pointer),
	}
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		common.Name = compile.NamingFor(name)
	} else {
		common.Anonymous = true
		common.Name = compile.NamingHint(hint)
	}
	return common
}

// internAlias interns a named Scalar at pointer whose Base is target, so a
// component (or a sibling-carrying schema) whose body lowered to a shared or
// referenced target still owns a resolvable node at its own TypeID. It carries
// the value constraints s writes, and the co-declared bound keyword no
// Constraints field holds, for the reason hoistScalar records.
func internAlias(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string,
	target ir.TypeRef,
) (ir.TypeID, []ir.Diagnostic) {
	var kept ir.Unmodeled
	cons, diags := ConstraintsOf(c, &kept, s, pointer)
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, kept)
		return &ir.Scalar{TypeCommon: common, Base: new(target), Constraints: cons}
	})
	return id, diags
}

// subSchemaHint names the node a $ref'd sub-schema pointer owns: the hint the
// position takes for where it is (ownHint), or, at a position that takes its
// target's name, the hint of the node its own $ref resolves to (targetHint).
//
// It names a position no declaration reached: where one did, hoistSubSchema
// takes the hint that declaration recorded. Beneath /components/schemas
// positionHint replays the declaration's hint exactly; elsewhere it names the
// position by the tokens it can read.
func subSchemaHint(refs *typed.Bindings, decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) string {
	hint, follow := ownHint(decl, pointer)
	if !follow {
		return hint
	}
	return cmp.Or(targetHint(refs, decl), hint)
}

// ownHint returns the hint the position at pointer takes for where it is, and
// whether a $ref written there names it after its target instead. decl is the
// schema written there, if known.
//
// Only a composition branch holding a $ref is named after its target, as the
// composition names it (branchHint); every other position is named for where it
// is, as positionHint replays it.
func ownHint(decl *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) (hint string, follow bool) {
	hint, branch := positionHint(pointer)
	return hint, branch && decl != nil && decl.IsReference()
}

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

// maxTargetHintHops bounds targetHint's walk along composition branches that
// hold a $ref: a loop of them, and any longer chain, stops there. An acyclic
// chain longer than the cap is legal, though no real document writes one: past
// it the walk keeps the name it has reached, so a union variant naming the
// chain's first branch can be named apart from the node it holds.
const maxTargetHintHops = 64

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

// positionalBranchHint names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranchHint(index string) string { return "variant_" + index }

// componentSchemaTokens is the length of /components/schemas/<name>, the
// pointer positionHint roots a component's walk at: the component itself.
const componentSchemaTokens = 3

// positionHint returns the hint the structural lowering gives the schema
// position at pointer, and whether it is a composition branch.
//
// It replays the lowering because the hint is composed: items is
// compile.SubHint(enclosing, "item"), so only a walk from the root tells the
// keyword from a property named items (GitHub #518). Beneath /components/schemas
// the replay is exact. Elsewhere the enclosing hint is not in the pointer, so
// the walk resets at every token it does not know, the name a position only
// references reach takes (GitHub #529). Each step consumes a token, bounding
// the walk.
func positionHint(pointer jsontext.Pointer) (hint string, branch bool) {
	tokens := slices.Collect(pointer.Tokens())
	start := min(1, len(tokens))
	if len(tokens) >= componentSchemaTokens && tokens[0] == "components" && tokens[1] == "schemas" {
		start = componentSchemaTokens
	}
	if start > 0 {
		hint = tokens[start-1]
	}
	for i := start; i < len(tokens); {
		var step int
		hint, branch, step = positionStep(hint, tokens[i:])
		i += step
	}
	return hint, branch
}

// positionStep applies the keyword at rest[0] to the enclosing hint, returning
// the hint of the position it leads to, whether that position is a composition
// branch, and how many tokens the step consumed: two for a keyword whose
// children are keyed or indexed, one otherwise. A keyword the lowering does not
// walk (not, if, then, else, an unknown one) is named after its own token: only
// a reference reaches it, so any stable spelling keeps its name independent of
// declaration order.
func positionStep(enclosing string, rest []string) (hint string, branch bool, step int) {
	keyword := rest[0]
	if role, ok := structuralRoles[keyword]; ok {
		return compile.SubHint(enclosing, role), false, 1
	}
	if len(rest) < 2 {
		return keyword, false, 1
	}
	child := rest[1]
	switch {
	case keyedSchemaMaps[keyword]:
		return child, false, 2
	case keyword == "patternProperties":
		return compile.SubHint(enclosing, "pattern"), false, 2
	case keyword == "prefixItems" && isDecimalIndex(child):
		return compile.SubHint(enclosing, child), false, 2
	case compositionKeywords[keyword] && isDecimalIndex(child):
		return positionalBranchHint(child), true, 2
	}
	return keyword, false, 1
}

// structuralRoles maps each keyword whose one schema the structural lowering
// names by role to that role: the suffixes its compile.SubHint call sites pass. A
// change to one of them has to be made here too, and
// TestInlinePosition_HintIsTheSameInBothOrders is what fails when they drift,
// provided its row aims the outside $ref above the node it asserts (see the
// refAt column there): a reference aimed at the node itself is renamed by the
// declaration in either order and cannot see a role missing here.
var structuralRoles = map[string]string{
	"items":                "item",
	"additionalProperties": "value",
	"contentSchema":        "content",
}

// keyedSchemaMaps are the keywords whose value maps a key to a schema that is
// named by the key alone: a property by its name, and the $defs, definitions,
// dependentSchemas and dependencies entries by theirs, as the pointer's last
// token always named them. patternProperties is keyed too but names by role, so
// positionStep takes it separately.
var keyedSchemaMaps = map[string]bool{
	"properties": true, "$defs": true, "definitions": true, "dependentSchemas": true, "dependencies": true,
}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

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
