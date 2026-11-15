// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// entryPrefix opens every key this compiler writes, naming its format.
const entryPrefix = "openapi:"

// Under returns src with each entry keyed beneath scope: "openapi:k" becomes
// "openapi:<scope>/k". src is not written. It keeps what one source object
// writes apart from what another writes on the carrier holding both, so the
// two can spell one key (docs/ir/unmodeled.md#keys). An empty src returns nil.
func Under(scope string, src ir.Unmodeled) ir.Unmodeled {
	if len(src) == 0 {
		return nil
	}
	out := make(ir.Unmodeled, len(src))
	for key, entry := range src {
		rest, _ := strings.CutPrefix(key, entryPrefix)
		out[entryPrefix+scope+"/"+rest] = entry
	}
	return out
}
