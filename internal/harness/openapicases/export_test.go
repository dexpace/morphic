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
		"union-beside-allof": "#1986: a union beside an allOf whose conjuncts name no kind composes a model, " +
			"which excludes the instances of other kinds the schema admits",
		"kept-key":     "#1989: a keyword on an inline allOf branch kept whole loses its own key, reason and report",
		"nested-title": "#1997: a title on an allOf entry nested in another names no union variant",
		"entry-closure": "#1998: a closure keyword on an inline allOf entry is kept where beside the allOf it " +
			"closes the model",
		"carrier-or-model": "#1999: a use of an object keeps what it writes on its carrier beside a $ref and on a " +
			"model in the allOf spelling",
		"hint-by-spelling": "#2000: a node hoisted for a use of a named schema is named for its target beside a " +
			"$ref and for its position in the allOf spelling",
		"any-kind-model": "#2001: an allOf over a schema admitting every kind lowers to a Model, whose kind " +
			"excludes the instances of other kinds the schema admits",
		"dynamic-ref-beside": "#2009: a $dynamicRef beside a $ref is dropped where the position has no carrier, " +
			"and beside an allOf of it keeps a model over a target that is no object",
		"dynamic-ref-read": "#2015: an allOf over a schema naming a kind lowers to a Model when the schema holds " +
			"a $dynamicRef it does not expand",
		"union-beside-ref": "#2010: a union beside a $ref is kept verbatim where beside an allOf of that $ref it is " +
			"consumed, distributed or read as validation logic",
		"entry-value-set": "#2017: an enum or const on an inline allOf entry builds its node at the entry's " +
			"pointer where beside the allOf it builds it at the position's",
		"entry-object-value": "#2019: an enum of objects or a const object on an inline allOf entry over an " +
			"object is kept verbatim on a model over it, where beside the allOf it lowers the value set and " +
			"keeps the allOf",
	}
}
