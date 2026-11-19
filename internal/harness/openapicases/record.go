// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	_ "embed" // the records are embedded files
	"fmt"

	"github.com/dexpace/morphic/internal/harness"
)

// The records of what each case oracle finds at the core cases, and at the
// cases outside it that what the core cases find does not explain.
var (
	//go:embed testdata/spelling.txt
	spellingRecord string
	//go:embed testdata/presence.txt
	presenceRecord string
	//go:embed testdata/instances.txt
	instanceRecord string
	//go:embed testdata/full/spelling.txt
	spellingFull string
	//go:embed testdata/full/presence.txt
	presenceFull string
	//go:embed testdata/full/instances.txt
	instanceFull string
)

// FullRecord returns what case oracle oracle is known to find at every case:
// the lines at the core cases, and those at cases outside the core. A name
// listed in both is an error.
func FullRecord(oracle harness.Oracle) (harness.Recorded, error) {
	core, full, err := records(oracle)
	if err != nil {
		return nil, err
	}
	return harness.ReadRecorded([]byte(core + "\n" + full))
}

// records returns the text of oracle's record at the core cases and of its
// record outside them.
func records(oracle harness.Oracle) (core, full string, err error) {
	switch oracle {
	case harness.OracleSpelling:
		return spellingRecord, spellingFull, nil
	case harness.OraclePresence:
		return presenceRecord, presenceFull, nil
	case harness.OracleInstance:
		return instanceRecord, instanceFull, nil
	default:
		return "", "", fmt.Errorf("openapicases: oracle %q keeps no record", oracle)
	}
}
