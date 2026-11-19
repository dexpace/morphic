// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// kindSet is a set of JSON instance kinds. An integer and a number with a
// fraction are two kinds, since `integer` admits one of them and `number` both.
type kindSet uint8

const (
	kindString kindSet = 1 << iota
	kindInteger
	kindFraction
	kindBoolean
	kindArray
	kindObject
	kindNull

	kindNumber = kindInteger | kindFraction
	kindAny    = kindString | kindNumber | kindBoolean | kindArray | kindObject | kindNull
)

// kindOfType returns the kinds a `type` member admits, and none for a spelling
// no schema defines.
func kindOfType(jsonSchemaTypeName statement.JSONSchemaTypeName) kindSet {
	switch jsonSchemaTypeName {
	case statement.KindString:
		return kindString
	case statement.KindInteger:
		return kindInteger
	case statement.KindNumber:
		return kindNumber
	case statement.KindBoolean:
		return kindBoolean
	case statement.KindArray:
		return kindArray
	case statement.KindObject:
		return kindObject
	case statement.KindNull:
		return kindNull
	default:
		return 0
	}
}

// kindOfValue returns the JSON kind of value.
func kindOfValue(value ir.Value) kindSet {
	switch value.Kind {
	case ir.ValueString, ir.ValueSymbol, ir.ValueBytes:
		return kindString
	case ir.ValueBool:
		return kindBoolean
	case ir.ValueNull:
		return kindNull
	case ir.ValueList:
		return kindArray
	case ir.ValueObject:
		return kindObject
	case ir.ValueNumber:
		return kindOfNumber(string(value.Number))
	case ir.ValueRefKind, ir.ValueConstructor, ir.ValueInfinity, ir.ValueNegativeInfinity, ir.ValueNaN:
		// A reference, a constructor and a non-finite number are no JSON
		// instance's value.
		return 0
	default:
		return 0
	}
}

// kindOfNumber returns the kind of a decimal literal. One with an exponent may
// be integral or not, and telling them apart costs what the exponent says, so it
// answers for both.
func kindOfNumber(num string) kindSet {
	if strings.ContainsAny(num, "eE") {
		return kindNumber
	}
	if _, fraction, _ := strings.Cut(num, "."); strings.Trim(fraction, "0") != "" {
		return kindFraction
	}
	return kindInteger
}

// anyInstance is what a schema writing no `type` admits, null aside: nulls is
// the one reading of null, so a set here holds no kindNull, and its zero is a
// schema writing `type: "null"`.
const anyInstance = kindAny &^ kindNull

// kindWords spells the members of a kindSet in the order a diagnostic lists
// them. A number holds an integer and a number with a fraction, so it is
// spelled before either: each is named only where the other is not left
// beside it, the second as no type names it.
var kindWords = []struct {
	kinds kindSet
	word  string
}{
	{kindObject, string(statement.KindObject)},
	{kindArray, string(statement.KindArray)},
	{kindString, string(statement.KindString)},
	{kindNumber, string(statement.KindNumber)},
	{kindInteger, string(statement.KindInteger)},
	{kindFraction, "number that is no integer"},
	{kindBoolean, string(statement.KindBoolean)},
}

// instanceWords spells kindSetValue, a set of instance types, for a diagnostic.
func instanceWords(kindSetValue kindSet) string {
	if kindSetValue == 0 {
		return string(statement.KindNull)
	}
	out := make([]string, 0, len(kindWords))
	for _, w := range kindWords {
		if kindSetValue&w.kinds == w.kinds {
			out = append(out, w.word)
			kindSetValue &^= w.kinds
		}
	}
	return strings.Join(out, " or ")
}

// declaredInstanceTypes is the set schema's own `type` keyword states. A schema
// writing none excludes nothing, whatever else it writes: `properties` alone
// constrains objects and admits a string. A word no dialect defines says
// nothing either: load reports it, so it is no conjunct's to contradict another.
func declaredInstanceTypes(schema *statement.Schema) kindSet {
	declared := schema.Types()
	if len(declared) == 0 {
		return anyInstance
	}
	var out kindSet
	for _, st := range declared {
		kinds := kindOfType(st)
		if kinds == 0 {
			kinds = anyInstance
		}
		out |= kinds &^ kindNull
	}
	return out
}

// typeKinds returns the kinds st, one type a type array lists, admits, and every
// kind but null for a spelling no dialect defines, as declaredInstanceTypes
// reads one.
func typeKinds(st statement.JSONSchemaTypeName) kindSet {
	if kinds := kindOfType(st) &^ kindNull; kinds != 0 {
		return kinds
	}
	return anyInstance
}
