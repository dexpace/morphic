// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// finisher writes on a node, in hand, what the frames lowering its position
// keep there beyond its build: its declaration's facts, the keywords it has no
// field for, the families a dispatch passed over, the branches kept beside
// it, the kind-inference mark, a discriminator settled beside a union.
// internNode runs it once, on the node it builds, before the registry stores
// it, so no write looks its node up, and a visit finding the node built writes
// nothing. read is what the build read, and held what an inline principal's
// readers keep after the frames outside (principalKept), nil for none.
type finisher func(td ir.TypeDef, read keySet, held *principalKept)

// then returns the finisher running f, then g, either of which may be nil.
func (f finisher) then(g finisher) finisher {
	switch {
	case f == nil:
		return g
	case g == nil:
		return f
	default:
		return func(td ir.TypeDef, read keySet, held *principalKept) {
			f(td, read, held)
			g(td, read, held)
		}
	}
}

// run runs f, a nil one finishing nothing.
func (f finisher) run(td ir.TypeDef, read keySet, held *principalKept) {
	if f != nil {
		f(td, read, held)
	}
}

// finished is what one frame's finisher wrote: whether it ran, and what it
// reported, which the frame joins to its own findings where they always stood,
// so finishing a node in its build moves no finding.
type finished struct {
	ran   bool
	diags []ir.Diagnostic
}

// on returns the finisher writing with write, recording into f that it ran and
// what it reported.
func (f *finished) on(write func(td ir.TypeDef) []ir.Diagnostic) finisher {
	return func(td ir.TypeDef, _ keySet, _ *principalKept) {
		f.ran = true
		f.diags = append(f.diags, write(td)...)
	}
}

// internNode is the single hoisting entry point: it derives pointer's stable
// TypeID (ids.Document.ForPointer) and shared TypeCommon (commonFor) once, then
// interns build's result under that ID, finished by fin. build receives the
// built TypeCommon, so it never re-derives pointer, name or id, and returns
// what it read of the schema it builds from.
//
// A $ref promises the node rather than building it (resolvePointer), so every
// lowering reaching pointer does so in the context its position gives it, and
// the first build, finished, stands for all.
func internNode(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, build func(common ir.TypeCommon) (ir.TypeDef, keySet),
	fin finisher,
) ir.TypeID {
	id := c.IDs().ForPointer(pointer)
	return ts.Intern(c.IDs().Coordinate(pointer), id, func() ir.TypeDef {
		td, read := build(commonFor(c, id, pointer))
		if fin != nil && !ir.IsNilTypeDef(td) {
			fin(td, read, nil)
		}
		return td
	})
}

// commonFor builds the TypeCommon shared by every hoisted node at pointer. A
// top-level component schema is named (source + canonical words); any deeper
// inline position is anonymous and carries only the hint its coordinate names
// it by (lowering.NameAt), whichever lowering builds it.
func commonFor(c lowering.Ctx, id ir.TypeID, pointer jsontext.Pointer) ir.TypeCommon {
	common := ir.TypeCommon{
		ID:         id,
		Provenance: c.Place().At(pointer),
	}
	if name, state := ids.ComponentSchema(pointer); state == ids.NamedComponent {
		common.Name = ir.NamingFor(name)
	} else {
		common.Anonymous = true
		common.Name = compile.NamingHint(lowering.NameAt(c, pointer))
	}
	return common
}

// internOwnNode interns the node the position js, written at pointer, owns
// where it lowered to ref, a use of what ref names, and nothing else built one:
// over an object alone the Model composing it, as the allOf of its $ref alone
// composes it, so no Scalar stands over a model; over any other target, one
// no node answers, or where the use admits null, which a Base does not carry,
// the Scalar alias (docs/compilers/openapi.md#allof-classification). fin
// finishes whichever it builds.
func internOwnNode(c lowering.Ctx, ts *compile.Types, js *statement.Statement, pointer jsontext.Pointer, ref ir.TypeRef,
	fin finisher,
) []ir.Diagnostic {
	if kinds, _ := lowersTo(c, js); ref.Nullable || kinds != kindObject || !stated(js.Schema) {
		_, diags := internAlias(c, ts, js.Schema, pointer, ref, fin, stand{})
		return diags
	}
	comp := allOfComposition{pointer: pointer, entries: []allOfEntry{{schema: js, pointer: pointer}}}
	if js.Ref == nil {
		comp = readAllOf(c, js.Schema, pointer)
	}
	if !slices.ContainsFunc(comp.entries, func(e allOfEntry) bool {
		if e.schema.Ref == nil {
			return false
		}
		_, resolved := resolveSchemaRef(c, ts, e.schema)
		return resolved
	}) {
		// The reference lowered to the top type, already reported.
		_, diags := internAlias(c, ts, js.Schema, pointer, ref, fin, stand{})
		return diags
	}
	var diags []ir.Diagnostic
	internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		td, read, modelDiags := composeModel(c, ts, TopLevelDepth, js.Schema, pointer, true, common, comp)
		diags = modelDiags
		return td, read
	}, fin)
	return diags
}

// internAlias interns a Scalar at pointer whose Base is target, so a component
// (or a sibling-carrying schema) whose body lowered to a shared or referenced
// target still owns a resolvable node at its own TypeID. It carries the value
// constraints s writes that the types the position admits take (boundKinds),
// and the exclusive modifier no Constraints field holds, for the reason
// hoistScalar records. fin finishes the alias where this builds it, which is
// where st says.
func internAlias(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var kept ir.Unmodeled
	voices := positionVoices(c, s, pointer, target)
	if st.of != nil {
		voices = st.voices(c, s, pointer)
	}
	cons, diags := ConstraintsOf(c, &kept, s, voices, pointer, st.takes(c, s))
	return internAliasHolding(c, ts, cons, kept, boundsRead(s, st.takes(c, s), nil), st.pointer(pointer), target, fin), diags
}

// internReferenceAlias is internAlias for the home of a declaration whose census
// left it no node, which also holds the collection bounds s writes. A position
// that reached a shared node with one written had its alias hoisted by the
// census first, so this reads none it also keeps.
func internReferenceAlias(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var kept ir.Unmodeled
	cons, diags := referenceConstraintsOf(c, &kept, s, positionVoices(c, s, pointer, target), target, pointer)
	return internAliasHolding(c, ts, cons, kept, referenceReads(c, s), pointer, target, fin), diags
}

// internAliasHolding interns the alias over target, holding the bounds its caller
// read, those of read, and the keyword kept beside them, finished by fin.
func internAliasHolding(c lowering.Ctx, ts *compile.Types, cons *ir.Constraints, kept ir.Unmodeled, read keySet,
	pointer jsontext.Pointer, target ir.TypeRef, fin finisher,
) ir.TypeID {
	return internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, kept)
		return &ir.Scalar{TypeCommon: common, Base: new(target), Constraints: cons}, read
	}, fin)
}
