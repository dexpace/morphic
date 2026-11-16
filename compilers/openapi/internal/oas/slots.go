// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// Slot is what a Schema Object keyword speaks of, such as a reference, the
// conjunction, one kind of value, the notes or the resource. The slots follow
// JSON Schema 2020-12's own grouping of its keywords: Validation §6.1 to §6.5
// by the kind each asserts about, Core §10.2's in-place applicators, and the
// annotations of Validation §7 to §9. SlotUnknown, the zero value, is a key
// the rows do not define, an extension among them.
type Slot uint8

// The slots a keyword speaks of.
const (
	SlotUnknown Slot = iota
	SlotReference
	SlotConjunction
	SlotKinds
	SlotValues
	SlotFormat
	SlotNumber
	SlotText
	SlotArray
	SlotObject
	SlotChoice
	SlotLogic
	SlotNotes
	SlotResource
)

// Slot returns the slot of the keyword name in the rows, and whether they
// define it only later: a keyword the dialect the compiler reads natively
// defines and the rows do not (Later), which is read as that dialect reads
// it. A key the rows neither define nor define later, and every key of the
// zero rows, is SlotUnknown.
func (r SchemaRules) Slot(name string) (slot Slot, later bool) {
	if r.rows == nil {
		return SlotUnknown, false
	}
	if _, defined := r.rows.fields[name]; defined {
		return SlotOf(name), false
	}
	if _, ok := r.rows.later[name]; ok {
		return SlotOf(name), true
	}
	return SlotUnknown, false
}

// SlotOf returns the slot of the keyword name in whichever dialect defines it,
// SlotUnknown for a name none does. A draft's spelling shares its twin's slot
// (`id` with `$id`, `definitions` with `$defs`), so whether the dialect in
// force defines a key is the rows' to say, which SchemaRules.Slot answers.
func SlotOf(name string) Slot {
	switch name {
	case "$ref", "$dynamicRef", "$recursiveRef":
		return SlotReference
	case "allOf":
		return SlotConjunction
	case "type", "nullable":
		return SlotKinds
	case "enum", "const":
		return SlotValues
	case "format":
		return SlotFormat
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
		return SlotNumber
	case "minLength", "maxLength", "pattern", "contentEncoding", "contentMediaType", "contentSchema":
		return SlotText
	case "items", "prefixItems", "additionalItems", "contains", "minContains", "maxContains", "minItems",
		"maxItems", "uniqueItems", "unevaluatedItems":
		return SlotArray
	case "properties", "patternProperties", "additionalProperties", "propertyNames", "required",
		"dependentRequired", "minProperties", "maxProperties", "discriminator", "unevaluatedProperties":
		return SlotObject
	case "oneOf", "anyOf":
		return SlotChoice
	case "not", "if", "then", "else", "dependentSchemas", "dependencies":
		return SlotLogic
	case "title", "description", "default", "example", "examples", "deprecated", "readOnly", "writeOnly",
		"xml", "externalDocs", "$comment":
		return SlotNotes
	case "$id", "id", "$schema", "$vocabulary", "$anchor", "$dynamicAnchor", "$recursiveAnchor", "$defs",
		"definitions":
		return SlotResource
	default:
		return SlotUnknown
	}
}
