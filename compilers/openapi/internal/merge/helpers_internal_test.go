// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"github.com/dexpace/morphic/ir"
)

// readOnlyVisibility and writeOnlyVisibility are the two non-empty shapes
// EffectiveVisibility ever produces, reused across the visibility-merge
// cases rather than re-spelling the lifecycle lists in each one.
var (
	readOnlyVisibility  = ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	writeOnlyVisibility = ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
)
