// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// TestScalarRenderers_AgreeOnKind holds the two renderers of a YAML scalar, an
// ir.Value and a payload kept verbatim, to one classification (value.Classify,
// GitHub #784): over every tag, style and spelling classification turns on,
// both refuse a scalar or both render it, as a number, a bool, a null or a
// string alike. Two differences are deliberate, each a kind JSON has no type
// for: a !!binary is bytes in a value and its source text in a payload, and a
// scalar under the document's own tag has no value and keeps its text in a
// payload.
func TestScalarRenderers_AgreeOnKind(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"null", "~", "!!null ''", "!!null abc", "true", "false", "!!bool maybe",
		"42", "-7", "0644", "0o17", "0x1f", "0b1010", "1_000", "+5", "1.5", "1.10", ".5", "1e10",
		"1.8e308", "-1.8e308", "1e400", ".inf", "-.inf", ".nan", "!!int 1.5", "!!float abc", "!!int 5",
		`!!int "5"`, "!<tag:yaml.org,2002:int> 5", "12345678901234567890123",
		"hello", `"hello"`, "'hello'", `"1.8e308"`, "'42'", "!!str 42", "<<", "|\n  block", ">\n  folded",
		"2021-1-1", "2021-01-01T10:20:30Z", "!!timestamp 2021-1-1", "!!timestamp notadate",
		"!!binary aGVsbG8=", `!!binary "###"`, "!acme thing", "!!python/object x",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			node := openapitest.YAMLNode(t, src)
			v, valueErr := value.FromNode(node)
			raw, rawErr := RawFromNode(node)
			s, classErr := value.Classify(node)

			if classErr != nil {
				require.Error(t, valueErr, "a scalar Classify refuses no value holds")
				require.Error(t, rawErr, "and no payload keeps")
				return
			}
			require.NoError(t, rawErr)
			switch s.Kind {
			case value.ScalarBinary:
				require.NoError(t, valueErr)
				assert.Equal(t, ir.ValueBytes, v.Kind)
				assert.Equal(t, jsontext.Kind('"'), raw.Kind(), "a payload keeps the source text")
			case value.ScalarTagged:
				require.Error(t, valueErr, "no ir.Value holds a document's own tag")
				assert.Equal(t, jsontext.Kind('"'), raw.Kind(), "a payload keeps the text")
			default:
				require.NoError(t, valueErr)
				assert.Equal(t, jsonKindOf(v), raw.Kind(), "the two renderers read one kind")
			}
		})
	}
}

// jsonKindOf is the JSON kind the scalar ir.Value v is written as.
func jsonKindOf(v ir.Value) jsontext.Kind {
	switch v.Kind {
	case ir.ValueNull:
		return 'n'
	case ir.ValueBool:
		if v.Bool {
			return 't'
		}
		return 'f'
	case ir.ValueNumber:
		return '0'
	case ir.ValueString:
		return '"'
	case ir.ValueBytes, ir.ValueList, ir.ValueObject, ir.ValueSymbol, ir.ValueRefKind, ir.ValueCtor:
		return 0
	}
	return 0
}
