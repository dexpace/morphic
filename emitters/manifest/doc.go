// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package manifest holds the generation manifest and pending manifest as data,
// their strict canonical JSON codecs, and the normalized content digest rule
// (docs/emitters/write-integrate.md).
//
// It performs no I/O; reading and writing the manifest files and mapping decode
// failures to engine/manifest-* findings stay in the engine.
package manifest
