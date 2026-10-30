// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package annotation reads the documentation-adjacent facts a schema or a
// carrier declares — descriptions, deprecation, redaction, visibility, XML
// hints, extensions — and the validation-only JSON Schema keywords the IR keeps
// verbatim rather than models.
//
// It exists because those two jobs share one question: given a $ref, does the
// fact belong to the referenced declaration or to the site that references it?
// Site and Home answer it once: no reader here resolves a referent of its own,
// so a new annotation cannot quietly pick another referent.
//
// It is also how this compiler keeps a construct verbatim, once a lowering
// decides to keep it.
package annotation
