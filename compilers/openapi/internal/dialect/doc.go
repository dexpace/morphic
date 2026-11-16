// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package dialect holds the rows each schema of a compile is read with: those
// of the dialect the innermost schema resource root naming one at or above it
// names, else those of its document, else the source's.
//
// The load phase fills it once, as it reads the documents' dialects, and every
// reader asks the one Table it built, the load phase's own judgement of a
// schema and the lowerings alike, so no two readers walk the fallback chain
// apart.
package dialect
