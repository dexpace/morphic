// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"maps"

	"github.com/dexpace/morphic/ir"
)

// KeptOnce is what a run keeps for the Document rather than a node: the
// declarations read so far, by coordinate, and the entries those reads kept. The
// nil KeptOnce, a zero context's, keeps nothing.
type KeptOnce struct {
	read    map[string]bool
	entries ir.Unmodeled
}

// Once runs keep the first time a lowering reaches the declaration at coordinate,
// holds what it keeps for the Document (Entries), and returns its report. A later
// reach returns nothing, so a declaration a $ref lowers once per reference costs
// one read and one copy, whatever number reach it (GitHub #1240). keep must read
// only that declaration, which is what makes the first reach as good as any.
func (k *KeptOnce) Once(coordinate string, keep func() (ir.Unmodeled, []ir.Diagnostic)) []ir.Diagnostic {
	if k == nil || k.read[coordinate] {
		return nil
	}
	k.read[coordinate] = true
	entries, diags := keep()
	maps.Copy(k.entries, entries)
	return diags
}

// Entries returns what Once kept. It is meant to be asked once every lowering has
// returned, as Ctx.Reached is.
func (k *KeptOnce) Entries() ir.Unmodeled {
	if k == nil {
		return nil
	}
	return k.entries
}
