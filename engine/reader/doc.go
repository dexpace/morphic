// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package reader reads the documents a compile's references name, for the
// engine and for any caller that compiles directly. Disk reads the local file
// system and http(s) URLs; Memory reads sources already held in memory, as
// Disk would read them were they files at their paths; Layered reads what a
// Memory holds from it and the rest through another reader. Each implements
// compilers.DocumentReader.
package reader
