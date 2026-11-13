// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package convention fills the facts an OpenAPI document implies by the names
// it gives its fields rather than declares: an operation's idempotency token,
// by the header the key travels in; a credential's redaction, by what it is
// called; and a response header's HTTP-date text, by the field it is. Each is a
// policy, and fills only what neither a keyword nor a promotion states
// (docs/compilers/openapi.md#policies).
//
// It runs once the document is whole, after the promotions, since what a name
// implies turns on the type it is given, which a $ref can name before a
// lowering builds it.
package convention
