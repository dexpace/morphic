// Package annotation reads the documentation-adjacent facts a schema or a
// carrier declares — descriptions, deprecation, redaction, visibility, XML
// hints, extensions — and the validation-only JSON Schema keywords the IR keeps
// verbatim rather than models.
//
// It exists because those two jobs share one question: given a $ref, does the
// fact belong to the referenced declaration or to the site that references it?
// Site and Home answer it once, and every reader here routes through them, so a
// new annotation cannot quietly pick the other answer.
package annotation
