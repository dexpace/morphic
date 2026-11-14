// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestTextOf_ReadsTheFirstSourceStatingAText pins how a stream target reads
// its sources on one node: the first to state a text wins, a later one
// stating the same is silent and one stating another is reported, and a
// value that is no text or an empty text states nothing and is reported, as is
// a member of a value that is no object. Each report is info, at the key, and
// names the target it does not fill.
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
			[]string{`extension "x-b" states another value than "x-a", which the policy reads first, so it does ` +
				`not fill stream.terminator`}},
		{"no text", map[string]string{"x-a": `1`}, lowering.TargetStreamTerminator, "",
			[]string{`extension "x-a" is no text, so it does not fill stream.terminator`}},
		{"an empty text before a text", map[string]string{"x-a": `""`, "x-b": `"[END]"`},
			lowering.TargetStreamTerminator, "[END]",
			[]string{`extension "x-a" is an empty text, so it does not fill stream.terminator`}},
		{"a member of no object", map[string]string{"x-c": `"[DONE]"`}, lowering.TargetStreamTerminator, "",
			[]string{`extension "x-c" is not an object, so no member of it is read`}},
		{"a member of an object", map[string]string{"x-c": `{"data": "[DONE]"}`}, lowering.TargetStreamTerminator,
			"[DONE]", nil},
		{"the condition", map[string]string{"x-w": `""`}, lowering.TargetStreamCondition, "",
			[]string{`extension "x-w" is an empty text, so it does not fill stream.condition`}},
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

// TestStatementOf_ReadsTheFirstSourceWritten pins that a target read whole is
// read from the first source whose key the node writes, whatever it holds: a
// later source writing the same value is silent, and one writing another is
// reported and not read. A source reading a member the key does not hold
// writes nothing.
func TestStatementOf_ReadsTheFirstSourceWritten(t *testing.T) {
	t.Parallel()
	p := promoter(mapping(lowering.TargetParamChoices, lowering.ExtensionSource{Key: "x-m", Member: "groups"},
		lowering.ExtensionSource{Key: "x-a"}, lowering.ExtensionSource{Key: "x-b"}))
	for _, tc := range []struct {
		name, later string
		reports     []string
	}{
		{"a later source writing another value", `{}`, []string{`extension "x-b" states another value than "x-a", ` +
			`which the policy reads first, so it does not fill operation.paramChoices`}},
		{"a later source writing the same value", `[1]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, written, diags := p.StatementOf(lowering.TargetParamChoices, ir.Unmodeled{
				"openapi:x-m": vendorExtension(`{}`), "openapi:x-a": vendorExtension(`[1]`),
				"openapi:x-b": vendorExtension(tc.later),
			})
			assert.True(t, written)
			assert.Equal(t, "x-a", st.Source.Key)
			assert.Equal(t, ir.RawValue(`[1]`), st.Value)
			var got []string
			for _, d := range diags {
				assert.Equal(t, diag.UnpromotedExtension, d.Code)
				got = append(got, d.Message)
			}
			assert.Equal(t, tc.reports, got)
		})
	}

	_, written, diags := p.StatementOf(lowering.TargetParamChoices, ir.Unmodeled{})
	assert.False(t, written)
	assert.Empty(t, diags)
	assert.Equal(t, "a list", lowering.ShapeOf(ir.RawValue(`[]`)))
}
