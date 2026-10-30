// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compilers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// TestRegistry_SourceLimitIsTheLargestLimitAsked pins how far a caller reads a
// source before detection: as far as the compiler that reads furthest, so no
// compiler is handed less of a source than it would read, and without bound as
// soon as one reads any size, or none is registered to read it.
func TestRegistry_SourceLimitIsTheLargestLimitAsked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		limits []int
		want   int
	}{
		{name: "the larger second", limits: []int{10, 20}, want: 20},
		{name: "the larger first", limits: []int{20, 10}, want: 20},
		{name: "a compiler reading any size, last", limits: []int{10, 0}, want: 0},
		{name: "a compiler reading any size, first", limits: []int{0, 10}, want: 0},
		{name: "a negative limit reads any size", limits: []int{10, -1}, want: 0},
		{name: "one compiler", limits: []int{7}, want: 7},
		{name: "none registered", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var cs []compilers.Compiler
			for i, limit := range tc.limits {
				format := compilers.SourceFormat{Name: string(rune('a' + i)), Version: "1"}
				cs = append(cs, &stubCompiler{formats: []compilers.SourceFormat{format}, limit: limit})
			}

			got, err := registryOf(t, cs...).SourceLimit(nil)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRegistry_SourceLimitAsksEachCompilerUnderItsOwnOptions pins that each
// compiler's limit is read from the options it would compile with, and from its
// defaults when those fail to decode, as Detect asks it.
func TestRegistry_SourceLimitAsksEachCompilerUnderItsOwnOptions(t *testing.T) {
	t.Parallel()
	a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, limit: 5}
	b := &stubCompiler{formats: []compilers.SourceFormat{beta}, limit: 9}

	got, err := registryOf(t, a, b).SourceLimit(configureBy(map[compilers.Compiler]string{a: "for alpha"}))

	require.NoError(t, err)
	assert.Equal(t, 9, got)
	assert.Equal(t, []compilers.Options{{FormatOptions: "for alpha"}}, a.limitsAsked)
	assert.Equal(t, []compilers.Options{{}}, b.limitsAsked, "options that failed to decode are its defaults")
}

// TestRegistry_SourceLimitRefusesACompilerThatPanics requires a panic in
// configuring a compiler or in its SourceLimit to be a breach naming the call.
func TestRegistry_SourceLimitRefusesACompilerThatPanics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		at        string
		configure compilers.Configure
		wantCall  string
	}{
		{"configuring through DecodeOptions", "DecodeOptions", configureViaDecodeOptions,
			"detect by alpha@1: configure: panic: boom"},
		{"SourceLimit", "SourceLimit", nil, "source limit of alpha@1: SourceLimit: panic: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := registryOf(t, newBreaker(tt.at, "boom")).SourceLimit(tt.configure)

			requirePanic(t, err, "boom")
			assert.Contains(t, err.Error(), tt.wantCall)
			assert.Zero(t, got)
		})
	}
}

// TestRegistry_DetectHoldsEachAnswerToTheCompilersLimit pins the duty a source
// cut at the limits rests on: past its own SourceLimit a compiler declines,
// saying why, so the first bytes of a source are never taken for the whole of
// one. Within the limit, and under no limit, any answer stands. The limit is
// the one the compiler reports under the options it was asked with.
func TestRegistry_DetectHoldsEachAnswerToTheCompilersLimit(t *testing.T) {
	t.Parallel()
	refusal := []ir.Diagnostic{{Severity: ir.SeverityError, Code: "alpha/too-large"}}
	tests := []struct {
		name     string
		data     string
		limit    int
		marker   string
		declines []ir.Diagnostic
		taken    bool
		wantErr  string
	}{
		{name: "a claim at the limit", data: "alpha", limit: 5, marker: "alpha", taken: true},
		{name: "a claim with no limit", data: "alpha and more", marker: "alpha", taken: true},
		{name: "a claim past the limit", data: "alpha!", limit: 5, marker: "alpha",
			wantErr: "detect by alpha@1: claimed a source of 6 bytes, past its limit of 5"},
		{name: "a decline past the limit, saying why", data: "nothing!", limit: 5, declines: refusal},
		{name: "a decline past the limit, saying nothing", data: "nothing!", limit: 5,
			wantErr: "detect by alpha@1: declined a source of 8 bytes, past its limit of 5, saying nothing"},
		{name: "a silent decline within the limit", data: "none", limit: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: tc.marker, limit: tc.limit,
				declines: tc.declines}

			det, ok, err := registryOf(t, a).Detect(t.Context(), compilers.Source{Data: []byte(tc.data)},
				configureBy(map[compilers.Compiler]string{a: "for alpha"}))

			assert.Equal(t, []compilers.Options{{FormatOptions: "for alpha"}}, a.limitsAsked,
				"the limit under the options the answer was given under")
			if tc.wantErr != "" {
				require.ErrorIs(t, err, compilers.ErrContract)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.False(t, ok)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.taken, ok)
			if !tc.taken {
				assert.Equal(t, tc.declines, det.Declined)
			}
		})
	}
}
