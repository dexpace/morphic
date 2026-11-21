// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// IfThenElseRaw combines the present if/then/else arms into one raw JSON
// object, and names the arms it combines.
func IfThenElseRaw(schema *statement.Schema) (ir.RawValue, []string, error) {
	return combine(schema, "if", "then", "else")
}

// ContainsRaw combines contains/minContains/maxContains into one raw JSON
// object, and names the keywords it combines.
func ContainsRaw(schema *statement.Schema) (ir.RawValue, []string, error) {
	if schema == nil || schema.Array == nil {
		return nil, nil, nil
	}
	if a := schema.Array; a.Contains == (statement.Subschema{}) && a.MinContains == nil && a.MaxContains == nil {
		return nil, nil, nil
	}
	return combine(schema, "contains", "minContains", "maxContains")
}

// ExamplesRaw combines example and examples into one raw JSON object, and names
// the keywords it combines: Set.Examples holds the two as one list.
func ExamplesRaw(schema *statement.Schema) (ir.RawValue, []string, error) {
	return combine(schema, "example", "examples")
}

// ClosesAfterComposition reports whether schema closes its model once
// composition is resolved: it writes `unevaluatedProperties: false` and no
// additionalProperties, which would evaluate every property the keyword could
// apply to (JSON Schema 2020-12 §11.3). It reads the adjacent keyword alone, so
// an applicator kept verbatim that evaluates a member leaves the closure typed
// (GitHub #1904).
func ClosesAfterComposition(schema *statement.Schema) bool {
	return schema.ObjectKeywords().UnevaluatedProperties.SchemaValueForm().ReadsFalse() && inertBeside(schema, "unevaluatedProperties", Dialect{}) == ""
}

// unevaluatedRaw renders the entry holding kept, each inert keyword beside the
// keyword written next to it that leaves it nothing to apply to (inertBeside),
// and names its unevaluated keywords alone, which are what its provenance is
// asked about: the keyword beside an inert one is context the entry holds
// whoever wrote it.
func unevaluatedRaw(schema *statement.Schema, kept []keptUnevaluated) (ir.RawValue, []string, error) {
	if len(kept) == 0 {
		return nil, nil, nil
	}
	raw, _, err := combine(schema, unevaluatedMembers(kept)...)
	names := make([]string, 0, len(kept))
	for _, keptUnevaluated := range kept {
		names = append(names, keptUnevaluated.keyword)
	}
	return raw, names, err
}

// keptUnevaluated is one unevaluated keyword an entry keeps, and the keyword
// written beside it that leaves it inert, "" for a live one.
type keptUnevaluated struct {
	keyword string
	inertBy string
}

// unevaluatedKept returns the unevaluated keywords schema writes that an entry
// keeps, in keyword order. closure says the node took
// `unevaluatedProperties: false` as its openness, so that form is left out, and
// dialect says where schema's dialect reads the elements past a prefix. It asks
// the typed values first, so a schema writing neither, nearly every one, costs
// no read of its node.
func unevaluatedKept(schema *statement.Schema, closure bool, dialect Dialect) []keptUnevaluated {
	var out []keptUnevaluated
	keep := func(keyword string) {
		out = append(out, keptUnevaluated{keyword: keyword, inertBy: inertBeside(schema, keyword, dialect)})
	}
	if up := schema.ObjectKeywords().UnevaluatedProperties.SchemaValueForm(); up != statement.Unwritten && (!closure || !up.ReadsFalse()) {
		keep("unevaluatedProperties")
	}
	if schema.ArrayKeywords().UnevaluatedItems.SchemaValueForm() != statement.Unwritten {
		keep("unevaluatedItems")
	}
	return out
}

// unevaluatedMembers returns the members an entry holding kept writes, each
// inert keyword after the keyword that leaves it so, which a consumer reading
// the entry beside what its position types then reads as the source does.
func unevaluatedMembers(kept []keptUnevaluated) []string {
	out := make([]string, 0, 2*len(kept))
	for _, keptUnevaluated := range kept {
		if keptUnevaluated.inertBy != "" {
			out = append(out, keptUnevaluated.inertBy)
		}
		out = append(out, keptUnevaluated.keyword)
	}
	return out
}

// inertBeside returns the keyword schema writes beside keyword, an unevaluated
// one, that evaluates every location keyword could apply to, and "" where
// schema writes none. Beside unevaluatedProperties that is an
// additionalProperties of any value (JSON Schema 2020-12 §10.3.2.3). Beside
// unevaluatedItems it is an `items` schema (§10.3.1.2), or, where dialect reads
// an `items` list as a tuple, the additionalItems written beside it (2019-09
// §9.3.1.2). An `items` list any other dialect reads is no schema, and
// evaluates nothing.
func inertBeside(schema *statement.Schema, keyword string, dialect Dialect) string {
	if keyword == "unevaluatedProperties" {
		if schema.ObjectKeywords().AdditionalProperties.SchemaValueForm() != statement.Unwritten {
			return "additionalProperties"
		}
		return ""
	}
	if items := keyNode(schema, "items"); items != nil && items.Kind == yaml.SequenceNode {
		if dialect.ListedTuple && keyNode(schema, "additionalItems") != nil {
			return "additionalItems"
		}
		return ""
	}
	if schema.ArrayKeywords().Items.SchemaValueForm() != statement.Unwritten {
		return "items"
	}
	return ""
}

// unevaluatedNote words the report of an unevaluated entry holding kept: as
// any validation-only entry's, but naming each inert keyword and the keyword
// that leaves it so, which the entry holds beside it.
func unevaluatedNote(kept []keptUnevaluated) string {
	var clauses []string
	for _, keptUnevaluated := range kept {
		if keptUnevaluated.inertBy == "" {
			continue
		}
		unit := "property"
		if keptUnevaluated.keyword == "unevaluatedItems" {
			unit = "element"
		}
		clauses = append(clauses, fmt.Sprintf("%s beside %s applies to no %s, since %s evaluates each one it would",
			keptUnevaluated.keyword, keptUnevaluated.inertBy, unit, keptUnevaluated.inertBy))
	}
	switch {
	case len(clauses) == 0:
		return validationOnlyMessage("unevaluated")
	case len(kept) == 1:
		return clauses[0] + "; both kept verbatim under Unmodeled"
	default:
		return strings.Join(clauses, "; ") + "; " + validationOnlyMessage("unevaluated")
	}
}

// combine renders the keys schema writes, of those given, as one raw JSON
// object, and names the keys written; nothing written yields no object rather
// than an empty one. The entry built from it has no position of its own, so
// those keys are what its provenance is asked about (GitHub #534). They are
// named beside a conversion error as well, because the report that stands in
// for an entry that could not be kept is attributed as the entry would have
// been.
func combine(schema *statement.Schema, keys ...string) (ir.RawValue, []string, error) {
	members, written, err := presentMembers(schema, keys...)
	if err != nil || len(members) == 0 {
		return nil, written, err
	}
	raw, err := jsonObject(members)
	return raw, written, err
}

// presentMembers collects the given keywords that are present on schema as raw
// JSON members, preserving the requested order, and names the keywords present.
// A keyword written as `null` states nothing, so it is read as left out.
//
// A member that cannot be converted fails the whole combination rather than
// being skipped: the entry is presented as the verbatim source, so dropping a
// member would restate GitHub #144 in miniature. The error names the first such
// member, and the keywords present are returned in full even then, since the
// report of the failure is attributed by all of them.
func presentMembers(schema *statement.Schema, keys ...string) ([]rawMember, []string, error) {
	var members []rawMember
	var present []string
	var failed error
	for _, k := range keys {
		node := keyNode(schema, k)
		if node == nil {
			continue
		}
		present = append(present, k)
		raw, err := RawFromNode(node)
		switch {
		case err == nil:
			members = append(members, rawMember{key: k, val: raw})
		case failed == nil:
			failed = fmt.Errorf("%s: %w", k, err)
		}
	}
	if failed != nil {
		return nil, present, failed
	}
	return members, present, nil
}

// rawMember is one key/raw-JSON pair of a combined validation-only object.
type rawMember struct {
	key string
	val ir.RawValue
}

// jsonObject renders ordered raw members into a JSON object; combine is what
// keeps an empty set from reaching it. Unlike rawConv.mapping, member order here
// is the caller's — a fixed handful of JSON Schema keywords in keyword order —
// and is preserved rather than sorted.
func jsonObject(members []rawMember) (ir.RawValue, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, rawMember := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := jsontext.AppendQuote(nil, rawMember.key)
		if err != nil {
			return nil, fmt.Errorf("member %q: %w", rawMember.key, err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(rawMember.val)
	}
	b.WriteByte('}')
	return ir.RawValue(b.String()), nil
}

// validationOnlyAt collects the validation-only keywords
// (docs/ir/boundaries.md#validation-only-constructs) a schema declares that the
// IR does not model, keeping each verbatim and announcing it, `dependencies`
// among them where dialect defines it. own says s is the schema written at the
// position rather than a voice, whose unevaluated keywords stay with its entry
// (schema.foldEntry), and closure is unevaluatedKept's.
//
// Site-only: these constrain the value at the position that wrote them.
func validationOnlyAt(schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc, dialect Dialect, own, closure bool) (ir.Unmodeled, []ir.Diagnostic) {
	var unmodeled ir.Unmodeled
	var diags []ir.Diagnostic

	// A keyword kept at its own node goes through PreserveValidationOnlyKeyword.
	// The entries combining several keywords into one object — if/then/else,
	// contains, unevaluated — go through keep, because no single keyword names
	// those.
	keepKeyword := func(keyword string) {
		diags = append(diags, PreserveValidationOnlyKeyword(&unmodeled, schema, keyword, pointer, doc)...)
	}
	// keep takes the conversion's error alongside its payload so an entry that
	// could not be converted is reported rather than passed on as an absent one —
	// the two were indistinguishable here before GitHub #144.
	//
	// The entry is located at the schema and attributed by combined, the keywords
	// it folds together, and so is the report that stands in for it when it
	// cannot be kept. note is what the report of one kept says.
	keep := func(key string, raw ir.RawValue, err error, note string, combined ...string) {
		from := make([]jsontext.Pointer, 0, len(combined))
		for _, keyword := range combined {
			from = append(from, pointer+ids.Ptr(keyword))
		}
		entry := doc.At(pointer, from...)
		if err != nil {
			diags = append(diags, unpreservableDiag(key, entry, err))
			return
		}
		diags = append(diags, preserveValidationOnlyInto(&unmodeled, key, raw, entry, doc.At(pointer), note)...)
	}

	if schema.LogicKeywords().Not.SchemaValueForm() != statement.Unwritten {
		keepKeyword("not")
	}
	ite, iteKeys, iteErr := IfThenElseRaw(schema)
	if ite != nil || iteErr != nil {
		keep("openapi:if-then-else", ite, iteErr, validationOnlyMessage("if/then/else"), iteKeys...)
	}
	// An empty dependency map is the keyword left out, and keeps nothing
	// (docs/compilers/openapi.md#validation-only).
	if schema != nil && schema.Logic != nil && len(schema.Logic.DependentSchemas) > 0 {
		keepKeyword("dependentSchemas")
	}
	// dependentRequired has no oas3.Schema field to gate on
	// (docs/parser-library.md#unmodeled-keywords), which is why it was once
	// silently dropped where its sibling dependentSchemas was kept. Its raw node
	// is the only reading, and an absent one keeps nothing.
	if !WritesEmptyDependencyMap(schema, "dependentRequired") {
		keepKeyword("dependentRequired")
	}
	if dialect.Dependencies && !WritesEmptyDependencyMap(schema, "dependencies") {
		// The drafts before 2019-09 spell dependentSchemas and dependentRequired
		// as one keyword, kept as they are (docs/compilers/openapi.md#validation-only).
		keepKeyword("dependencies")
	}
	if schema.ObjectKeywords().PropertyNames.SchemaValueForm() != statement.Unwritten {
		keepKeyword("propertyNames")
	}
	craw, cKeys, cErr := ContainsRaw(schema)
	if craw != nil || cErr != nil {
		keep("openapi:contains", craw, cErr, validationOnlyMessage("contains"), cKeys...)
	}
	if !own {
		return unmodeled, diags
	}
	kept := unevaluatedKept(schema, closure, dialect)
	u, uKeys, uErr := unevaluatedRaw(schema, kept)
	if u != nil || uErr != nil {
		keep("openapi:unevaluated", u, uErr, unevaluatedNote(kept), uKeys...)
	}
	return unmodeled, diags
}
