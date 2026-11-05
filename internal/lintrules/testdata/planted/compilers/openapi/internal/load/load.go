// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package load stands for the OpenAPI compiler's load phase, which may read
// the version a document declares: its reads draw nothing here.
package load

import soa "github.com/speakeasy-api/openapi/openapi"

// Declared reads the version doc declares, as the load phase does, its core
// model's spelling among them.
func Declared(doc *soa.OpenAPI) (string, string, string) {
	return doc.OpenAPI, doc.GetOpenAPI(), doc.GetCore().OpenAPI.Value
}
