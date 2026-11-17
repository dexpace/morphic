// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// Reasons maps each reason a record's line may give to the issue saying why
// its finding stands, which the tests holding each record judge it by.
func Reasons() map[string]string {
	return map[string]string{
		"structural-keyword": "#1067: a structural keyword beside a 3.1 $ref is kept verbatim, not composed",
		"value-keyword": "#1855: a value keyword beside a $ref is kept verbatim where beside an allOf of that $ref " +
			"it refines",
		"decorated-object": "#1874: a redaction beside a $ref with no carrier hoists a Scalar over a model, where " +
			"the allOf spelling hoists a Model",
		"nested-title": "#1997: a title on an allOf entry nested in another names no union variant",
		"carrier-or-model": "#1999: a use of an object keeps what it writes on its carrier beside a $ref and on a " +
			"model in the allOf spelling",
		"hint-by-spelling": "#2000: a node hoisted for a use of a named schema is named for its target beside a " +
			"$ref and for its position in the allOf spelling",
		"dynamic-ref-beside": "#2009: a $dynamicRef beside a $ref is dropped where the position has no carrier, " +
			"and beside an allOf of it keeps a model where the target is no object",
		"union-beside-ref": "#2010: a union beside a $ref is kept verbatim where beside an allOf of that $ref it is " +
			"consumed, distributed or read as validation logic",
	}
}
