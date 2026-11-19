// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package componentlookup stands for the OpenAPI compiler's componentlookup package, which
// reads an inline entry's own object off the library's reference wrapper: its
// reads draw nothing here.
package componentlookup

import soa "github.com/speakeasy-api/openapi/openapi"

// Inline reads the object an inline parameter holds, as componentlookup.Object does.
func Inline(rp *soa.ReferencedParameter) *soa.Parameter { return rp.GetObject() }
