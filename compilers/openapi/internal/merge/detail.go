// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// holding is which of two nodes says all the other does of one part of them:
// both where they say the same, neither where each says what the other lacks.
type holding struct{ a, b bool }

// bothNodesHoldPart is the holding of a part two nodes say alike.
var bothNodesHoldPart = holding{a: true, b: true}

// and is the holding of two parts together.
func (h holding) and(holdingValue holding) holding {
	return holding{a: h.a && holdingValue.a, b: h.b && holdingValue.b}
}

// holdingOf reads a verdict on two parts as a holding: the narrower says all
// the broader does, and two that meet each say what the other lacks. Any other
// verdict is no holding, since nothing builds a part saying both.
func holdingOf(rel relation) (holding, bool) {
	switch rel {
	case relSame:
		return bothNodesHoldPart, true
	case firstTypeIsNarrower:
		return holding{a: true}, true
	case secondTypeIsNarrower:
		return holding{b: true}, true
	case relMeet:
		return holding{}, true
	default:
		return holding{}, false
	}
}

// nullHolding is the holding of two references' null admission: the one
// admitting no null the other refuses says all the other does of null.
func nullHolding(a, b ir.TypeRef) holding {
	return holding{a: !a.Nullable || b.Nullable, b: !b.Nullable || a.Nullable}
}

// verdict judges two nodes of one kind by what they say of their values and
// what else they say. Two saying one set of values are the same type where they
// say the rest alike, and otherwise two types the source declared apart, which
// overlap. Otherwise the one saying all the other does, in both, is narrower,
// and two each saying what the other lacks meet in a node the fold builds,
// where one can hold all they say (adoptable), and overlap where none can.
func verdict(values, detail holding, adoptable bool) relation {
	if values.a && values.b {
		if detail.a && detail.b {
			return relSame
		}
		return relOverlap
	}
	holdingValue := values.and(detail)
	switch {
	case holdingValue.a:
		return firstTypeIsNarrower
	case holdingValue.b:
		return secondTypeIsNarrower
	case adoptable:
		return relMeet
	default:
		return relOverlap
	}
}

// detailOf judges what two nodes say beside their values: their encoding and
// their TypeCommon but where each was declared (siteless). A field one sets
// and the other leaves empty is said by the first alone, and one both set
// differently by neither. adoptable reports whether one node could say all
// both do: no field set differently, and no examples only one gives, since
// nothing shows the node built of the two admits them.
func (j typeJudge) detailOf(a, b extras) (holding, bool) {
	holdingValue, adoptable := fieldHolding(reflect.ValueOf(a.Encoding), reflect.ValueOf(b.Encoding))
	um, umOK := unmodeledHolding(a.Common.Unmodeled, b.Common.Unmodeled, j.sites.walk)
	ex, exOK := fieldHolding(reflect.ValueOf(a.Common.Examples), reflect.ValueOf(b.Common.Examples))
	holdingValue, adoptable = holdingValue.and(um).and(ex), adoptable && umOK && exOK && ex == bothNodesHoldPart
	ac, bc := siteless(a.Common), siteless(b.Common)
	ac.Unmodeled, bc.Unmodeled, ac.Examples, bc.Examples = nil, nil, nil, nil
	av, bv := reflect.ValueOf(ac), reflect.ValueOf(bc)
	for i := range av.NumField() {
		fh, ok := fieldHolding(av.Field(i), bv.Field(i))
		holdingValue, adoptable = holdingValue.and(fh), adoptable && ok
	}
	return holdingValue, adoptable
}

// fieldHolding is the holding of one field two nodes may set, and false where
// both set it differently.
func fieldHolding(a, b reflect.Value) (holding, bool) {
	switch {
	case reflect.DeepEqual(a.Interface(), b.Interface()):
		return bothNodesHoldPart, true
	case b.IsZero():
		return holding{a: true}, true
	case a.IsZero():
		return holding{b: true}, true
	default:
		return holding{}, false
	}
}

// unmodeledHolding is the holding of what two nodes keep under Unmodeled, key
// by key as fieldHolding reads a field, and false where both keep a key
// differently, as sameEntry reads two entries through walk.
func unmodeledHolding(a, b ir.Unmodeled, walk FieldDeclarationContext) (holding, bool) {
	holdingValue := bothNodesHoldPart
	for _, k := range slices.Sorted(maps.Keys(a)) {
		other, ok := b[k]
		if !ok {
			holdingValue.b = false
			continue
		}
		if !sameEntry(walk, a[k], other) {
			return holding{}, false
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			holdingValue.a = false
		}
	}
	return holdingValue, true
}

// adopt returns base holding, beside all it says, each field of others that
// base leaves empty, as a reference to each would read it: the first of others
// to set it gives it, and differ hears of each later one setting it otherwise,
// by its index and the name a document spells the field with. Base keeps its
// site and its examples, which nothing adopts (detailOf).
func adopt(base ir.TypeCommon, others []ir.TypeCommon, walk FieldDeclarationContext,
	differ func(k int, field string),
) ir.TypeCommon {
	said := func(typeCommon ir.TypeCommon) reflect.Value {
		typeCommon = siteless(typeCommon)
		typeCommon.Unmodeled, typeCommon.Examples = nil, nil
		return reflect.ValueOf(typeCommon)
	}
	own, out := said(base), reflect.ValueOf(&base).Elem()
	theirs := make([]reflect.Value, len(others))
	for k, typeCommon := range others {
		theirs[k] = said(typeCommon)
	}
	for i := range out.NumField() {
		if !own.Field(i).IsZero() {
			continue
		}
		set := false
		for k := range others {
			v := theirs[k].Field(i)
			switch {
			case v.IsZero():
			case !set:
				out.Field(i).Set(v)
				set = true
			case !reflect.DeepEqual(v.Interface(), out.Field(i).Interface()):
				differ(k, jsonName(out.Type().Field(i)))
			}
		}
	}
	base.Unmodeled = adoptUnmodeled(base.Unmodeled, others, walk, differ)
	return base
}

// jsonName is the name a document spells f with, its JSON member's.
func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name
}

// adoptUnmodeled returns own with each key of others' Unmodeled it lacks, the
// first to keep one giving it, its provenance with it. differ hears of a later
// one keeping the key otherwise. It writes to no map it was given.
func adoptUnmodeled(own ir.Unmodeled, others []ir.TypeCommon, walk FieldDeclarationContext,
	differ func(k int, field string),
) ir.Unmodeled {
	out := maps.Clone(own)
	taken := map[string]bool{}
	for k, typeCommon := range others {
		for _, key := range slices.Sorted(maps.Keys(typeCommon.Unmodeled)) {
			entry := typeCommon.Unmodeled[key]
			kept, ok := out[key]
			switch {
			case !ok:
				if out == nil {
					out = ir.Unmodeled{}
				}
				out[key], taken[key] = entry, true
			case taken[key] && !sameEntry(walk, kept, entry):
				differ(k, key)
			}
		}
	}
	return out
}

// copyOf returns a pointer to a copy of what pointer points at, or nil where it
// is nil. What a fold takes of a declaration's deprecation it copies, since a
// later stage writes each holder's own (replacement.Resolve, replacement.Infer).
func copyOf[T any](pointer *T) *T {
	if pointer == nil {
		return nil
	}
	return new(*pointer)
}
