// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
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

// UnevaluatedRaw combines a non-false unevaluatedProperties and any
// unevaluatedItems into one raw JSON object (a false unevaluatedProperties is a
// structural mode, handled in fillAdditional), and names the keywords it
// combines.
func UnevaluatedRaw(s *oas3.Schema) (ir.RawValue, []string, error) {
	var want []string
	if up := s.GetUnevaluatedProperties(); up != nil && !IsFalseSchema(up) {
		want = append(want, "unevaluatedProperties")
	}
	if s.GetUnevaluatedItems() != nil {
		want = append(want, "unevaluatedItems")
	}
	return combine(s, want...)
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
		if node == nil || IsNullNode(node) {
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

// validationOnlyAt collects the §4.7 keywords a schema declares that the IR does
// not model, keeping each verbatim and announcing it.
//
// Site-only: these constrain the value at the position that wrote them.
func validationOnlyAt(s *oas3.Schema, pointer jsontext.Pointer, locate Locator) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic

	// A keyword kept at its own node goes through PreserveValidationOnlyKeyword.
	// The entries combining several keywords into one object — if/then/else,
	// contains, unevaluated — go through keep, because no single keyword names
	// those.
	keepKeyword := func(keyword string) {
		diags = append(diags, PreserveValidationOnlyKeyword(&p, s, keyword, pointer, locate)...)
	}
	// keep takes the conversion's error alongside its payload so an entry that
	// could not be converted is reported rather than passed on as an absent one —
	// the two were indistinguishable here before GitHub #144.
	//
	// The entry is located at the schema and attributed by combined, the keywords
	// it folds together, and so is the report that stands in for it when it
	// cannot be kept.
	keep := func(key string, raw ir.RawValue, err error, label string, combined ...string) {
		from := make([]jsontext.Pointer, 0, len(combined))
		for _, keyword := range combined {
			from = append(from, pointer+ids.Ptr(keyword))
		}
		entry := locate(pointer, from...)
		if err != nil {
			diags = append(diags, unpreservableDiag(key, entry, err))
			return
		}
		diags = append(diags, preserveValidationOnlyInto(&p, key, raw, entry, locate(pointer), label)...)
	}

	if Written(s.GetNot()) {
		keepKeyword("not")
	}
	ite, iteKeys, iteErr := IfThenElseRaw(s)
	if ite != nil || iteErr != nil {
		keep("openapi:if-then-else", ite, iteErr, "if/then/else", iteKeys...)
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		keepKeyword("dependentSchemas")
	}
	// dependentRequired has no oas3.Schema field to gate on
	// (docs/parser-library.md#unmodeled-keywords), which is why it was once
	// silently dropped where its sibling dependentSchemas was kept. Its raw node
	// is the only reading, and an absent one keeps nothing.
	keepKeyword("dependentRequired")
	if Written(s.GetPropertyNames()) {
		keepKeyword("propertyNames")
	}
	craw, cKeys, cErr := ContainsRaw(s)
	if craw != nil || cErr != nil {
		keep("openapi:contains", craw, cErr, "contains", cKeys...)
	}
	u, uKeys, uErr := UnevaluatedRaw(s)
	if u != nil || uErr != nil {
		keep("openapi:unevaluated", u, uErr, "unevaluated", uKeys...)
	}
	return p, diags
}
