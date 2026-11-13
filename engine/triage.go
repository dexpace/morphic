// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/engine/triage"
)

// maxTriageBytes bounds a triage file ReadTriage reads, 16 MiB. A rule takes a
// few hundred bytes, so the bound holds one for each of tens of thousands of
// findings, past what the largest spec measured reports.
const maxTriageBytes = 16 << 20

// ReadTriage reads the triage file at path, a regular file as a spec is, for
// RunOptions.Triage: no further than 16 MiB, and decoded and checked as
// triage.Decode says. A file it cannot read, one past the bound and one Decode
// refuses are each an error naming path.
func ReadTriage(ctx context.Context, path string) ([]triage.Rule, error) {
	data, err := reader.ReadSource(ctx, path, maxTriageBytes)
	if err != nil {
		return nil, fmt.Errorf("engine: read triage file %q: %w", path, err)
	}
	if len(data) > maxTriageBytes {
		return nil, fmt.Errorf("engine: triage file %q holds more than %d bytes", path, maxTriageBytes)
	}
	rules, err := triage.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("engine: triage file %q: %w", path, err)
	}
	return rules, nil
}
