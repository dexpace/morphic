// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package pinnedspecs reads the list of public specs the nightly sweep reads
// in place, each at a pinned commit of its repository (ReadList), holds a
// fetched copy to the SHA-256 the list pins (Spec.Read), and names what
// harness.Check finds in a spec as the lines of its record (Findings), which
// harness.Recorded judges. scripts/fetch-pinned-specs.sh fetches the specs,
// and the package's tests hold each to its record under testdata. A record
// holds what a spec draws, not what it compiles to, so a loss nothing reports,
// as #2150's, passes. It is test/tooling infrastructure, not part of the
// compile pipeline.
package pinnedspecs
