// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/ir"
)

// agreement is what the conjuncts of one allOf write for the position holding
// it, entry by entry: the first writer's rendering of each, the entries two
// writers render apart, and what the writers of each value bound say of it
// together. It is a set over the conjuncts, so no order decides what agrees.
type agreement struct {
	c      lowering.Ctx
	first  map[string]foldRendering
	split  map[string]bool
	bounds map[string]*boundReading
}

// newAgreement returns an agreement no conjunct has written to, reading value
// bounds in the dialect c's document declares.
func newAgreement(c lowering.Ctx) *agreement {
	return &agreement{c: c, first: map[string]foldRendering{}, split: map[string]bool{},
		bounds: map[string]*boundReading{}}
}

// write records what conjunct writes for the position and returns its entries,
// rendering each once: the first writer's is compared with every later one's.
func (a *agreement) write(conjunct *oas3.Schema) []string {
	var entries []string
	var cons *ir.Constraints
	read := false
	for _, keyword := range rawMappingKeys(conjunct.GetRootNode()) {
		entry, fold := foldEntry(conjunct, keyword)
		if !fold || annotation.WritesKeywordLeftOut(conjunct, keyword) {
			continue
		}
		entries = append(entries, entry)
		rendered := foldValue(conjunct, entry)
		if held, ok := a.first[entry]; !ok {
			a.first[entry] = rendered
		} else if !held.agrees(rendered) {
			a.split[entry] = true
		}
		if !slices.Contains(valueConstraintKeywords, entry) {
			continue
		}
		if !read {
			// What it reports and keeps, the lowering reports and keeps where it reads it.
			cons, _, _ = annotation.Constraints(conjunct, a.c.Is30(), "", unlocated)
			read = true
		}
		a.bound(entry).add(boundOf(entry, cons))
	}
	return entries
}

// unlocated is the annotation.Locator of a reading whose findings go nowhere.
func unlocated(jsontext.Pointer, ...jsontext.Pointer) ir.Provenance { return ir.Provenance{} }

// bound returns the reading of the value bound entry, starting one.
func (a *agreement) bound(entry string) *boundReading {
	b, ok := a.bounds[entry]
	if !ok {
		b = &boundReading{}
		a.bounds[entry] = b
	}
	return b
}

// apart reports whether the writers of entry disagree, so none of them folds: a
// value bound agrees where every writer states a value and the values meet, as a
// redeclared field's bounds do (merge.Constraints), and any entry agrees where
// every writer renders it alike.
func (a *agreement) apart(entry string) bool {
	if b, ok := a.bounds[entry]; ok && b.meets() {
		return false
	}
	return a.split[entry]
}

// boundReading is what the writers of one value bound say of it together.
type boundReading struct {
	// met is what they say together, nil before one states a value.
	met *ir.Constraints
	// unstated marks a writer whose keyword fills no field, a 3.0 exclusive
	// modifier with no bound beside it or a value the reader refused.
	unstated bool
	// disjoint marks two values no one value holds (merge.Meets).
	disjoint bool
}

// add records one writer's reading of the bound.
func (b *boundReading) add(bound ir.Constraints) {
	switch {
	case bound == ir.Constraints{}:
		b.unstated = true
	case b.met == nil:
		b.met = &bound
	case !merge.Meets(b.met, &bound):
		b.disjoint = true
	default:
		b.met = merge.Constraints(b.met, &bound)
	}
}

// meets reports whether every writer states a value and the values meet. Whether
// two meet is a set's answer: an ordered bound always meets another, and one with
// no order, or a literal no decimal holds, meets only one written alike.
func (b *boundReading) meets() bool { return !b.unstated && !b.disjoint }

// boundOf returns the fields of cons the value bound keyword entry fills. A 3.0
// exclusive modifier moves the bound beside it, so a minimum and its exclusive
// form share a pair of fields, and the maximum's likewise.
func boundOf(entry string, cons *ir.Constraints) ir.Constraints {
	if cons == nil {
		return ir.Constraints{}
	}
	switch entry {
	case "minimum", "exclusiveMinimum":
		return ir.Constraints{Min: cons.Min, ExclusiveMin: cons.ExclusiveMin}
	case "maximum", "exclusiveMaximum":
		return ir.Constraints{Max: cons.Max, ExclusiveMax: cons.ExclusiveMax}
	case "multipleOf":
		return ir.Constraints{MultipleOf: cons.MultipleOf}
	case "minLength":
		return ir.Constraints{MinLength: cons.MinLength}
	case "maxLength":
		return ir.Constraints{MaxLength: cons.MaxLength}
	case "minProperties":
		return ir.Constraints{MinProps: cons.MinProps}
	case "maxProperties":
		return ir.Constraints{MaxProps: cons.MaxProps}
	case "pattern":
		return ir.Constraints{Pattern: cons.Pattern}
	default:
		return ir.Constraints{}
	}
}
