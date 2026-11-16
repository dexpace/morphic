// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/internal/testspec"
)

// cases is a fake product of three cases, the middle one writing nothing and
// the last one panicking when written, unless calm.
type cases struct{ calm bool }

func (cases) Axes() []harness.Axis {
	return []harness.Axis{{Name: "n", Values: []harness.Value{{Name: "zero"}, {Name: "one"}, {Name: "two", Like: "zero"}}}}
}

func (c cases) Write(at []int) (harness.Document, bool) {
	switch {
	case at[0] == 1:
		return harness.Document{}, false
	case at[0] == 2 && !c.calm:
		panic("the writer exploded")
	default:
		return harness.Document{Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}}, true
	}
}

// emptyRecord is a format's record of no finding.
func emptyRecord(harness.Oracle) (harness.Recorded, error) { return harness.Recorded{}, nil }

// runArgs runs the command over fmts with args.
func runArgs(t *testing.T, fmts []format, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(context.Background(), fmts, args, &out, &errb)
	return code, out.String(), errb.String()
}

// TestRun_ArgumentsTheCasesRefuse pins the usage errors of -cases and -at.
func TestRun_ArgumentsTheCasesRefuse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"an unknown mode", []string{"-cases", "most"}, `-cases is core or full, not "most"`},
		{"paths beside cases", []string{"-cases", "core", "spec.yaml"}, "-cases checks the cases, not paths"},
		{"an index with no cases", []string{"-at", "3"}, "-at names a case of -cases"},
		{"an index that is no number", []string{"-cases", "core", "-at", "x"}, `invalid value "x" for flag -at`},
		{"a flag that is none", []string{"-case", "core"}, "flag provided but not defined: -case"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := runArgs(t, nil, tc.args...)
			assert.Equal(t, 2, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "morphic-harness: "+tc.want)
			assert.Contains(t, stderr, "usage:")
		})
	}
}

// TestRun_TheCasesOfEachFormat pins which cases each mode runs, over a fake
// format: core, its core case alone, which writes calmly, the report empty and
// the summary counting it; full, every case, among them one whose Write
// panics.
func TestRun_TheCasesOfEachFormat(t *testing.T) {
	t.Parallel()
	fmts := []format{{cases{}, emptyRecord}}
	code, stdout, stderr := runArgs(t, fmts, "-cases", "core")
	require.Equal(t, 0, code, stderr)
	results, err := harness.CheckCases(context.Background(), cases{}, harness.Core(cases{}))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "\n"+harness.CaseSummary(results), stdout)

	code, stdout, _ = runArgs(t, fmts, "-cases", "full")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "the writer exploded")
}

// TestRun_AtWritesOneCase pins -at: a case's name, its sources and each
// verdict; the last index names the last product's last case; and an index
// past the last case, or before the first, names none.
func TestRun_AtWritesOneCase(t *testing.T) {
	t.Parallel()
	c := openapicases.New()
	n := 0
	for _, f := range formats() {
		count, err := harness.CaseCount(f.cases)
		require.NoError(t, err)
		n += count
	}

	code, stdout, stderr := runArgs(t, formats(), "-cases", "full", "-at", "0")
	require.Equal(t, 0, code, stderr)
	name, rest, _ := strings.Cut(stdout, "\n")
	assert.Equal(t, "3.0.3/object/none/none/component/inline", name)
	doc, ok := c.Write([]int{0, 0, 0, 0, 0, 0})
	require.True(t, ok)
	assert.Equal(t, "--- openapi.json\n"+string(doc.Sources[0].Data)+"\n"+
		"spelling declined: no partner\npresence declined: no construct\ninstance declined: no instance\n", rest)

	groups := openapicases.Groups().Axes()
	last := groups[0].Values[len(groups[0].Values)-1].Name + "/" + groups[1].Values[len(groups[1].Values)-1].Name +
		"/" + groups[2].Values[len(groups[2].Values)-1].Name + "/" + groups[3].Values[len(groups[3].Values)-1].Name
	_, stdout, _ = runArgs(t, formats(), "-cases", "full", "-at", fmt.Sprint(n-1))
	name, _, _ = strings.Cut(stdout, "\n")
	assert.Equal(t, last, name, "the last index names the groups' last case")

	for _, at := range []int{n, -1} {
		code, stdout, stderr = runArgs(t, formats(), "-cases", "core", "-at", fmt.Sprint(at))
		assert.Equal(t, 2, code, "%d", at)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, fmt.Sprintf("-at %d names none of the %d cases", at, n))
	}
}

// TestCheckCases_WhatTheRecordsLeave drives the report over a fake format: a
// panic is reported and exits 1; a format whose cases or record cannot be read
// exits 2.
func TestCheckCases_WhatTheRecordsLeave(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := checkCases(context.Background(), []format{{cases{}, emptyRecord}}, true, &stdout, &stderr)
	assert.Equal(t, 0, code, "the core case writes calmly: %s", stdout.String())

	stdout.Reset()
	code = checkCases(context.Background(), []format{{cases{}, emptyRecord}}, false, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout.String(), "two panic")
	assert.Contains(t, stdout.String(), "the writer exploded")

	unreadable := func(harness.Oracle) (harness.Recorded, error) { return nil, errors.New("no record") }
	for name, f := range map[string]format{
		"a record that cannot be read": {cases{}, unreadable},
		"cases that cannot be counted": {cases: openapicases.New(), record: emptyRecord},
	} {
		stderr.Reset()
		if name == "cases that cannot be counted" {
			f.cases = harness.Cases(nil)
		}
		assert.Equal(t, 2, checkCases(context.Background(), []format{f}, true, &stdout, &stderr), name)
		assert.Contains(t, stderr.String(), "morphic-harness: ", name)
	}
}

// TestCheckOne_CountsOneFormatAfterAnother pins -at over two formats: an index
// past the first format's cases names one of the second's, and a format whose
// cases cannot be counted is a usage error.
func TestCheckOne_CountsOneFormatAfterAnother(t *testing.T) {
	t.Parallel()
	fmts := []format{{cases{calm: true}, emptyRecord}, {cases{}, emptyRecord}}
	var stdout, stderr bytes.Buffer

	assert.Equal(t, 0, checkOne(context.Background(), fmts, 3, &stdout, &stderr))
	assert.True(t, strings.HasPrefix(stdout.String(), "zero\n--- spec.yaml\n"), stdout.String())

	stdout.Reset()
	assert.Equal(t, 0, checkOne(context.Background(), fmts, 4, &stdout, &stderr))
	assert.Equal(t, "one writes no document\n", stdout.String())

	stdout.Reset()
	assert.Equal(t, 1, checkOne(context.Background(), fmts, 5, &stdout, &stderr), "the second format's last case panics")
	assert.True(t, strings.HasPrefix(stdout.String(), "two\npanic panic: the writer exploded"), stdout.String())
	assert.NotContains(t, stdout.String(), "---", "a case a panic ended is not written again")

	assert.Equal(t, 2, checkOne(context.Background(), []format{{cases: nil, record: emptyRecord}}, 0, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "no cases")
}
