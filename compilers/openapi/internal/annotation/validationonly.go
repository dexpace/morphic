// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// IfThenElseRaw combines the present if/then/else arms into one raw JSON
// object, and names the arms it combines.
func IfThenElseRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	return combine(s, "if", "then", "else")
}

// ContainsRaw combines contains/minContains/maxContains into one raw JSON
// object, and names the keywords it combines.
func ContainsRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	if s.GetContains() == nil && s.GetMinContains() == nil && s.GetMaxContains() == nil {
		return nil, nil, nil
	}
	return combine(s, "contains", "minContains", "maxContains")
}

// ExamplesRaw combines example and examples into one raw JSON object, and names
// the keywords it combines: Set.Examples holds the two as one list.
func ExamplesRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	return combine(s, "example", "examples")
}

// ClosesAfterComposition reports whether s closes its model once composition
// is resolved: it writes `unevaluatedProperties: false` and no
// additionalProperties, which would evaluate every property the keyword could
// apply to (JSON Schema 2020-12 §11.3).
func ClosesAfterComposition(s *oas3.Schema) bool {
	return IsFalseSchema(s.GetUnevaluatedProperties()) && inertBeside(s, "unevaluatedProperties", Dialect{}) == ""
}

// UnevaluatedRaw combines the unevaluated keywords s writes into one raw JSON
// object, each beside the keyword written next to it that leaves it nothing to
// apply to (inertBeside), and names the unevaluated keywords it combines.
// closure says the node took `unevaluatedProperties: false` as its openness, so
// that form is left out; dialect says where s's dialect reads the elements past
// a prefix.
func UnevaluatedRaw(s *oas3.Schema, closure bool, dialect Dialect) (ir.RawValue, []string, error) {
	return unevaluatedRaw(s, unevaluatedKept(s, closure, dialect))
}

// unevaluatedRaw renders the entry holding kept and names its unevaluated
// keywords alone, which are what its provenance is asked about: the keyword
// beside an inert one is context the entry holds whoever wrote it.
func unevaluatedRaw(s *oas3.Schema, kept []keptUnevaluated) (ir.RawValue, []string, error) {
	if len(kept) == 0 {
		return nil, nil, nil
	}
	raw, _, err := combine(s, unevaluatedMembers(kept)...)
	names := make([]string, 0, len(kept))
	for _, k := range kept {
		names = append(names, k.keyword)
	}
	return raw, names, err
}

// keptUnevaluated is one unevaluated keyword an entry keeps, and the keyword
// written beside it that leaves it inert, "" for a live one.
type keptUnevaluated struct {
	keyword string
	inertBy string
}

// unevaluatedKept returns the unevaluated keywords s writes that an entry
// keeps, in keyword order, as UnevaluatedRaw reads closure and dialect. It asks
// the model first, so a schema writing neither, nearly every one, costs no
// read of its node.
func unevaluatedKept(s *oas3.Schema, closure bool, dialect Dialect) []keptUnevaluated {
	var out []keptUnevaluated
	keep := func(keyword string) {
		out = append(out, keptUnevaluated{keyword: keyword, inertBy: inertBeside(s, keyword, dialect)})
	}
	if up := s.GetUnevaluatedProperties(); Written(up) && (!closure || !IsFalseSchema(up)) {
		keep("unevaluatedProperties")
	}
	if Written(s.GetUnevaluatedItems()) {
		keep("unevaluatedItems")
	}
	return out
}

// unevaluatedMembers returns the members an entry holding kept writes, each
// inert keyword after the keyword that leaves it so, which a consumer applying
// the entry as a schema then reads as the source does.
func unevaluatedMembers(kept []keptUnevaluated) []string {
	out := make([]string, 0, 2*len(kept))
	for _, k := range kept {
		if k.inertBy != "" {
			out = append(out, k.inertBy)
		}
		out = append(out, k.keyword)
	}
	return out
}

// inertBeside returns the keyword s writes beside keyword, an unevaluated one,
// that evaluates every location keyword could apply to, and "" where s writes
// none. Beside unevaluatedProperties that is an additionalProperties of any
// value (JSON Schema 2020-12 §10.3.2.3). Beside unevaluatedItems it is an
// `items` schema (§10.3.1.2), or, where dialect reads an `items` list as a
// tuple, the additionalItems written beside it (2019-09 §9.3.1.2). An `items`
// list any other dialect reads is no schema, and evaluates nothing.
func inertBeside(s *oas3.Schema, keyword string, dialect Dialect) string {
	if keyword == "unevaluatedProperties" {
		if Written(s.GetAdditionalProperties()) {
			return "additionalProperties"
		}
		return ""
	}
	if items := RawPropertyNode(s, "items"); items != nil && items.Kind == yaml.SequenceNode {
		if dialect.ListedTuple && RawPropertyNode(s, "additionalItems") != nil {
			return "additionalItems"
		}
		return ""
	}
	if Written(s.GetItems()) {
		return "items"
	}
	return ""
}

// unevaluatedNote words the report of an unevaluated entry holding kept: as
// any validation-only entry's, but naming each inert keyword and the keyword
// that leaves it so, which the entry holds beside it.
func unevaluatedNote(kept []keptUnevaluated) string {
	var clauses []string
	for _, k := range kept {
		if k.inertBy == "" {
			continue
		}
		unit := "property"
		if k.keyword == "unevaluatedItems" {
			unit = "element"
		}
		clauses = append(clauses, fmt.Sprintf("%s beside %s applies to no %s, since %s evaluates each one it would",
			k.keyword, k.inertBy, unit, k.inertBy))
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

// combine renders the keys s writes, of those given, as one raw JSON object,
// and names the keys written; nothing written yields no object rather than an
// empty one. The entry built from it has no position of its own, so those keys
// are what its provenance is asked about (GitHub #534). They are named beside a
// conversion error as well, because the report that stands in for an entry that
// could not be kept is attributed as the entry would have been.
func combine(s *oas3.Schema, keys ...string) (ir.RawValue, []string, error) {
	members, written, err := presentMembers(s, keys...)
	if err != nil || len(members) == 0 {
		return nil, written, err
	}
	raw, err := jsonObject(members)
	return raw, written, err
}

// presentMembers collects the given keywords that are present on s as raw JSON
// members, preserving the requested order, and names the keywords present. A
// keyword written as `null` states nothing, so it is read as left out.
//
// A member that cannot be converted fails the whole combination rather than
// being skipped: the entry is presented as the verbatim source, so dropping a
// member would restate GitHub #144 in miniature. The error names the first such
// member, and the keywords present are returned in full even then, since the
// report of the failure is attributed by all of them.
func presentMembers(s *oas3.Schema, keys ...string) ([]rawMember, []string, error) {
	var members []rawMember
	var present []string
	var failed error
	for _, k := range keys {
		node := RawPropertyNode(s, k)
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
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := jsontext.AppendQuote(nil, m.key)
		if err != nil {
			return nil, fmt.Errorf("member %q: %w", m.key, err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return ir.RawValue(b.String()), nil
}

// validationOnlyAt collects the validation-only keywords
// (docs/ir/boundaries.md#validation-only-constructs) a schema declares that the
// IR does not model, keeping each verbatim and announcing it, `dependencies`
// among them where dialect defines it. own says s is the schema written at the
// position rather than a voice, whose unevaluated keywords stay with its branch
// (schema.foldEntry), and closure is UnevaluatedRaw's.
//
// Site-only: these constrain the value at the position that wrote them.
func validationOnlyAt(s *oas3.Schema, pointer jsontext.Pointer, doc provenance.Doc, dialect Dialect, own, closure bool) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic

	// A keyword kept at its own node goes through PreserveValidationOnlyKeyword.
	// The entries combining several keywords into one object — if/then/else,
	// contains, unevaluated — go through keep, because no single keyword names
	// those.
	keepKeyword := func(keyword string) {
		diags = append(diags, PreserveValidationOnlyKeyword(&p, s, keyword, pointer, doc)...)
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
		diags = append(diags, preserveValidationOnlyInto(&p, key, raw, entry, doc.At(pointer), note)...)
	}

	if Written(s.GetNot()) {
		keepKeyword("not")
	}
	ite, iteKeys, iteErr := IfThenElseRaw(s)
	if ite != nil || iteErr != nil {
		keep("openapi:if-then-else", ite, iteErr, validationOnlyMessage("if/then/else"), iteKeys...)
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		keepKeyword("dependentSchemas")
	}
	// dependentRequired has no oas3.Schema field to gate on
	// (docs/parser-library.md#unmodeled-keywords), which is why it was once
	// silently dropped where its sibling dependentSchemas was kept. Its raw node
	// is the only reading, and an absent one keeps nothing.
	keepKeyword("dependentRequired")
	if dialect.Dependencies {
		// The drafts before 2019-09 spell dependentSchemas and dependentRequired
		// as one keyword, kept as they are (docs/compilers/openapi.md#validation-only).
		keepKeyword("dependencies")
	}
	if Written(s.GetPropertyNames()) {
		keepKeyword("propertyNames")
	}
	craw, cKeys, cErr := ContainsRaw(s)
	if craw != nil || cErr != nil {
		keep("openapi:contains", craw, cErr, validationOnlyMessage("contains"), cKeys...)
	}
	if !own {
		return p, diags
	}
	kept := unevaluatedKept(s, closure, dialect)
	u, uKeys, uErr := unevaluatedRaw(s, kept)
	if u != nil || uErr != nil {
		keep("openapi:unevaluated", u, uErr, unevaluatedNote(kept), uKeys...)
	}
	return p, diags
}
