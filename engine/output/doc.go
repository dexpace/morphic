// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package output writes what a pipeline run produced: the IR document, to a
// stream or to a file it replaces atomically. It is the engine's writer, so a
// driver publishes a document exactly as the CLI does without encoding or
// replacing anything itself. A write whose context ends before the document is
// encoded writes none of it: a stream gets nothing, and a file keeps its
// previous content.
package output
