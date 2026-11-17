// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// Reasons maps each reason a record's line may give to the issue saying why
// its finding stands, which the tests holding each record judge it by.
func Reasons() map[string]string {
	return map[string]string{
		"value-keyword": "#1855: a $ref a value set passes over is kept under openapi:$ref beside it and under " +
			"its entry's key beside an allOf of it",
		"decorated-object": "#1874: a redaction beside a $ref with no carrier hoists a Scalar over a model, where " +
			"the allOf spelling hoists a Model",
		"kept-key": "#1989: a keyword on an inline allOf branch kept whole loses its own key, reason and report",
		"entry-value-set": "#2017: an enum or const on an inline allOf entry builds its node at the entry's " +
			"pointer where beside the allOf it builds it at the position's",
		"entry-object-value": "#2019: an enum of objects or a const object on an inline allOf entry over an " +
			"object is kept verbatim on a model over it, where beside the allOf it lowers the value set and " +
			"keeps the allOf",
	}
}
