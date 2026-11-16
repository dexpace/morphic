// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// Reasons maps each reason a record's line may give to the issue saying why
// its finding stands, which the tests holding each record judge it by.
func Reasons() map[string]string {
	return map[string]string{
		"inferred-kind": "#1987: a schema writing properties, required or additionalProperties and no type " +
			"lowers to a model, whose kind excludes the instances of other kinds the keywords admit",
		"union-beside-allof": "#1986: a union beside an allOf whose conjuncts name no kind composes a model, " +
			"which excludes the instances of other kinds the schema admits",
	}
}
