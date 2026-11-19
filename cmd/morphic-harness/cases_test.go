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

func (c cases) Write(at []int) (harness.CaseDocument, bool) {
	switch {
	case at[0] == 1:
		return harness.CaseDocument{}, false
	case at[0] == 2 && !c.calm:
		panic("the writer exploded")
	default:
		return harness.CaseDocument{Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}}, true
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

// TestRun_ArgumentsTheCasesRefuse pins the usage errors of -cases and -case-index.
func TestRun_ArgumentsTheCasesRefuse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"an unknown mode", []string{"-cases", "most"}, `-cases is core or full, not "most"`},
		{"paths beside cases", []string{"-cases", "core", "spec.yaml"}, "-cases checks the cases, not paths"},
		{"an index with no cases", []string{"-case-index", "3"}, "-case-index names a case of -cases"},
		{"an index that is no number", []string{"-cases", "core", "-case-index", "x"}, `invalid value "x" for flag -case-index`},
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

// TestRun_AtWritesOneCase pins -case-index: a case's name, its sources and each
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

	code, stdout, stderr := runArgs(t, formats(), "-cases", "full", "-case-index", "0")
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
	_, stdout, _ = runArgs(t, formats(), "-cases", "full", "-case-index", fmt.Sprint(n-1))
	name, _, _ = strings.Cut(stdout, "\n")
	assert.Equal(t, last, name, "the last index names the groups' last case")

	for _, at := range []int{n, -1} {
		code, stdout, stderr = runArgs(t, formats(), "-cases", "core", "-case-index", fmt.Sprint(at))
		assert.Equal(t, 2, code, "%d", at)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, fmt.Sprintf("-case-index %d names none of the %d cases", at, n))
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

// TestCheckOne_CountsOneFormatAfterAnother pins -case-index over two formats: an index
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

// spelt is a fake product of three cases, each a spelling the oracle holds to
// the second, which retitles testspec.Tiny: the first writes Tiny, and the
// last, like the first, writes Tiny again, or, when other, Tiny under another
// path, which the oracle finds as another difference.
type spelt struct{ other bool }

func (spelt) Axes() []harness.Axis {
	return []harness.Axis{{Name: "n", Values: []harness.Value{{Name: "zero"}, {Name: "one"}, {Name: "two", Like: "zero"}}}}
}

func (c spelt) Write(at []int) (harness.CaseDocument, bool) {
	spec := testspec.Tiny
	switch {
	case at[0] == 1:
		return harness.CaseDocument{Sources: []compilers.Source{{Path: "spec.yaml",
			Data: []byte(strings.Replace(spec, "title: Tiny", "title: Other", 1))}}}, true
	case at[0] == 2 && c.other:
		spec = strings.Replace(spec, "/ping", "/pong", 1)
	}
	return harness.CaseDocument{
		Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
		Equal: &harness.Equal{To: []int{1}, Rule: "docs/compilers/openapi.md#ref-use-site",
			Relocate: func(s string) string { return s }},
	}, true
}

// TestCheckCases_AFindingOutsideTheCoreIsExplainedByItsProjection pins -cases
// full over a case like a core one: its finding is explained by the line the
// record holds at the core case, so the run exits 0, unless the finding
// differs from the line's, when it is left to report and the run exits 1.
// With -case-index, a finding is written and exits 1 though the record lists it.
func TestCheckCases_AFindingOutsideTheCoreIsExplainedByItsProjection(t *testing.T) {
	t.Parallel()
	results, err := harness.CheckCases(context.Background(), spelt{}, harness.Core(spelt{}))
	require.NoError(t, err)
	found := harness.Findings(results, harness.OracleSpelling)
	require.Len(t, found, 1, "the core case differs from its partner")
	record := func(o harness.Oracle) (harness.Recorded, error) {
		if o != harness.OracleSpelling {
			return harness.Recorded{}, nil
		}
		return harness.ReadRecorded([]byte(found[0].Name + "\t" + found[0].What + "\treason\n"))
	}

	code, stdout, stderr := runArgs(t, []format{{spelt{}, record}}, "-cases", "full")
	assert.Equal(t, 0, code, "the case like zero finds what zero's line lists: %s%s", stdout, stderr)

	code, stdout, _ = runArgs(t, []format{{spelt{other: true}, record}}, "-cases", "full")
	assert.Equal(t, 1, code, "the case like zero finds another difference")
	assert.True(t, strings.HasPrefix(stdout, "two "), stdout)
	assert.NotContains(t, stdout, "\nzero ", "the core case's finding is listed")

	code, stdout, _ = runArgs(t, []format{{spelt{}, record}}, "-cases", "full", "-case-index", "0")
	assert.Equal(t, 1, code, "-case-index exits 1 on a finding the record lists")
	assert.True(t, strings.HasPrefix(stdout, "zero\n--- spec.yaml\n"), stdout)
	assert.Contains(t, stdout, "\nspelling spelling-dependent "+found[0].What+"\n")
}

// uncompiled is a fake product of two spellings of one document the compiler
// reports an error for, the first held to the second.
type uncompiled struct{}

func (uncompiled) Axes() []harness.Axis {
	return []harness.Axis{{Name: "n", Values: []harness.Value{{Name: "zero"}, {Name: "one"}}}}
}

func (uncompiled) Write(at []int) (harness.CaseDocument, bool) {
	doc := harness.CaseDocument{Sources: []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.BadHeader)}}}
	if at[0] == 0 {
		doc.Equal = &harness.Equal{To: []int{1}, Rule: "docs/compilers/openapi.md#ref-use-site",
			Relocate: func(s string) string { return s }}
	}
	return doc, true
}

// TestCheckCases_ACaseThatDoesNotCompileIsReportedUnlessListed holds a run to
// a case its oracle declines for an error diagnostic: reported as a finding
// is, exiting 1, by a run of the cases and by -case-index, and explained by a record
// listing the decline, though -case-index exits 1 on it listed or not.
func TestCheckCases_ACaseThatDoesNotCompileIsReportedUnlessListed(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runArgs(t, []format{{uncompiled{}, emptyRecord}}, "-cases", "full")
	assert.Equal(t, 1, code, stderr)
	assert.True(t, strings.HasPrefix(stdout, "zero declined declined: error diagnostic\n\n"), stdout)

	listing := func(o harness.Oracle) (harness.Recorded, error) {
		if o != harness.OracleSpelling {
			return harness.Recorded{}, nil
		}
		return harness.ReadRecorded([]byte("zero\tdeclined: error diagnostic\twhy\n"))
	}
	code, stdout, _ = runArgs(t, []format{{uncompiled{}, listing}}, "-cases", "full")
	assert.Equal(t, 0, code, stdout)

	code, stdout, _ = runArgs(t, []format{{uncompiled{}, listing}}, "-cases", "full", "-case-index", "0")
	assert.Equal(t, 1, code, "-case-index exits 1 on a decline a record lists as a finding")
	assert.Contains(t, stdout, "\nspelling declined: error diagnostic\n")

	code, stdout, _ = runArgs(t, []format{{uncompiled{}, listing}}, "-cases", "full", "-case-index", "1")
	assert.Equal(t, 0, code, "a case with no partner is asked nothing: %s", stdout)
}
