// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package reader reads the documents a compile's references name, for the
// engine and for any caller that compiles directly. Disk reads the local file
// system and http(s) URLs; Memory reads sources already held in memory, as
// Disk would read them were they files at their paths; Layered reads what a
// Memory holds from it and the rest through another reader. Each implements
// compilers.DocumentReader.
//
// Disk judges each address an http(s) read dials where it is dialled, every
// redirect a new dial, against the request's Reach, and states the space of
// the address that answered (compilers.DocumentRead), so a document read from
// the network makes the compile reach no address more private than its own.
package reader
