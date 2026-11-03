// Package load stands for the load phase, the other package that may read the
// version a document declares.
package load

import soa "github.com/speakeasy-api/openapi/openapi"

// SupportedMinor stands for the load phase's version gate, and reads the
// library's field and method, which draws nothing here.
func SupportedMinor(doc *soa.OpenAPI) (string, bool) {
	return doc.OpenAPI, doc.GetOpenAPI() != ""
}
