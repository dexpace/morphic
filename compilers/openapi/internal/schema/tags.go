// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxReportedTags bounds how many variants the inferred-discriminator report
// names, so a union of hundreds says what it means in one line.
const maxReportedTags = 3

// deferUnionTags queues the discriminator-inference policy's reading of the
// union n, u lowered at pointer, where the policy is on: for the discriminator
// standing with it declaring no mapping, or for none at all
// (docs/compilers/openapi.md#discriminator-inference). It reads what each
// variant's type says of its tag, which only a registry every node is interned
// in answers in any declaration order, so it is a settlement. A mapping the
// document declares is read as written, beside the routes it implies
// (spellImplicitRoutes), so one of those its variant rejects stands (#1798).
// A discriminator n cannot hold (tagLocated) leaves n untagged, so nothing is
// read.
func deferUnionTags(c lowering.Ctx, ts *compile.Types, u unionAt, n *ir.Union, pointer jsontext.Pointer) {
	if !c.DiscriminatorInference.Honours() {
		return
	}
	if u.disc.GetMapping().Len() > 0 || u.disc != nil && n.Discriminator == nil {
		return
	}
	tags := unionTags{c: c, id: n.ID, pointer: pointer}
	if u.disc != nil {
		tags.at = u.discAt + ids.Ptr("discriminator")
		tags.implicit = implicitRoutes(c, u.holder)
	}
	ts.Defer(tags)
}

// unionTags settles the tag of the union id under the discriminator-inference
// policy, from the strings each variant admits for its tag property: a mapping
// for the discriminator written at at, which declares none, or, where at is
// empty, a discriminator for a union declaring none. What it reads no
// settlement after it changes (tagReadings), so the settling order changes no
// answer.
type unionTags struct {
	c        lowering.Ctx
	id       ir.TypeID
	pointer  jsontext.Pointer // where the union is written
	at       jsontext.Pointer // the discriminator's, "" for none
	implicit []implicitRoute  // per variant; nil for no discriminator
}

// Settle reads the union's tags, and writes them where the policy reads one.
func (t unionTags) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	u, ok := reg[t.id].(*ir.Union)
	if !ok || u == nil {
		return t.invariant("is not registered as one")
	}
	if t.at != "" {
		return t.mapDeclared(reg, u)
	}
	return t.inferUndeclared(reg, u)
}

// invariant reports a union the settlement finds otherwise than its lowering
// left it, a compiler bug: the reading is not taken.
func (t unionTags) invariant(why string) []ir.Diagnostic {
	return []ir.Diagnostic{t.c.DiagAt(ir.SeverityError, diag.InternalInvariant, t.pointer,
		"internal: union %q %s, so the discriminator inference deferred for it is not taken", t.id, why)}
}

// mapDeclared maps the declared discriminator of u by the strings each
// variant admits for its tag property, where every variant pins a set no
// other shares and the implicit mapping does not route each by all it admits:
// one rejecting the component name it routes by, one admitting another
// string, or one naming no component. Where each admits that name alone, the
// implicit mapping already says it.
func (t unionTags) mapDeclared(reg ir.TypeRegistry, u *ir.Union) []ir.Diagnostic {
	d := u.Discriminator
	if d == nil {
		return t.invariant("holds no discriminator, though its source declares one")
	}
	if len(t.implicit) != len(u.Variants) {
		return t.invariant("has a variant no branch stands for")
	}
	r := readingsOf(t.c)
	tags, ok := pinnedTags(reg, r, flattenVariants(reg, u.Variants), d.PropertyName, false /* required */)
	if !ok || routedByName(tags, t.implicit) {
		return nil
	}
	d.Mapping = tagMapping(tags, u.Variants)
	u.Provenance.Inferred = ir.JoinInferred(u.Provenance.Inferred, lowering.DiscriminatorInferenceHeuristic)
	return []ir.Diagnostic{t.c.DiagAt(ir.SeverityInfo, diag.InferredDiscriminator, t.at,
		"the discriminator declares no mapping, and OpenAPI's implicit mapping routes each variant only by the "+
			"name of the component it names, not by what it admits for %q (%s); "+
			"mapped by the values each admits, marked inferred",
		d.PropertyName, disagreements(tags, t.implicit))}
}

// routedByName reports whether the implicit mapping routes each variant by all
// it admits: the name of the component it names, and nothing else.
func routedByName(tags [][]string, implicit []implicitRoute) bool {
	for i, set := range tags {
		if !implicit[i].routesExactly(set) {
			return false
		}
	}
	return true
}

// inferUndeclared gives u, a closed union of two or more variants declaring no
// discriminator, the one its variants carry in all but name: the first property
// of the first variant, in layout order, that every variant requires and pins
// to a set no other shares. A payload then names its variant by that value, as
// validation would find it. An open union may meet a variant carrying any tag.
// The other variants are flattened once, and only where the first pins one,
// each keyed by wire name, so a candidate costs the same however many members
// a variant has.
func (t unionTags) inferUndeclared(reg ir.TypeRegistry, u *ir.Union) []ir.Diagnostic {
	if u.Discriminator != nil {
		return t.invariant("holds a discriminator, though its source declares none")
	}
	if !u.Closed || len(u.Variants) < 2 ||
		slices.ContainsFunc(u.Variants, func(v ir.Variant) bool { return v.Type.Nullable }) {
		return nil
	}
	r := readingsOf(t.c)
	first := ir.FlattenedProperties(reg, u.Variants[0].Type.Target)
	var flats []variantFlat
	for _, p := range first.Properties {
		if !p.Required || !r.object(reg, first, p, true, p.Property.EffectiveWireName()).pins() {
			continue
		}
		if flats == nil {
			flats = flattenVariants(reg, u.Variants)
		}
		name := p.Property.EffectiveWireName()
		tags, ok := pinnedTags(reg, r, flats, name, true /* required */)
		if !ok {
			continue
		}
		u.Discriminator = &ir.Discriminator{PropertyName: name, Mapping: tagMapping(tags, u.Variants), Inferred: true}
		u.WireTagged = true
		u.Provenance.Inferred = ir.JoinInferred(u.Provenance.Inferred, lowering.DiscriminatorInferenceHeuristic)
		return nil
	}
	return nil
}

// variantFlat is one variant's flattening and its members keyed by wire name.
type variantFlat struct {
	flat   ir.Flattening
	byName map[string]ir.FlatProperty
}

// flattenVariants returns the flattening of each variant across its
// composition (ir.FlattenedProperties), its members keyed by wire name, in
// variant order.
func flattenVariants(reg ir.TypeRegistry, variants []ir.Variant) []variantFlat {
	flats := make([]variantFlat, len(variants))
	for i, v := range variants {
		flat := ir.FlattenedProperties(reg, v.Type.Target)
		byName := make(map[string]ir.FlatProperty, len(flat.Properties))
		for _, p := range flat.Properties {
			byName[p.Property.EffectiveWireName()] = p
		}
		flats[i] = variantFlat{flat: flat, byName: byName}
	}
	return flats
}

// pinnedTags returns the strings each variant admits for the property name, in
// variant order, read as tagReadings.object reads them, and false unless every
// one pins a set (tagSet.pins) and no two share a string: a mapping routes a
// tag to one variant. Where required is set, each variant must require the
// property too, as a tag no payload need carry names no variant.
func pinnedTags(reg ir.TypeRegistry, r *tagReadings, flats []variantFlat, name string, required bool) ([][]string, bool) {
	tags := make([][]string, 0, len(flats))
	seen := map[string]bool{}
	for _, v := range flats {
		p, declared := v.byName[name]
		if required && !p.Required {
			return nil, false
		}
		got := r.object(reg, v.flat, p, declared, name)
		if !got.pins() {
			return nil, false
		}
		for _, tag := range got.strings {
			if seen[tag] {
				return nil, false
			}
			seen[tag] = true
		}
		tags = append(tags, got.strings)
	}
	return tags, true
}

// tagMapping maps each string of each set to the variant admitting it, the
// sets being in variant order.
func tagMapping(tags [][]string, variants []ir.Variant) map[string]ir.TypeID {
	mapping := map[string]ir.TypeID{}
	for i, set := range tags {
		for _, tag := range set {
			mapping[tag] = variants[i].Type.Target
		}
	}
	return mapping
}

// disagreements names the first maxReportedTags variants the implicit mapping
// does not route by all they admit (routedByName), with a few of what each
// admits, and how many more there are.
func disagreements(tags [][]string, implicit []implicitRoute) string {
	var named []string
	var more int
	for i, set := range tags {
		if implicit[i].routesExactly(set) {
			continue
		}
		if len(named) == maxReportedTags {
			more++
			continue
		}
		route := "names none"
		if implicit[i].named {
			route = "routes by " + strconv.Quote(implicit[i].name)
		}
		named = append(named, fewQuoted(set)+" where it "+route)
	}
	if more > 0 {
		named = append(named, strconv.Itoa(more)+" more")
	}
	return strings.Join(named, ", ")
}

// fewQuoted quotes the first maxReportedTags of tags and says how many more
// there are.
func fewQuoted(tags []string) string {
	return quotedOf(tags[:min(len(tags), maxReportedTags)], len(tags))
}

// quotedOf quotes shown, the first of total strings, and says how many more
// there are, for a caller that builds only the ones it shows.
func quotedOf(shown []string, total int) string {
	named := make([]string, 0, len(shown))
	for _, s := range shown {
		named = append(named, strconv.Quote(s))
	}
	list := strings.Join(named, ", ")
	if more := total - len(shown); more > 0 {
		list += " and " + strconv.Itoa(more) + " more"
	}
	return list
}
