package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

func TestIntern_IdempotentOnSamePointer(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	calls := 0
	build := func() ir.TypeDef {
		calls++
		return &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "x"}, Prim: ir.PrimString}
	}
	first := l.intern("/p", "x", build)
	second := l.intern("/p", "x", build)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls, "build runs only on first intern of a pointer")
}

func TestPrimID_SecondCallReuses(t *testing.T) {
	t.Parallel()
	l := newLowerer(0, &loaded{Doc: nil, Source: ir.SourceInfo{}}, Options{}.withDefaults())
	first := l.primID(ir.PrimString)
	second := l.primID(ir.PrimString)
	assert.Equal(t, first, second, "interned primitive reused on second call")
}
