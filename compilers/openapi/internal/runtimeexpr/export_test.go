// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr

// Indexed exposes how many models, operations and responses lk has indexed, for
// the external tests, which pin that a list is indexed when a value first reads
// it and not before.
func (lk *Lookups) Indexed() (models, operations, responses int) {
	return len(lk.members), len(lk.params), len(lk.headers)
}
