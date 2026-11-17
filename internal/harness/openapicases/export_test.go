// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import "github.com/dexpace/morphic/internal/harness"

// Record returns what case oracle o is known to find at the core cases, the
// lines the core tests hold their run to.
func Record(o harness.Oracle) (harness.Recorded, error) {
	core, _, err := records(o)
	if err != nil {
		return nil, err
	}
	return harness.ReadRecorded([]byte(core))
}

// Reasons maps each reason a record's line may give to the issue saying why
// its finding stands, which the tests holding each record judge it by.
func Reasons() map[string]string {
	return map[string]string{
		"dynamic-ref-beside": "#2009: a $dynamicRef an allOf entry writes over an object is read as a reference " +
			"of its own, where beside the $ref it is kept unexpanded on the model over the object",
		"ref-entry-sibling": "an allOf entry writing keywords beside its $ref reads that $ref a second time: beside " +
			"an enum or const it is reported twice, and an entry kept whole keeps the $ref with its keywords, " +
			"where the entry spelling keeps the keywords alone",
		"two-unions-on-entry": "#2011: a oneOf and an anyOf on one schema are both kept beside its body, where " +
			"written on an allOf entry the oneOf is distributed",
		"entry-missing-items": "#2048: a 3.0 type: array on an allOf entry writing no items is warned of, where " +
			"beside the allOf the conjunction's items answers it",
		"unfiled-entry-type-list": "no issue yet: a type list on an inline allOf entry over an object composition " +
			"is kept verbatim, where beside the allOf it is read (the cause #2039 names, at a keyword it leaves out)",
		"unfiled-closure-beside-union": "no issue yet: a closure keyword beside a oneOf or anyOf on one inline " +
			"allOf entry keeps the entry whole, where beside the allOf it closes each variant",
		"unfiled-entry-residue-per-variant": "no issue yet: a keyword kept verbatim beside a union on an inline " +
			"allOf entry is kept on each variant the union distributes into, where beside the allOf it is kept " +
			"once on the Union",
		"unfiled-entry-union-dropped": "no issue yet: a oneOf or anyOf that an enum or const beside it on an " +
			"inline allOf entry leaves nothing to say is dropped with no record and no report, where beside the " +
			"allOf it is kept verbatim with an info",
		"unfiled-disjoint-value-keywords": "no issue yet: a value keyword contradicting another, or the union " +
			"beside it, lowers to another node in each spelling, keeping the reference under the keyword that " +
			"spelling wrote it with",
	}
}
