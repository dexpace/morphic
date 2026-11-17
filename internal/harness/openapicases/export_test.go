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
		"ref-entry-sibling": "an allOf entry writing enum or const beside its $ref reads that $ref a second time, " +
			"reporting it twice, where on an entry of its own the keyword draws one report",
	}
}
