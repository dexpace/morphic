// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
)

// valuesSource holds the scalars an RFC 9535 comparison has to read as the
// compiler reads them: a hexadecimal integer, a plain date, a mapping written
// in another order and an empty member.
const valuesSource = head + `paths:
  /a: {get: {operationId: a, x-since: 2021-01-01, x-limit: 0x10, responses: {'200': {description: ok}}}}
  /b: {get: {operationId: b, x-since: 2022-02-02, x-limit: 0, responses: {'200': {description: ok}}}}
  /c: {get: {operationId: c, x-meta: {p: 1, q: 2}, x-same: {q: 2, p: 1}, responses: {'200': {description: ok}}}}
`

// appliedRead is applied with the source read as the compiler reads one, so a
// plain date is a string and 0x10 the number 16, where applied's source is
// decoded by the YAML library, which tags a date as a timestamp.
func appliedRead(t *testing.T, src, ov string, lax bool) (overlay.Origin, []string, []string) {
	t.Helper()
	root := overlayDoc(t, src)
	origin, diags := overlay.Apply(t.Context(), overlayIndex, root, overlayDoc(t, ov),
		overlay.Options{Path: "o.yaml", Data: []byte(ov), Lax: lax})
	return origin, holders(root, "summary", "hit"), codes(diags)
}

// TestApply_SelectsByTheValuesAFilterComparesNotByTheirSpelling pins what a
// filter's comparison reads in a source: 0x10 is 16, a plain date is its text,
// a mapping equals another whatever order it writes its members in, and two
// empty nodelists are equal.
func TestApply_SelectsByTheValuesAFilterComparesNotByTheirSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, target string
		want         []string
	}{
		{"a plain date is a string", `$.paths.*[?@['x-since'] == '2021-01-01']`, []string{"/paths//a/get"}},
		{"a comparison against another node of the document", `$.paths[?@.get['x-since'] == $.paths['/b'].get['x-since']]`,
			[]string{"/paths//b"}},
		{"zero is not 0x10", `$.paths.*[?@['x-limit'] == 0]`, []string{"/paths//b/get"}},
		{"0x10 is sixteen", `$.paths.*[?@['x-limit'] == 16]`, []string{"/paths//a/get"}},
		{"objects equal in any order, and so do empty nodelists", `$.paths.*[?@['x-meta'] == @['x-same']]`,
			[]string{"/paths//a/get", "/paths//b/get", "/paths//c/get"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ov := v11 + "  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {summary: hit}\n"

			origin, hit, got := appliedRead(t, valuesSource, ov, true)

			assert.True(t, origin.Applied(), "%v", got)
			assert.Equal(t, tc.want, hit)
			assert.Empty(t, got, "no finding")
		})
	}
}

// membersSource is valuesSource with two members whose names the compiler once
// refused: one holding a carriage return, written as the YAML escape for it,
// and one named as a literal is.
const membersSource = valuesSource + `x-m: {"a\rb": {x: 1}, "true": {x: 1}}` + "\n"

// TestApply_ReadsAMemberNamedByAnEscapeOrALiteral pins the targets the
// compiler once refused as unsupported: a function in a filter, an escape in a
// bracketed name and a member named true. Each is a query like any other, and
// selects what RFC 9535 says it does.
func TestApply_ReadsAMemberNamedByAnEscapeOrALiteral(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, target string
		hit          []string
		codes        []string
	}{
		{"match over a member nothing has", `$.paths.*[?match(@.summary, 'x')]`, nil, []string{"warning " + diag.OverlayAction}},
		{"a name holding a carriage return", `$['x-m']['a\rb']`, []string{"/x-m/a\rb"}, nil},
		{"a name written as a literal", `$['x-m'].true`, []string{"/x-m/true"}, nil},
		{"a name written in brackets", `$['x-m']['true']`, []string{"/x-m/true"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ov := v11 + "  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {t: 1}\n"
			root := overlayDoc(t, membersSource)

			origin, diags := overlay.Apply(t.Context(), overlayIndex, root, overlayDoc(t, ov),
				overlay.Options{Path: "o.yaml", Data: []byte(ov), Lax: true})

			assert.True(t, origin.Applied(), "%+v", diags)
			sameCodes(t, tc.codes, diags)
			assert.Equal(t, tc.hit, holders(root, "t", "1"))
		})
	}
}

// TestApply_RefusesATargetRFC9535DoesNotReadAndSaysWhere pins the refusal of a
// selector RFC 9535 does not write: an invalid overlay at the action, in
// either mode, naming the offset the evaluator stops at and, for the two
// mistakes the compiler can say what to write instead of, that.
func TestApply_RefusesATargetRFC9535DoesNotReadAndSaysWhere(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, target, says string }{
		{"a member name with a dash", "$.info.x-internal", "['x-internal']"},
		{"the property-name operator", "$.info~", "~"},
		{"an unclosed filter", "$[?(@.a)", ""},
		{"a number with a leading zero", "$.info[?@.a == 01]", ""},
	}
	offset := regexp.MustCompile(`its target does not read as an RFC 9535 query at offset \d+`)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, lax := range []bool{false, true} {
				ov := v11 + "  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {x: 1}\n"
				origin, root, diags := applied(t, head, ov, lax)

				assert.False(t, origin.Applied())
				sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
				assert.Equal(t, "/actions/0", string(diags[0].Provenance.Pointer), "lax=%v", lax)
				assert.Regexp(t, offset, diags[0].Message)
				assert.Contains(t, diags[0].Message, tc.says)
				sameValue(t, head, root)
			}
		})
	}
}
