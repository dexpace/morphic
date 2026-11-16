// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	_ "embed" // the records are embedded files
	"fmt"

	"github.com/dexpace/morphic/internal/harness"
)

// The records of what each case oracle finds at the core cases.
var (
	//go:embed testdata/spelling.txt
	spellingRecord string
	//go:embed testdata/presence.txt
	presenceRecord string
	//go:embed testdata/instances.txt
	instanceRecord string
)

// Record returns what case oracle o is known to find at the core cases.
func Record(o harness.Oracle) (harness.Recorded, error) {
	var data string
	switch o {
	case harness.OracleSpelling:
		data = spellingRecord
	case harness.OraclePresence:
		data = presenceRecord
	case harness.OracleInstance:
		data = instanceRecord
	default:
		return nil, fmt.Errorf("openapicases: oracle %q keeps no record", o)
	}
	return harness.ReadRecorded([]byte(data))
}
