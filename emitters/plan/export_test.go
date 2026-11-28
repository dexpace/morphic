// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

// MethodRegistry exposes methodRegistry to plan's external tests, which hold
// it to the registry's own rows.
func MethodRegistry() map[string]Effects { return methodRegistry() }
