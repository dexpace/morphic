// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxChainHops bounds how many $refs a mapping value's chain reads
// (styleguide bounded-everything rule). A chain no loop ends that is longer
// stops at the position the bound reached, as though that position wrote no
// $ref: a loop breaks the chain where the binding finds it, long before.
const maxChainHops = 64

// link is one position of a mapping value's chain, the schema held there,
// which a reference object the position's $ref passes may have the bindings
// read in another document (resolve.Scope.Target), and the ID of the type
// declared there (declaredAt).
type link struct {
	at   bind.Position
	decl *oas3.JSONSchema[oas3.Referenceable]
	id   ir.TypeID
}

// valueChain is where a mapping value leads: the position it names, then each
// position its chain of $refs passes, in order, to the first writing none,
// every one a position a type is declared at. broken reports that the last
// writes a $ref landing on no such position, or lies on a loop.
type valueChain struct {
	links  []link
	broken bool
}

// end is the last position of v, which holds at least one.
func (v valueChain) end() link { return v.links[len(v.links)-1] }

// chainFrom keys the chain read from a position and the schema held there
// (follow).
type chainFrom struct {
	at   bind.Position
	decl *oas3.JSONSchema[oas3.Referenceable]
}

// chainOf reads value, a value of d's mapping or its defaultMapping, as
// docs/compilers/openapi.md#discriminator does: the schema component of the
// entry document it names, whatever its characters, else the position it
// names as a URI reference written at d (resolve.Scope.MappingTarget). It is
// false for a value that resolves to no schema, which the caller reports. The
// chain from a position is read once a run, as many values may name it.
func chainOf(c lowering.Ctx, d *oas3.Discriminator, value string) (valueChain, bool) {
	at, decl, ok := valuePosition(c, d, value)
	if !ok {
		return valueChain{}, false
	}
	id, declared := declaredAt(c, at, decl)
	if !declared {
		return valueChain{}, false
	}
	return lowering.Remembered(c, chainFrom{at, decl}, func() valueChain {
		return follow(c, link{at: at, decl: decl, id: id})
	}), true
}

// valuePosition is the position value, a value of d's mapping, names, and the
// schema held there: the entry document's component it is the name of, else
// where it lands read as a URI reference, false where it lands on no schema.
func valuePosition(c lowering.Ctx, d *oas3.Discriminator, value string) (bind.Position, *oas3.JSONSchema[oas3.Referenceable], bool) {
	if c.DeclaresSchema(value) {
		at := bind.Position{Doc: bind.Source, Pointer: ids.Ptr("components", "schemas", value)}
		return at, c.Bindings().At(at), true
	}
	return c.RefScope().MappingTarget(d, value)
}

// follow returns the chain from start: each $ref read in turn, to the first
// position writing none, or past maxChainHops to the one the bound reached. A
// component is read through as any other position: one writing a $ref lowers
// to an alias over what it names, which is no subtype or variant, so a route
// naming it means what its $ref names.
func follow(c lowering.Ctx, start link) valueChain {
	scope := c.RefScope()
	chain := valueChain{links: []link{start}}
	for range maxChainHops {
		decl := chain.end().decl
		if _, writes := scope.Bindings.Entry(decl); !writes {
			break
		}
		next, ok := scope.Target(decl)
		l := link{at: next, decl: scope.Bindings.Declared(decl)}
		if ok {
			l.id, ok = declaredAt(c, l.at, l.decl)
		}
		if !ok {
			chain.broken = true
			break
		}
		chain.links = append(chain.links, l)
	}
	return chain
}

// declaredAt returns the ID of the type declared at at, holding decl, which a
// route can name, as resolveAt resolves it without promising it (declaredIn):
// a component the source declares, by its name, or a schema the compile can
// hoist, in a document it read, by the ID its pointer derives there.
func declaredAt(c lowering.Ctx, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable]) (ir.TypeID, bool) {
	in, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return "", false
	}
	id, _, ok := declaredIn(in, at.Pointer, decl)
	return id, ok
}

// hierarchyRoute is the type a value whose chain is chain routes to on a
// Model: the one declared where the chain ends, promised a node. It is false
// for a broken chain, whose last position is promised instead, so the $ref
// failing there is lowered and reported where it is written, though only the
// mapping reaches it.
func hierarchyRoute(c lowering.Ctx, ts *compile.Types, chain valueChain) (ir.TypeID, bool) {
	end := chain.end()
	id, ok := resolveAt(c, ts, end.at, end.decl)
	return id, ok && !chain.broken
}

// branchIndex is what a union's branches tell a mapping value: its variants,
// in branch order past the null branches, and the pointer of each one's
// branch. is keys each branch's own position and its variant's type by the
// first branch they are; passes keys each other position a branch's chain of
// $refs passes by the first branch passing it, and several by every branch
// passing it.
type branchIndex struct {
	variants []ir.Variant
	pointers []jsontext.Pointer
	is       map[ir.TypeID]int
	passes   map[ir.TypeID]int
	several  map[ir.TypeID][]int
}

// indexBranches indexes the branches of the union u, whose variants are
// built, so the mapping beside it routes to them (route). It reads the
// branches and the variants' IDs, never the registry, so no lowering order
// reaches an answer. A branch's chain is read as a value's is (follow).
func indexBranches(c lowering.Ctx, u unionAt, variants []ir.Variant) *branchIndex {
	branches, key, _ := unionBranches(u.holder)
	x := &branchIndex{variants: variants, is: map[ir.TypeID]int{}, passes: map[ir.TypeID]int{},
		several: map[ir.TypeID][]int{}}
	for n, b := range branches {
		// A non-null branch lacking its variant is a compiler bug
		// spellImplicitRoutes reports; it routes nothing here.
		i := len(x.pointers)
		if nullBranch(c, b) || i == len(variants) {
			continue
		}
		vptr := u.at + ids.Ptr(key, strconv.Itoa(n))
		x.pointers = append(x.pointers, vptr)
		keepFirst(x.is, variants[i].Type.Target, i)
		own, placed := c.Position(vptr)
		if id, ok := declaredAt(c, own, b); ok && placed {
			keepFirst(x.is, id, i)
		}
		for _, l := range follow(c, link{at: own, decl: b}).links[1:] {
			keepFirst(x.passes, l.id, i)
			x.several[l.id] = append(x.several[l.id], i)
		}
	}
	return x
}

// keepFirst keys id by i, unless an earlier branch has it.
func keepFirst(index map[ir.TypeID]int, id ir.TypeID, i int) {
	if _, ok := index[id]; !ok {
		index[id] = i
	}
}

// route returns the variant a value whose chain is chain routes to: at the
// first position of the chain a branch meets, the branch whose own position or
// type it is, else the first whose chain passes it, beside every branch
// passing it where several do. It is false where no branch meets the chain.
func (x *branchIndex) route(chain valueChain) (int, []int, bool) {
	for _, l := range chain.links {
		if i, ok := x.is[l.id]; ok {
			return i, nil, true
		}
		if i, ok := x.passes[l.id]; ok {
			return i, x.several[l.id], true
		}
	}
	return 0, nil, false
}

// discriminatorMapping resolves a discriminator's mapping into the type each
// tag routes to (routeEntry). An entry that routes nowhere is left out, never
// given an ID nothing backs (GitHub #14); a mapping left with none is nil,
// which routes by type name, and round-trips as written.
func discriminatorMapping(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, x *branchIndex, pointer jsontext.Pointer) (map[string]ir.TypeID, []ir.Diagnostic) {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make(map[string]ir.TypeID, m.Len())
	for tag, value := range m.All() {
		id, ok, entryDiags := routeEntry(c, ts, d, x, value, "discriminator mapping "+strconv.Quote(tag),
			pointer+ids.Ptr("discriminator", "mapping", tag))
		diags = append(diags, entryDiags...)
		if ok {
			out[tag] = id
		}
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// discriminatorDefault resolves an OpenAPI 3.2 defaultMapping to the type it
// routes to, as discriminatorMapping resolves an entry.
func discriminatorDefault(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, x *branchIndex, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	dm := d.GetDefaultMapping()
	if dm == "" {
		return "", nil
	}
	id, ok, diags := routeEntry(c, ts, d, x, dm, "discriminator defaultMapping", pointer+ids.Ptr("discriminator", "defaultMapping"))
	if !ok {
		return "", diags
	}
	return id, diags
}

// routeEntry returns the type value, the entry of d that subject names,
// written at at, routes to: on a union the variant route gives, and on a model,
// or where no branch meets the chain, the type it ends on (hierarchyRoute),
// which pass.Validate holds to being a subtype or a variant. A value that
// resolves to no schema is reported here, once; one whose chain breaks routes
// nowhere, unreported, as its failing $ref is reported where it is written.
func routeEntry(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, x *branchIndex, value, subject string,
	at jsontext.Pointer,
) (ir.TypeID, bool, []ir.Diagnostic) {
	chain, ok := chainOf(c, d, value)
	if !ok {
		return "", false, []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef, at,
			"%s references unresolved schema %q%s", subject, value, mappingWhy(c, d, value))}
	}
	if x == nil {
		id, ok := hierarchyRoute(c, ts, chain)
		return id, ok, nil
	}
	i, among, routed := x.route(chain)
	if !routed {
		id, ok := hierarchyRoute(c, ts, chain)
		return id, ok, nil
	}
	if len(among) < 2 {
		return x.variants[i].Type.Target, true, nil
	}
	// Only the branches the report shows are spelled, so an entry costs no more
	// for a union whose every branch aliases the type it names.
	shown := among[:min(len(among), maxReportedTags)]
	pointers := make([]string, 0, len(shown))
	for _, b := range shown {
		pointers = append(pointers, string(x.pointers[b]))
	}
	return x.variants[i].Type.Target, true, []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"%s names a schema %d branches of this union reach through a $ref (%s), and a tag routes to one "+
			"variant; routed to the first", subject, len(among), quotedOf(pointers, len(among)))}
}

// mappingWhy is the reason value, a value of d's mapping, reports when it
// resolves to no type: why it lands on no schema, or why the table never read
// it, as unresolvedWhy says for a $ref. A value holding no '#' may have been
// meant as a component's name, which is read first, so its reason says both
// readings failed.
func mappingWhy(c lowering.Ctx, d *oas3.Discriminator, value string) string {
	why := unreadWhy(c.Bindings(), d)
	if m, ok := c.Bindings().Mapping(d, value); ok {
		why = hopWhy(m.Hop)
	}
	if strings.Contains(value, "#") {
		return why
	}
	return ": the entry document declares no schema component of that name, and as a URI reference " +
		cmp.Or(strings.TrimPrefix(why, ": "), "it names no schema")
}

// resolveDiscriminatorRoutes resolves the mapping and defaultMapping values of
// s's discriminator, written at pointer, where no node holds it and the census
// keeps it verbatim: each value reaches what it names as a reference does, so
// the type each chain ends on is promised a node, and one naming no schema is
// reported at its entry, as on a model.
func resolveDiscriminatorRoutes(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	d := s.GetDiscriminator()
	if d == nil {
		return nil
	}
	_, diags := discriminatorMapping(c, ts, d, nil, pointer)
	_, defaultDiags := discriminatorDefault(c, ts, d, nil, pointer)
	return append(diags, defaultDiags...)
}

// tagsOf keys the keys each type a discriminator's mapping routes to on a model
// (mappingTagsFor).
type tagsOf struct {
	d *oas3.Discriminator
}

// mappingTagsFor returns every key d's mapping spells for the type id, sorted
// in byte order, as the caller elects the first and a mapping's key order is
// no property of the document. They are the keys whose chain ends on id
// unbroken, as hierarchyRoute routes them, so a base's Mapping and its
// subtypes' tags cannot disagree. Nothing sends the caller to the implicit
// name, unlike the empty key, which is a tag. The mapping is read once a run,
// not once for each subtype asking.
func mappingTagsFor(c lowering.Ctx, d *oas3.Discriminator, id ir.TypeID) []string {
	if d.GetMapping().Len() == 0 {
		return nil
	}
	return slices.Clone(lowering.Remembered(c, tagsOf{d}, func() map[ir.TypeID][]string {
		tags := map[ir.TypeID][]string{}
		for tag, value := range d.GetMapping().All() {
			if chain, ok := chainOf(c, d, value); ok && !chain.broken {
				tags[chain.end().id] = append(tags[chain.end().id], tag)
			}
		}
		for _, keys := range tags {
			slices.Sort(keys)
		}
		return tags
	})[id])
}
