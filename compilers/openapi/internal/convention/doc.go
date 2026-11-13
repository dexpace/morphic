// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package convention fills the facts an OpenAPI document implies by the names
// it gives its fields rather than declares: an operation's idempotency token,
// by the header the key travels in; a credential's redaction, by what it is
// called; and a response header's HTTP-date text, by the field it is. Each is a
// policy the lowering context carries, and each fills only what the document
// leaves unstated (docs/compilers/openapi.md#idempotency-inference).
//
// It runs once the document is whole, since what a name implies turns on the
// type it is given, which a $ref can name before any lowering builds it.
package convention
