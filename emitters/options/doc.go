// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package options reads configuration strictly, by the rules every emitter
// configuration shares (docs/emitters/contract/options.md): an unknown or
// repeated member, a name in another case and invalid UTF-8 are refused, and a
// value is read over a copy of its defaults. DecodeLayer reads a layer stating
// no version, such as a user's type table, refusing every null.
package options
