// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import "github.com/dexpace/morphic/internal/harness"

// Record returns what case oracle oracle is known to find at the core cases,
// the lines the core tests hold their run to.
func Record(oracle harness.Oracle) (harness.Recorded, error) {
	core, _, err := records(oracle)
	if err != nil {
		return nil, err
	}
	return harness.ReadRecorded([]byte(core))
}

// Reasons maps each reason a record's line may give to the issue saying why
// its finding stands, which the tests holding each record judge it by.
func Reasons() map[string]string {
	return map[string]string{
		"entry-missing-items": "#2048: a 3.0 type: array on an allOf entry writing no items is warned of, where " +
			"beside the allOf the conjunction's items answers it",
		"entry-type-list": "#2039: a type list on an inline allOf entry over an object composition is kept " +
			"verbatim, where beside the allOf it is read",
		"closure-beside-union": "#2047: an additionalProperties or patternProperties beside a oneOf or anyOf on " +
			"one inline allOf entry keeps the entry whole, where beside the allOf each variant takes it",
		"residue-per-variant": "#2054: a keyword kept verbatim beside a union on an inline allOf entry is " +
			"copied onto each variant the union distributes into, where beside the allOf it is kept once on " +
			"the Union",
	}
}
