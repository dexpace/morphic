// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package stream reads what the promotion policy maps an operation's or a
// streaming content's keys to, of the stream an operation's success responses
// carry, once the document is whole: the frame data that ends it and the
// request input that asks for it (docs/compilers/openapi.md#streaming-media).
// It reads the document through whole.Document and an expression through
// runtimeexpr, and nothing of the walks that built the document.
package stream
