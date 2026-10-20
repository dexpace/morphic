package planted

import (
	"net/url"

	"github.com/speakeasy-api/openapi/expression"
	"github.com/speakeasy-api/openapi/references"
)

// lookalikeReader has a GetJSONPointer of another signature.
type lookalikeReader struct{}

// GetJSONPointer is no reading of a fragment.
func (lookalikeReader) GetJSONPointer() string { return "" }

// wrapped promotes the library's GetJSONPointer.
type wrapped struct{ references.Reference }

// wrappedPtr promotes it through a pointer.
type wrappedPtr struct{ *references.Reference }

// deep promotes it two embeddings down.
type deep struct{ wrapped }

func plantFragment(r references.Reference, p *references.Reference, w wrapped, wp wrappedPtr, d deep, e expression.Expression) {
	_ = r.GetJSONPointer()                  // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	read := r.GetJSONPointer                // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_ = references.Reference.GetJSONPointer // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_ = p.GetJSONPointer()                  // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_, _ = url.QueryUnescape("a+b")         // want "ruleguard: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	unescape := url.QueryUnescape           // want "ruleguard: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_ = w.GetJSONPointer()                  // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_ = wp.GetJSONPointer()                 // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_ = d.GetJSONPointer()                  // want "ruleguard: references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer (gocritic)"
	_ = lookalikeReader{}.GetJSONPointer()
	_ = e.GetJSONPointer()
	_, _ = url.PathUnescape("a+b")
	_, _ = read, unescape
}
