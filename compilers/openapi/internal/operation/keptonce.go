// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"maps"

	"github.com/dexpace/morphic/ir"
)

// keptOnce is what the service walk keeps for the Document rather than a node:
// the declarations read so far, by coordinate, and the entries those reads kept.
// LowerService returns the entries once every lowering of the walk has returned.
type keptOnce struct {
	read    map[string]bool
	entries ir.Unmodeled
}

func newKeptOnce() *keptOnce {
	return &keptOnce{read: map[string]bool{}, entries: ir.Unmodeled{}}
}

// once runs keep the first time a lowering reaches the declaration at coordinate,
// holds what it keeps for the Document (entries), and returns its report. A later
// reach returns nothing, so a declaration a $ref lowers once per reference costs
// one read and one copy, whatever number reach it (GitHub #1240). keep must read
// only that declaration, which is what makes the first reach as good as any.
func (k *keptOnce) once(coordinate string, keep func() (ir.Unmodeled, []ir.Diagnostic)) []ir.Diagnostic {
	if k.read[coordinate] {
		return nil
	}
	k.read[coordinate] = true
	entries, diags := keep()
	maps.Copy(k.entries, entries)
	return diags
}
