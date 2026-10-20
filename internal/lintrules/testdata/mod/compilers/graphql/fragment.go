package graphql

import (
	"net/url"

	"github.com/speakeasy-api/openapi/expression"
	"github.com/speakeasy-api/openapi/references"
)

// lookalikeReader has a GetJSONPointer of its own.
type lookalikeReader struct{}

// GetJSONPointer is no reading of a fragment.
func (lookalikeReader) GetJSONPointer() string { return "" }

// Reference is named like the library's type, with its method.
type Reference string

// GetJSONPointer is no reading of a fragment either.
func (Reference) GetJSONPointer() string { return "" }

// QueryUnescape is named like the function in net/url.
func QueryUnescape(s string) (string, error) { return s, nil }

// wrapped promotes the library's GetJSONPointer.
type wrapped struct{ references.Reference }

// wrappedPtr promotes it through a pointer.
type wrappedPtr struct{ *references.Reference }

// deep promotes it three embeddings down.
type deep struct{ deeper }

type deeper struct{ wrapped }

// aliased names the library's type otherwise.
type aliased = references.Reference

func plantFragment(r references.Reference, p *references.Reference, w wrapped, wp wrappedPtr, d deep, a aliased, e expression.Expression, err error) {
	_ = r.GetJSONPointer()                  // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	read := r.GetJSONPointer                // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = references.Reference.GetJSONPointer // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = p.GetJSONPointer()                  // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = w.GetJSONPointer()                  // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = wp.GetJSONPointer()                 // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = d.GetJSONPointer()                  // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = a.GetJSONPointer()                  // want "fragmentreadings: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_, _ = url.QueryUnescape("a+b")         // want "fragmentreadings: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	unescape := url.QueryUnescape           // want "fragmentreadings: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	_ = lookalikeReader{}.GetJSONPointer()
	_ = e.GetJSONPointer()
	_, _ = url.PathUnescape("a+b")
	_ = Reference("").GetJSONPointer()
	_, _ = QueryUnescape("a+b")
	_ = err.Error()
	_, _ = read, unescape
}
