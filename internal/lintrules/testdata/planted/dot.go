// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package planted

import . "strings" // want "dot-imports: should not use dot imports (revive)"

// shouted reads the dot-imported ToUpper.
var shouted = ToUpper("x")
