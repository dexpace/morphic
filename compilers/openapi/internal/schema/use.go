// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// maxLentHops bounds how many $refs one use reads on through (styleguide
// bounded-everything rule), a loop of allOfs each naming the next ending on it.
const maxLentHops = 64

// Use is what speaks for one position beside the schema written there: the
// branches of its allOf read as written beside it, then what it falls back to.
type Use struct {
	Voices []annotation.Voice
	// Referents is what the position falls back to, nearest first
	// (docs/compilers/openapi.md#ref-use-site).
	Referents []*oas3.Schema
	// Principal is a refinement's inline principal where it owns a node, and nil
	// otherwise: it is also the first of Referents.
	Principal *annotation.Voice
	// narrowed is what the position admits where a referent may say more, and nil
	// where every referent's default stands.
	narrowed *admitted
	// narrowedFrom is the first of Referents past the value set narrowing the
	// position: one before it writes that set or stands beside it, so its default
	// is the position's own, as a plain enum's is.
	narrowedFrom int
}

// Site is u as annotation.Read takes it, for s, the schema written at the
// position. A position that lost its $ref still reads it, so a Site here is a
// reference site whatever s writes.
func (u Use) Site(s *oas3.Schema) annotation.Site {
	return annotation.Site{Kind: annotation.Reference, Node: s, Voices: u.Voices, Referents: u.Referents}
}

// DefaultChain is u.Site(s)'s chain less each referent past the narrowing whose
// default the position does not admit: a default is a value, so unlike a
// description it binds only where the narrowed type takes it. An example never
// does: a target's stay on its node (docs/compilers/openapi.md#schema-examples).
func (u Use) DefaultChain(s *oas3.Schema) []*oas3.Schema {
	chain := u.Site(s).Chain()
	if u.narrowed == nil {
		return chain
	}
	own := len(chain) - len(u.Referents) + u.narrowedFrom
	return slices.Concat(chain[:own], slices.DeleteFunc(slices.Clone(chain[own:]), func(r *oas3.Schema) bool {
		return !u.narrowed.takes(r.GetDefault())
	}))
}

// Written returns the inline branches written for the position that a keeper of
// what only the position can hold reads beside its schema: u's voices, then a
// principal owning a node, which holds no such keyword itself.
func (u Use) Written() []annotation.Voice {
	if u.Principal == nil {
		return u.Voices
	}
	return append(slices.Clone(u.Voices), *u.Principal)
}

// passage is how a use of a schema reads past it, into the one $ref its allOf
// writes (throughRef).
type passage struct {
	// ref is nil for a refinement writing no $ref.
	ref *oas3.JSONSchema[oas3.Referenceable]
	// lost marks a ref the position does not lower to, so no Base reaches it.
	lost bool
	// principal is a refinement's, nil for any other schema.
	principal *allOfBranch
	// admits is what the schema admits.
	admits admitted
}

// passageOf returns the passage of s, written at pointer, and false for a schema
// a use does not read past.
func passageOf(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (passage, bool) {
	if r, refined := refinementOf(c, s, pointer); refined {
		p := passage{admits: r.joint, principal: r.principal}
		if r.ref != nil {
			p.ref, p.lost = r.ref.schema, !r.principalIs(*r.ref)
		}
		return p, true
	}
	ref := throughRef(s)
	if ref == nil {
		return passage{}, false
	}
	if slices.Contains(dispatchOf(s).skipped, "allOf") {
		w := newAdmission(c)
		return passage{ref: ref, lost: true, admits: w.written(s)}, true
	}
	return passage{ref: ref, admits: admitted{kinds: kindAny}}, true
}

// throughRef returns the one $ref a use of s reads on through, or nil: that of an
// allOf an enum or a const beside it passed over, or of one saying nothing an
// object holds, a refinement's or a decorator's. It reads the source alone, since
// a chain asks it of every link.
func throughRef(s *oas3.Schema) *oas3.JSONSchema[oas3.Referenceable] {
	refs := slices.Collect(allOfRefBranches(s))
	if len(refs) != 1 {
		return nil
	}
	if slices.Contains(dispatchOf(s).skipped, "allOf") {
		return refs[0]
	}
	if !allOfDecides(s) || annotation.DeclaresAny(s, objectKeywords) || !refinableBranches(allOfBranches(s, "")) {
		return nil
	}
	return refs[0]
}

// ownsNode reports whether p's principal lowered to a node of its own, which
// keeps what it writes, t being what the position lowered to.
func (p passage) ownsNode(c lowering.Ctx, ts *compile.Types, t ir.TypeRef) bool {
	return p.principal != nil && LoweredToOwnNode(c, ts, p.principal.pointer, t)
}

// UseOf reads what speaks for a carrier holding js at pointer, which lowered to
// t: what a $ref names, or what passageOf reads on through. A principal owning a
// node keeps its annotations there, so it speaks here as a referent does. A
// composition saying more than its $ref does lends nothing: its Model holds what
// it composes as Base and Mixins.
func UseOf(c lowering.Ctx, ts *compile.Types, js *oas3.JSONSchema[oas3.Referenceable], t ir.TypeRef, pointer jsontext.Pointer) Use {
	s := js.GetSchema()
	if resolve.IsRefSite(js, s) {
		return lentBy(c, c.RefScope().TargetSchema(js, s))
	}
	u := Use{Voices: positionVoices(c, ts, s, pointer, t)}
	p, ok := passageOf(c, s, pointer)
	if !ok {
		return u
	}
	if p.ownsNode(c, ts, t) {
		u.Principal = &annotation.Voice{Node: p.principal.schema.GetSchema(), At: p.principal.pointer}
		u.Referents = append(u.Referents, u.Principal.Node)
	}
	if p.ref == nil {
		return u
	}
	u.narrowedFrom = len(u.Referents)
	u.Referents = append(u.Referents, p.ref.GetSchema())
	u.Referents = append(u.Referents, lentBy(c, c.RefScope().TargetSchema(p.ref, p.ref.GetSchema())).Referents...)
	u.narrowed = &p.admits
	return u
}

// nodeUse reads what speaks for the node a position owns at pointer, which
// lowered to target. A Base reaches what a $ref says, so the node falls back
// only to a $ref its lowering lost.
func nodeUse(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, target ir.TypeRef) Use {
	u := Use{Voices: positionVoices(c, ts, s, pointer, target)}
	if p, ok := passageOf(c, s, pointer); ok && p.lost {
		u.Referents = lentBy(c, c.RefScope().TargetSchema(p.ref, p.ref.GetSchema())).Referents
	}
	return u
}

// lentBy reads what s, the schema a $ref names, lends a use of it, nearest first:
// s, the branches of its allOf that fold, and past a refinement, a decorator or a
// narrowing enum whatever its own $ref lends in turn. Only s itself is read for
// what it admits, the narrowest of the chain, so the narrowing starts past its
// branches. A chain of $refs is read as resolve.Scope.TargetSchema reads it, to
// its end, past what an alias on it writes (GitHub #1029).
func lentBy(c lowering.Ctx, s *oas3.Schema) Use {
	var u Use
	if p, ok := passageOf(c, s, ""); ok && p.ref != nil {
		u.narrowed = &p.admits
		u.narrowedFrom = 1 + len(foldingBranches(s))
	}
	for range maxLentHops {
		if s == nil {
			break
		}
		u.Referents = append(append(u.Referents, s), foldingBranches(s)...)
		through := throughRef(s)
		if through == nil {
			break
		}
		u.Referents = append(u.Referents, through.GetSchema())
		s = c.RefScope().TargetSchema(through, through.GetSchema())
	}
	return u
}

// DecidingSchema returns the schema deciding what s lowers to, s itself or, past
// a refinement, its principal's, for a reader of the shape alone. A principal
// owning a node, a $ref or a narrower inline branch, is what the position is.
func DecidingSchema(c lowering.Ctx, s *oas3.Schema) *oas3.Schema {
	for range maxLentHops {
		r, refined := refinementOf(c, s, "")
		if !refined || r.principal == nil {
			break
		}
		s = r.principal.schema.GetSchema()
		if isRefBranch(r.principal.schema) {
			s = c.RefScope().TargetSchema(r.principal.schema, s)
		}
	}
	return s
}
