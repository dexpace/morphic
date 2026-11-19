// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
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
	terminator := mapping(promotion.TargetStreamTerminator,
		promotion.ExtensionSource{Key: "x-a"}, promotion.ExtensionSource{Key: "x-b"},
		promotion.ExtensionSource{Key: "x-c", Member: "data"})
	for _, tc := range []struct {
		name    string
		keys    map[string]string
		target  promotion.ExtensionTarget
		want    string
		reports []string
	}{
		{"no key", map[string]string{}, promotion.TargetStreamTerminator, "", nil},
		{"the first stating one", map[string]string{"x-b": `"[DONE]"`, "x-a": `"[DONE]"`},
			promotion.TargetStreamTerminator, "[DONE]", nil},
		{"a later source stating another", map[string]string{"x-a": `"[DONE]"`, "x-b": `"[END]"`},
			promotion.TargetStreamTerminator, "[DONE]",
			[]string{`extension "x-b" states another value than "x-a", which the policy reads first, so it does ` +
				`not fill stream.terminator`}},
		{"no text", map[string]string{"x-a": `1`}, promotion.TargetStreamTerminator, "",
			[]string{`extension "x-a" is no text, so it does not fill stream.terminator`}},
		{"an empty text before a text", map[string]string{"x-a": `""`, "x-b": `"[END]"`},
			promotion.TargetStreamTerminator, "[END]",
			[]string{`extension "x-a" is an empty text, so it does not fill stream.terminator`}},
		{"a member of no object", map[string]string{"x-c": `"[DONE]"`}, promotion.TargetStreamTerminator, "",
			[]string{`extension "x-c" is not an object, so no member of it is read`}},
		{"a member of an object", map[string]string{"x-c": `{"data": "[DONE]"}`}, promotion.TargetStreamTerminator,
			"[DONE]", nil},
		{"the condition", map[string]string{"x-w": `""`}, promotion.TargetStreamCondition, "",
			[]string{`extension "x-w" is an empty text, so it does not fill stream.condition`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy := terminator
			if tc.target == promotion.TargetStreamCondition {
				policy = mapping(promotion.TargetStreamCondition, promotion.ExtensionSource{Key: "x-w"})
			}
			own := ir.Unmodeled{}
			for key, value := range tc.keys {
				own["openapi:"+key] = vendorExtension(value)
			}
			st, diags := promotion.New(policy).TextOf(tc.target, own)
			assert.Equal(t, tc.want, st.Text)
			assert.Equal(t, tc.want != "", st.At.Pointer != "", "a key stating a text is where it sits")
			got := make([]string, 0, len(diags))
			for _, diagnostic := range diags {
				assert.Equal(t, diag.UnpromotedExtension, diagnostic.Code)
				assert.Equal(t, ir.SeverityInfo, diagnostic.Severity)
				got = append(got, diagnostic.Message)
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
	promoter := promotion.New(mapping(promotion.TargetParamChoices, promotion.ExtensionSource{Key: "x-m", Member: "groups"},
		promotion.ExtensionSource{Key: "x-a"}, promotion.ExtensionSource{Key: "x-b"}))
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
			st, written, diags := promoter.StatementOf(promotion.TargetParamChoices, ir.Unmodeled{
				"openapi:x-m": vendorExtension(`{}`), "openapi:x-a": vendorExtension(`[1]`),
				"openapi:x-b": vendorExtension(tc.later),
			})
			assert.True(t, written)
			assert.Equal(t, "x-a", st.Source.Key)
			assert.Equal(t, ir.RawValue(`[1]`), st.Value)
			got := make([]string, 0, len(diags))
			for _, diagnostic := range diags {
				assert.Equal(t, diag.UnpromotedExtension, diagnostic.Code)
				got = append(got, diagnostic.Message)
			}
			assert.ElementsMatch(t, tc.reports, got)
		})
	}

	_, written, diags := promoter.StatementOf(promotion.TargetParamChoices, ir.Unmodeled{})
	assert.False(t, written)
	assert.Empty(t, diags)
}
