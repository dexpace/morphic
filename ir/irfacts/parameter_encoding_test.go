// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// encodedParam is a parameter whose type is "u", written as "x" and decoded to
// "t".
func encodedParam() ir.Parameter {
	return ir.Parameter{
		ID: "parameter/test/q", Name: ir.Naming{Source: "q"}, Type: useRef("u"),
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
	model := &ir.Model{ID: "m", Properties: []ir.Property{{ID: "property/f", Args: []ir.Parameter{arg}}}}

	_, _, _, args := irfacts.Parts(model)
	assert.ElementsMatch(t, []ir.TypeID{"u", "t"}, args)
}

// TestCorrelation_KeysWhatAParametersEncodingDecodesTo pins that the type a
// parameter's encoding decodes to is keyed under the parameter, as a response
// header's is, and the type it is written as is not.
func TestCorrelation_KeysWhatAParametersEncodingDecodesTo(t *testing.T) {
	t.Parallel()
	operation := namedOperation("operation/test/a", "getA", "/a")
	operation.Params = []ir.Parameter{{
		ID: "parameter/test/q", Name: ir.Naming{Source: "q"}, Type: useRef("type/anonymous/u"),
		Encoding: &ir.Encoding{WireType: &ir.TypeRef{Target: "type/anonymous/x"}, Schema: &ir.TypeRef{Target: "type/anonymous/t"}},
	}}
	operation.Bindings.HTTP[0].ParamBindings = []ir.HTTPParamBinding{
		{Param: "parameter/test/q", Location: ir.HTTPLocationQuery, WireName: "q"},
	}

	assertKeys(t, operationsDoc([]ir.Operation{operation}, anonymousAny("type/anonymous/u"), anonymousAny("type/anonymous/t"), anonymousAny("type/anonymous/x")),
		map[string]string{
			"type/anonymous/u": "route:GET ~1a/param:query q/schema",
			"type/anonymous/t": "route:GET ~1a/param:query q/decoded",
			"type/anonymous/x": "",
		})
}

// TestCorrelation_KeysWhatAFieldArgumentsEncodingDecodesTo pins that the type a
// field argument's encoding decodes to is keyed under the argument, as an
// operation parameter's is, and the type it is written as is not.
func TestCorrelation_KeysWhatAFieldArgumentsEncodingDecodesTo(t *testing.T) {
	t.Parallel()
	arg := ir.Parameter{
		ID: "parameter/test/q", Name: ir.Naming{Source: "q"}, Type: useRef("type/anonymous/u"),
		Encoding: &ir.Encoding{WireType: &ir.TypeRef{Target: "type/anonymous/x"}, Schema: &ir.TypeRef{Target: "type/anonymous/t"}},
	}
	holder := &ir.Model{ID: "type/test/Holder", Name: ir.Naming{Source: "Holder"}, Properties: []ir.Property{{
		ID: "property/test/f", Name: ir.Naming{Source: "f"}, Type: useRef("type/anonymous/f"), Args: []ir.Parameter{arg},
	}}}

	assertKeys(t, irtest.DocOf(holder, anonymousAny("type/anonymous/f"), anonymousAny("type/anonymous/u"), anonymousAny("type/anonymous/t"), anonymousAny("type/anonymous/x")),
		map[string]string{
			"type/anonymous/u": "type:Holder/prop:f/arg:q/schema",
			"type/anonymous/t": "type:Holder/prop:f/arg:q/decoded",
			"type/anonymous/x": "",
		})
}
