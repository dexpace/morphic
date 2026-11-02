// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/openapi/core"
)

// ReferencedMediaType is a content entry OpenAPI 3.2 lets a document write as a
// Reference Object, or a components/mediaTypes entry, as the library's own
// Referenced* kinds model the others. The library names no such alias and
// models neither, so the load phase builds one for each, and its resolver
// follows the $ref as it follows every other kind's
// (docs/parser-library.md#media-type-references).
type ReferencedMediaType = soa.Reference[soa.MediaType, *soa.MediaType, *core.MediaType]
