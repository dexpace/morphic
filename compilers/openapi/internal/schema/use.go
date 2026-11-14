// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
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
	// places[i] is where Referents[i] is written, in whichever document.
	places []referentPlace
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
	// nullable says the position's reference admits null, whatever narrowed says.
	nullable bool
	// deprecatedBase says the node at the position composes, as its Base, a
	// schema a keyword or a key deprecates, as a decorator over a deprecated
	// target does.
	deprecatedBase bool
}

// Site is u as annotation.Read takes it, for s, the schema written at the
// position. A position that lost its $ref still reads it, so a Site here is a
// reference site whatever s writes.
func (u Use) Site(s *oas3.Schema) annotation.Site {
	return annotation.Site{Kind: annotation.Reference, Node: s, Voices: u.Voices, Referents: u.Referents}
}

// SiteAt is Site for s written at pointer, saying what the dialect s is read
// in spells otherwise, which annotation.Read keeps (dialectIn).
func (u Use) SiteAt(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) annotation.Site {
	site := u.Site(s)
	site.Dialect = dialectIn(lowering.SchemaRulesAt(c, pointer), s)
	return site
}

// dialectIn is what s, read with rules, spells otherwise than 2020-12: the
// keyword naming its identifier, whether the dialect ignores it beside a $ref s
// writes, as the binder does, whether it defines `dependencies`, and whether it
// reads an `items` list as a tuple (listedTuple).
func dialectIn(rules oas.SchemaRules, s *oas3.Schema) annotation.Dialect {
	return annotation.Dialect{Identifier: rules.Identifier(), IdentifierIgnored: rules.IgnoresRefFields() && writesRef(s),
		Dependencies: rules.Defines("dependencies"), ListedTuple: rules.Takes("items", oas.List)}
}

// writesRef reports whether s writes a $ref the binder reads as one: a string,
// empty or not. A null, a mapping or a list writes none.
func writesRef(s *oas3.Schema) bool {
	n := nodeview.Deref(annotation.RawPropertyNode(s, "$ref"))
	return n != nil && n.Kind == yaml.ScalarNode
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
		return !u.takes(r.GetDefault())
	}))
}

// Default returns the default the nearest schema of u's chain writes for the
// carrier at pointer holding s, as DefaultChain reads the chain, and a warning
// at that default's own position where no ir.Value holds it. The carrier then
// takes none: a later hop's default is not what the nearest writes.
func (u Use) Default(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (*ir.Value, []ir.Diagnostic) {
	chain := u.Site(s).Chain()
	own := len(chain) - len(u.Referents) + u.narrowedFrom
	for i, r := range chain {
		d := r.GetDefault()
		if d == nil || u.narrowed != nil && i >= own && !u.takes(d) {
			continue
		}
		v, err := value.FromNode(d)
		if err != nil {
			return nil, []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.UnloweredConstruct, u.defaultAt(c, i, pointer),
				"default is no value the IR holds, so no property, header or parameter reading it takes one: %s",
				diag.OneLine(err))}
		}
		return &v, nil
	}
	return nil, nil
}

// defaultAt places the default the schema at index i of u.Site's chain writes,
// for the carrier at pointer: the site's own, a voice's, or a referent's where
// it is written, in whichever document. A referent the bindings place nowhere
// is reported at the carrier, which reads it.
func (u Use) defaultAt(c lowering.Ctx, i int, pointer jsontext.Pointer) ir.Provenance {
	switch v := i - 1; {
	case i == 0:
		return c.Place().At(pointer.AppendToken("default"))
	case v < len(u.Voices):
		return c.Place().At(u.Voices[v].At.AppendToken("default"))
	}
	r := i - 1 - len(u.Voices)
	if r >= len(u.places) || !u.places[r].known {
		return c.Place().At(pointer)
	}
	at := u.places[r]
	return at.doc.At(at.pointer.AppendToken("default"))
}

// takes reports whether the position admits v: the narrowing does, or v is the
// null its reference admits.
func (u Use) takes(v values.Value) bool {
	return u.narrowed.takes(v) || (u.nullable && isNullValue(v))
}

// lentKeys returns what the mapped keys of what u falls back to lend its
// position. They are read along the chain its keywords are, nearest first
// (Referents): the nearest deprecated, by a keyword or a key, lends its
// members, any stating a deprecation by a key states it of the use, and any
// asking for redaction asks it of the use. No `deprecated` withdraws them, as
// none withdraws another's keyword (annotation.EffectiveDeprecated).
func (u Use) lentKeys(c lowering.Ctx) lowering.Referent {
	from := lowering.Referent{Homes: u.deprecatedBase}
	lent := false
	for _, r := range u.Referents {
		read := keysOf(c, r)
		from.Redacted = from.Redacted || read.Redacted
		from.Deprecated = from.Deprecated || read.Deprecated
		from.Homes = from.Homes || r.GetDeprecated()
		if !lent && (read.Deprecated || r.GetDeprecated()) {
			from.Members, lent = read.Members, true
		}
	}
	return from
}

// keysOf reads what the mapped keys of r, a schema a use falls back to, lend
// it, as the policy answers once for each schema (lowering.Promoter.Referent).
func keysOf(c lowering.Ctx, r *oas3.Schema) lowering.Referent {
	promoter := c.Promoter()
	return promoter.Referent(r, r.GetDeprecated(), func() ir.Unmodeled {
		return annotation.ExtensionsNamed(r.GetExtensions(), promoter.Keys())
	})
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
	// admits is what the schema admits, read only where narrows says it is less
	// than ref admits: a default no narrowing excludes reads as beside a $ref.
	admits admitted
	// ref is nil for a refinement writing no $ref.
	ref *oas3.JSONSchema[oas3.Referenceable]
	// principal is a refinement's, nil for any other schema.
	principal *allOfBranch
	// lost marks a ref the position does not lower to, so no Base reaches it.
	lost bool
	// narrows reports that the schema admits less than ref admits, so admits
	// is read.
	narrows bool
}

// passageOf returns the passage of s, written at pointer, and false for a schema
// a use does not read past.
func passageOf(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (passage, bool) {
	if r, refined := refinementOf(c, s, pointer); refined {
		p := passage{admits: r.joint, principal: r.principal}
		if r.ref != nil {
			p.ref, p.lost = r.ref.schema, !r.principalIs(*r.ref)
			p.narrows = p.lost || r.held.narrowedTo(r.joint)
		}
		return p, true
	}
	ref := throughRef(c, s)
	if ref == nil {
		return passage{}, false
	}
	if slices.Contains(dispatchOf(s).skipped, "allOf") {
		w := newAdmission(c)
		return passage{ref: ref, lost: true, admits: w.written(s, w.nullable), narrows: true}, true
	}
	return passage{ref: ref}, true
}

// throughRef returns the one $ref a use of s reads on through, or nil: that of an
// allOf an enum or a const beside it passed over, or of one saying nothing an
// object holds, a refinement's or a decorator's. A union beside a composing allOf
// is distributed across it or kept beside it, so that allOf lends nothing. It
// reads the source and its bindings alone, since a chain asks it of every link.
func throughRef(c lowering.Ctx, s *oas3.Schema) *oas3.JSONSchema[oas3.Referenceable] {
	refs := slices.Collect(allOfRefBranches(s))
	if len(refs) != 1 {
		return nil
	}
	if slices.Contains(dispatchOf(s).skipped, "allOf") {
		return refs[0]
	}
	if !allOfMayRefine(s) || annotation.DeclaresAny(s, objectKeywords) || !refinableBranches(allOfBranches(s, "")) {
		return nil
	}
	if declaresUnion(s) && allOfComposes(c, s) {
		return nil
	}
	return refs[0]
}

// speakers returns what p's $ref says of a use, nearest first, as a Use's
// referents: what is written beside it, then what it lands on lends.
func (p passage) speakers(c lowering.Ctx) Use {
	var u Use
	u.lend(p.ref.GetSchema(), placeOf(c, p.ref))
	lent := lentBy(c, c.Bindings().Declared(p.ref))
	u.Referents, u.places = append(u.Referents, lent.Referents...), append(u.places, lent.places...)
	return u
}

// ownsNode reports whether p's principal lowered to a node of its own, which
// keeps what it writes, t being what the position lowered to.
func (p passage) ownsNode(c lowering.Ctx, ts *compile.Types, t ir.TypeRef) bool {
	return p.principal != nil && LoweredToOwnNode(c, ts, p.principal.pointer, t)
}

// UseOf reads what speaks for a carrier holding js at pointer, which lowered to
// t: what a $ref names, or what passageOf reads on through. A principal owning a
// node keeps its annotations there, so it speaks here as a referent does. A
// composition throughRef reads no $ref through lends nothing: its Model holds what
// it composes as Base and Mixins.
func UseOf(c lowering.Ctx, ts *compile.Types, js *oas3.JSONSchema[oas3.Referenceable], t ir.TypeRef, pointer jsontext.Pointer) Use {
	s := js.GetSchema()
	if resolve.IsRefSite(js, s) {
		u := lentBy(c, c.Bindings().Declared(js))
		u.nullable = t.Nullable
		return u
	}
	u := Use{Voices: positionVoices(c, ts, s, pointer, t), nullable: t.Nullable}
	p, ok := passageOf(c, s, pointer)
	if !ok {
		return u
	}
	if p.ownsNode(c, ts, t) {
		u.Principal = &annotation.Voice{Node: p.principal.schema.GetSchema(), At: p.principal.pointer}
		u.lend(u.Principal.Node, placeIn(c, p.principal.pointer))
	}
	if p.ref == nil {
		return u
	}
	u.narrowedFrom = len(u.Referents)
	speakers := p.speakers(c)
	u.Referents, u.places = append(u.Referents, speakers.Referents...), append(u.places, speakers.places...)
	if p.narrows {
		u.narrowed = &p.admits
	}
	return u
}

// nodeUse reads what speaks for the node a position owns at pointer, which
// lowered to target. A Base reaches what a $ref says, so the node falls back
// only to a $ref its lowering lost; of one its Base holds, it reads only
// whether a keyword or a key along it deprecates the node, which the node's own
// member keys then annotate (lowering.Referent.Homes).
func nodeUse(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, target ir.TypeRef) Use {
	u := Use{Voices: positionVoices(c, ts, s, pointer, target)}
	p, ok := passageOf(c, s, pointer)
	switch {
	case !ok:
	case p.lost:
		speakers := p.speakers(c)
		u.Referents, u.places = speakers.Referents, speakers.places
	case p.ref != nil:
		u.deprecatedBase = slices.ContainsFunc(p.speakers(c).Referents, func(r *oas3.Schema) bool {
			return r.GetDeprecated() || keysOf(c, r).Deprecated
		})
	}
	return u
}

// lentOf keys what lentBy read of one schema.
type lentOf struct {
	schema *oas3.Schema
}

// lentBy reads what js, the schema a $ref lands on, lends a use of it, nearest
// first: each hop of the chain of $refs from js that writes more than its $ref,
// the schema the chain ends on, the branches of its allOf that fold
// (lendingBranches), and past a refinement, a decorator or a narrowing enum
// whatever its own $ref lends in turn. Only that end is read for what it admits,
// so the narrowing starts past its branches. Every $ref landing on js asks, so it
// is read once a run.
func lentBy(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) Use {
	return lowering.Remembered(c, lentOf{js.GetSchema()}, func() Use { return readLent(c, js) })
}

// readLent reads what lentBy remembers. A run of aliases is read within one
// turn, so only the passages throughRef reads spend maxLentHops.
func readLent(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) Use {
	var u Use
	for turn := range maxLentHops {
		s, ok := u.lendRun(c, js)
		if !ok {
			break
		}
		if turn == 0 {
			u.narrowTo(c, s)
		}
		through := throughRef(c, s)
		if through == nil {
			break
		}
		u.lend(through.GetSchema(), placeOf(c, through))
		js = c.Bindings().Declared(through)
	}
	return u
}

// lendRun appends to u.Referents each alias of the run of $refs js starts that
// writes more than its $ref, then the schema the run ends on and its lending
// branches, and returns that schema. It is false, the run lending nothing past
// what it appended, where the run ends on no schema, as a chain that loops or
// fails does. The table's count of the chain's hops bounds the run.
func (u *Use) lendRun(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) (*oas3.Schema, bool) {
	e, ref := c.Bindings().Entry(js)
	if ref && e.Chain.Status != bind.Resolved {
		return nil, false
	}
	for range e.Chain.Hops + 1 {
		s := annotation.SchemaOf(js)
		if s == nil {
			return nil, false
		}
		at := placeOf(c, js)
		if !resolve.IsRefSite(js, s) {
			u.lend(s, at)
			u.lendBranches(c, s, at)
			return s, true
		}
		if writesMoreThanRef(s) {
			u.lend(s, at)
			u.lendBranches(c, s, at)
		}
		js = c.Bindings().Declared(js)
	}
	return nil, false
}

// narrowTo has u admit what s admits where s, the end of the first run of a
// chain, narrows what its own $ref admits: past it, a referent's default stands
// only where s admits it.
func (u *Use) narrowTo(c lowering.Ctx, s *oas3.Schema) {
	if p, ok := passageOf(c, s, ""); ok && p.narrows {
		u.narrowed, u.narrowedFrom = &p.admits, len(u.Referents)
	}
}

// lend appends s, written at, to what u falls back to.
func (u *Use) lend(s *oas3.Schema, at referentPlace) {
	u.Referents = append(u.Referents, s)
	u.places = append(u.places, at)
}

// lendBranches appends the lending branches of s, written at, each placed
// beneath it.
func (u *Use) lendBranches(c lowering.Ctx, s *oas3.Schema, at referentPlace) {
	for _, b := range lendingBranches(c, s) {
		u.lend(b.schema.GetSchema(), at.beneath(b.pointer))
	}
}

// writesMoreThanRef reports whether s, a schema writing a $ref, writes anything
// else, which a use reads; one writing its $ref alone lends nothing.
func writesMoreThanRef(s *oas3.Schema) bool {
	n := nodeview.Deref(s.GetRootNode())
	return n == nil || n.Kind != yaml.MappingNode || len(n.Content) > 2
}

// referentPlace is where a referent is written: the document placing it and the
// pointer there, unknown where the bindings place none.
type referentPlace struct {
	doc     provenance.Doc
	pointer jsontext.Pointer
	known   bool
}

// placeOf returns where the bindings read js, unknown for a schema they do not
// know or a document the compile did not read.
func placeOf(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) referentPlace {
	p, ok := c.Bindings().Site(js)
	if !ok {
		return referentPlace{}
	}
	in, ok := lowering.InDocument(c, p.Doc)
	if !ok {
		return referentPlace{}
	}
	return referentPlace{doc: in.Place(), pointer: p.Pointer, known: true}
}

// placeIn returns pointer of c's document as a place.
func placeIn(c lowering.Ctx, pointer jsontext.Pointer) referentPlace {
	return referentPlace{doc: c.Place(), pointer: pointer, known: true}
}

// beneath returns the place of what is written at below, relative to p.
func (p referentPlace) beneath(below jsontext.Pointer) referentPlace {
	if !p.known {
		return p
	}
	p.pointer += below
	return p
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
