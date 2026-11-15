// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import "slices"

// DecidedKeywordList returns a copy of the keywords the census leaves alone
// because another reader decided them, for the tests that hold the list to its
// recorded members.
func DecidedKeywordList() []string { return slices.Clone(decidedKeywords[:]) }
