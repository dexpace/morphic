// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// Reasons maps each reason a record's line may give to the issue saying why
// its finding stands, which the tests holding each record judge it by.
func Reasons() map[string]string {
	return map[string]string{
		"dynamic-ref-beside": "#2009: a $dynamicRef an allOf entry writes over an object is read as a reference " +
			"of its own, where beside the $ref it is kept unexpanded on the model over the object",
	}
}
