// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// writeTriage writes text to a triage file in a directory of the test's own,
// and returns its path.
func writeTriage(t *testing.T, text []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "triage.json")
	require.NoError(t, os.WriteFile(path, text, 0o600))
	return path
}

// TestReadTriage_ReadsTheRulesAFileLists pins a file read whole, its rules in
// the order written.
func TestReadTriage_ReadsTheRulesAFileLists(t *testing.T) {
	t.Parallel()
	path := writeTriage(t, []byte(`{"rules": [
		{"code": "x/a", "severity": "info", "reason": "first"},
		{"code": "x/b", "pointer": "/p", "accept": true, "reason": "second"}
	]}`))

	rules, err := engine.ReadTriage(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, []triage.Rule{
		{Code: "x/a", Severity: ir.SeverityInfo, Reason: "first"},
		{Code: "x/b", Pointer: "/p", Accept: true, Reason: "second"},
	}, rules)
}

// TestReadTriage_RefusesWhatItCannotRead pins each refusal to an error naming
// the file, and a rule no run can apply to the error triage refuses it with.
func TestReadTriage_RefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		cancelled bool
		path      func(t *testing.T) string
		want      string
	}{
		{"a missing file", false, func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "absent.json")
		}, "read triage file"},
		{"a directory", false, func(t *testing.T) string { return t.TempDir() }, "read triage file"},
		{"a file past the bound", false, func(t *testing.T) string {
			return writeTriage(t, bytes.Repeat([]byte(" "), engine.MaxTriageBytes+1))
		}, "holds more than 16777216 bytes"},
		{"text that is no triage file", false, func(t *testing.T) string {
			return writeTriage(t, []byte(`{"rule": []}`))
		}, `triage file "`},
		{"a cancelled run", true, func(t *testing.T) string {
			return writeTriage(t, []byte(`{"rules": []}`))
		}, "read triage file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.path(t)
			ctx, cancel := context.WithCancel(t.Context())
			if tc.cancelled {
				cancel()
			}
			t.Cleanup(cancel)
			rules, err := engine.ReadTriage(ctx, path)
			require.Error(t, err)
			assert.Nil(t, rules)
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), path, "the error names the file")
		})
	}

	_, err := engine.ReadTriage(t.Context(), writeTriage(t, []byte(`{"rules": [{"code": "x", "reason": "r"}]}`)))
	require.ErrorIs(t, err, triage.ErrInvalidRule)
}

// TestReadTriage_TakesAFileAtTheBound is the bound's other side: a file of
// exactly 16 MiB is read.
func TestReadTriage_TakesAFileAtTheBound(t *testing.T) {
	t.Parallel()
	text := []byte(`{"rules": []}`)
	text = append(text, bytes.Repeat([]byte(" "), engine.MaxTriageBytes-len(text))...)
	rules, err := engine.ReadTriage(t.Context(), writeTriage(t, text))
	require.NoError(t, err)
	assert.Empty(t, rules)
}
