// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestTextOf_ReadsTheFirstSourceStatingAText pins how a stream target reads
// its sources on one node: the first to state a text wins, a later one
// stating the same is silent and one stating another is reported, and a
// value that is no text, an empty text or a member of a value that is no
// object states nothing and is reported. Each report is info, at the key, and
// names the field the target fills.
func TestTextOf_ReadsTheFirstSourceStatingAText(t *testing.T) {
	t.Parallel()
	terminator := mapping(lowering.TargetStreamTerminator,
		lowering.ExtensionSource{Key: "x-a"}, lowering.ExtensionSource{Key: "x-b"},
		lowering.ExtensionSource{Key: "x-c", Member: "data"})
	for _, tc := range []struct {
		name    string
		keys    map[string]string
		target  lowering.ExtensionTarget
		want    string
		reports []string
	}{
		{"no key", map[string]string{}, lowering.TargetStreamTerminator, "", nil},
		{"the first stating one", map[string]string{"x-b": `"[DONE]"`, "x-a": `"[DONE]"`},
			lowering.TargetStreamTerminator, "[DONE]", nil},
		{"a later source stating another", map[string]string{"x-a": `"[DONE]"`, "x-b": `"[END]"`},
			lowering.TargetStreamTerminator, "[DONE]",
			[]string{`extension "x-b" states another text than "x-a", which the policy reads first, so it fills no stream terminator`}},
		{"no text", map[string]string{"x-a": `1`}, lowering.TargetStreamTerminator, "",
			[]string{`extension "x-a" is no text, so no stream terminator is filled`}},
		{"an empty text before a text", map[string]string{"x-a": `""`, "x-b": `"[END]"`},
			lowering.TargetStreamTerminator, "[END]",
			[]string{`extension "x-a" is an empty text, so no stream terminator is filled`}},
		{"a member of no object", map[string]string{"x-c": `"[DONE]"`}, lowering.TargetStreamTerminator, "",
			[]string{`extension "x-c.data" is no text, so no stream terminator is filled`}},
		{"a member of an object", map[string]string{"x-c": `{"data": "[DONE]"}`}, lowering.TargetStreamTerminator,
			"[DONE]", nil},
		{"the condition", map[string]string{"x-w": `""`}, lowering.TargetStreamCondition, "",
			[]string{`extension "x-w" is an empty text, so no stream condition is filled`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy := terminator
			if tc.target == lowering.TargetStreamCondition {
				policy = mapping(lowering.TargetStreamCondition, lowering.ExtensionSource{Key: "x-w"})
			}
			own := ir.Unmodeled{}
			for key, value := range tc.keys {
				own["openapi:"+key] = vendorExtension(value)
			}
			st, diags := promoter(policy).TextOf(tc.target, own)
			assert.Equal(t, tc.want, st.Text)
			assert.Equal(t, tc.want != "", st.At.Pointer != "", "a key stating a text is where it sits")
			got := make([]string, 0, len(diags))
			for _, d := range diags {
				assert.Equal(t, diag.UnpromotedExtension, d.Code)
				assert.Equal(t, ir.SeverityInfo, d.Severity)
				got = append(got, d.Message)
			}
			assert.ElementsMatch(t, tc.reports, got)
		})
	}
}

// TestParamChoiceStatement_ReadsTheFirstSourceWritten pins that the choices
// are read from the first source whose key the operation writes, whatever it
// holds, and a later source's key written beside it is reported and not read.
// A source reading a member the key does not hold writes nothing.
func TestParamChoiceStatement_ReadsTheFirstSourceWritten(t *testing.T) {
	t.Parallel()
	p := promoter(mapping(lowering.TargetParamChoices, lowering.ExtensionSource{Key: "x-m", Member: "groups"},
		lowering.ExtensionSource{Key: "x-a"}, lowering.ExtensionSource{Key: "x-b"}))
	st, written, diags := p.ParamChoiceStatement(ir.Unmodeled{
		"openapi:x-m": vendorExtension(`{}`), "openapi:x-a": vendorExtension(`[1]`), "openapi:x-b": vendorExtension(`{}`),
	})
	assert.True(t, written)
	assert.Equal(t, "x-a", st.Source.Key)
	assert.Equal(t, ir.RawValue(`[1]`), st.Value)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpromotedExtension, diags[0].Code)
	assert.Equal(t, `extension "x-b" is written beside "x-a", which the policy reads first, so no choice is filled from it`,
		diags[0].Message)

	_, written, diags = p.ParamChoiceStatement(ir.Unmodeled{})
	assert.False(t, written)
	assert.Empty(t, diags)
	assert.Equal(t, "a list", lowering.ShapeOf(ir.RawValue(`[]`)))
}
