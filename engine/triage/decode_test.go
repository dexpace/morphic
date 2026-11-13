// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package triage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// TestDecode_ReadsEachMemberOfARule pins the file's spelling as literal text:
// the text is what a person writes, and a round trip through the type would
// pass however the members were named.
func TestDecode_ReadsEachMemberOfARule(t *testing.T) {
	t.Parallel()
	rules, err := triage.Decode([]byte(`{"rules": [
		{"code": "openapi/unresolved-security-scheme", "severity": "warning", "reason": "named by the gateway"},
		{"code": "openapi/degraded-construct", "pointer": "/components/schemas/A/readOnly", "accept": true, "reason": "known"},
		{"code": "pass/name-collision", "node": "s/openapi/0", "severity": "info", "reason": "two tags"}
	]}`))
	require.NoError(t, err)
	assert.Equal(t, []triage.Rule{
		{Code: "openapi/unresolved-security-scheme", Severity: ir.SeverityWarning, Reason: "named by the gateway"},
		{Code: "openapi/degraded-construct", Pointer: "/components/schemas/A/readOnly", Accept: true, Reason: "known"},
		{Code: "pass/name-collision", Node: "s/openapi/0", Severity: ir.SeverityInfo, Reason: "two tags"},
	}, rules)
}

// TestDecode_TakesAFileListingNoRule pins an object with no rules, or an empty
// list, as a file that rules nothing.
func TestDecode_TakesAFileListingNoRule(t *testing.T) {
	t.Parallel()
	for _, text := range []string{`{}`, `{"rules": []}`, `{"rules": null}`} {
		rules, err := triage.Decode([]byte(text))
		require.NoError(t, err, "%s", text)
		assert.Empty(t, rules, "%s", text)
	}
}

// TestDecode_RefusesWhatItCannotReadAsWritten pins each refusal: a misspelt
// member, read as absent, would leave a rule ruling other findings than its
// author meant.
func TestDecode_RefusesWhatItCannotReadAsWritten(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text, want string
	}{
		{"a misspelt rule member", `{"rules": [{"code": "c", "severty": "info", "reason": "r"}]}`, "severty"},
		{"a misspelt file member", `{"rule": []}`, "rule"},
		{"a member given twice", `{"rules": [{"code": "c", "code": "d", "severity": "info", "reason": "r"}]}`,
			"duplicate"},
		{"null", `null`, "holds null"},
		{"a list", `[]`, "triage:"},
		{"no JSON", `rules: []`, "triage:"},
		{"bytes that are not UTF-8", "{\"rules\": [{\"code\": \"c\xff\", \"severity\": \"info\", \"reason\": \"r\"}]}",
			"UTF-8"},
		{"a rule no run can apply", `{"rules": [{"code": "c", "accept": true, "reason": "r"}]}`,
			"accepts without naming a place"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := triage.Decode([]byte(tc.text))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
