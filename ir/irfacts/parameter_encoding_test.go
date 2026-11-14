// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// encodedParam is a parameter whose type is "u", written as "x" and decoded to
// "t".
func encodedParam() ir.Parameter {
	return ir.Parameter{
		ID: "param/test/q", Name: ir.Naming{Source: "q"}, Type: useRef("u"),
		Encoding: &ir.Encoding{WireType: &ir.TypeRef{Target: "x"}, Schema: &ir.TypeRef{Target: "t"}},
	}
}

// TestIndex_UsageReadsAParametersEncoding pins that what a parameter's
// encoding decodes to is used as the parameter's own type is, as a response
// header's is, and that the type it is written as is no usage root, as for a
// property's.
func TestIndex_UsageReadsAParametersEncoding(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Params: []ir.Parameter{encodedParam()}},
		useType("t"), useType("u"), useType("x"))

	got := usageOf(doc)
	assert.Equal(t, useIn, got["u"], "the parameter's type")
	assert.Equal(t, useIn, got["t"], "what its encoding decodes to")
	assert.Zero(t, got["x"], "a wire type says how a value is written, not a value it holds")
}

// TestIndex_PartsReadAFieldArgumentsEncoding pins that a field argument's
// encoding is read by the type graph as its property's is: what it decodes to
// is a part the type holds as a field argument, and its wire type is none.
func TestIndex_PartsReadAFieldArgumentsEncoding(t *testing.T) {
	t.Parallel()
	arg := encodedParam()
	m := &ir.Model{ID: "m", Properties: []ir.Property{{ID: "p/f", Args: []ir.Parameter{arg}}}}

	_, _, _, args := irfacts.Parts(m)
	assert.ElementsMatch(t, []ir.TypeID{"u", "t"}, args)
}

// TestCorrelation_KeysWhatAParametersEncodingDecodesTo pins that the type a
// parameter's encoding decodes to is keyed under the parameter, as a response
// header's is, and the type it is written as is not.
func TestCorrelation_KeysWhatAParametersEncodingDecodesTo(t *testing.T) {
	t.Parallel()
	op := namedOp("op/test/a", "getA", "/a")
	op.Params = []ir.Parameter{{
		ID: "param/test/q", Name: ir.Naming{Source: "q"}, Type: useRef("t/anon/u"),
		Encoding: &ir.Encoding{WireType: &ir.TypeRef{Target: "t/anon/x"}, Schema: &ir.TypeRef{Target: "t/anon/t"}},
	}}
	op.Bindings.HTTP[0].ParamBindings = []ir.HTTPParamBinding{
		{Param: "param/test/q", Location: ir.HTTPLocationQuery, WireName: "q"},
	}

	assertKeys(t, opsDoc([]ir.Operation{op}, anonAny("t/anon/u"), anonAny("t/anon/t"), anonAny("t/anon/x")),
		map[string]string{
			"t/anon/u": "route:GET ~1a/param:query q/schema",
			"t/anon/t": "route:GET ~1a/param:query q/decoded",
			"t/anon/x": "",
		})
}
