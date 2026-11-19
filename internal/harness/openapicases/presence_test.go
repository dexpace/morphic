// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir/irtest"
)

// presenceRecord is the file listing what the presence oracle finds, and
// presenceHeader the comment it opens with.
const (
	presenceRecord = "testdata/presence.txt"
	presenceHeader = "What the presence oracle finds at the core cases, and the reason each finding\n" +
		"stands: case name, what was found, reason (Reasons).\n"
)

// notCarried are the keywords nothing carries, which the cases write as the
// control: each changes nothing.
var notCarried = []string{"$anchor", "$comment", "$defs", "$dynamicAnchor"}

// presence returns result's verdict of the presence oracle.
func presence(t *testing.T, result harness.Result) harness.Verdict {
	t.Helper()
	i := slices.IndexFunc(result.Verdicts, func(verdict harness.Verdict) bool { return verdict.Oracle == harness.OraclePresence })
	require.GreaterOrEqual(t, i, 0, result.Spec)
	return result.Verdicts[i]
}

// TestCore_EveryKeywordIsCarriedEverywhere holds each keyword of the rule
// table, at every core position, to the document: written it changes what
// compiles, and again when its value changes, and a keyword nothing carries
// changes nothing. What the oracle finds is the record's, each line with the
// reason it stands; the four keywords nothing carries are asked at every
// position and found equal to the case leaving them out. No case faults: a
// fault gives no verdict, so -update would delete the line of a case it ended.
func TestCore_EveryKeywordIsCarriedEverywhere(t *testing.T) {
	results := coreResults(t)
	for _, result := range results {
		require.False(t, result.Faulted(), "%s: %s: %s", result.Spec, result.Outcome, result.Detail)
	}
	found := harness.Findings(results, harness.OraclePresence)
	record, err := openapicases.Record(harness.OraclePresence)
	require.NoError(t, err)

	if irtest.Update() {
		require.NoError(t, os.WriteFile(presenceRecord, record.Write(presenceHeader, found), 0o644))
		return
	}
	unlisted, changed, stale, unreasoned := record.Judge(found, openapicases.Reasons())
	assert.Empty(t, unlisted, "a finding the record does not list: fix the compiler, or list it with its reason (-update)")
	assert.Empty(t, changed)
	assert.Empty(t, stale, "a line whose finding is gone: delete it (-update)")
	assert.Empty(t, unreasoned)

	askedAt := map[string]map[string]int{}
	for _, result := range results {
		parts := strings.Split(result.Spec, "/")
		verdict := presence(t, result)
		if !verdict.Asked() {
			assert.Contains(t, []harness.Decline{harness.DeclineNoConstruct, harness.DeclineErrorDiagnostic}, verdict.Declined, result.Spec)
			continue
		}
		if askedAt[parts[2]] == nil {
			askedAt[parts[2]] = map[string]int{}
		}
		askedAt[parts[2]][parts[4]]++
	}
	positions := openapicases.New().Axes()[4].Values
	for _, keyword := range keywords(t) {
		want := len(positions)
		if keyword == only30Keyword {
			want -= len(from31Positions)
		}
		assert.Len(t, askedAt[keyword], want, "%s is asked at every position it is written at", keyword)
	}
}

// TestCore_TheControlsAreAskedAndChangeNothing holds each keyword nothing
// carries to the verdict that it was asked of the document and passed: it
// compiles to what the case leaving it out does, at every position.
func TestCore_TheControlsAreAskedAndChangeNothing(t *testing.T) {
	asked := map[string]int{}
	for _, result := range coreResults(t) {
		keyword := strings.Split(result.Spec, "/")[2]
		if !slices.Contains(notCarried, keyword) {
			continue
		}
		verdict := presence(t, result)
		assert.True(t, verdict.Asked(), "%s: %s", result.Spec, verdict.Declined)
		assert.False(t, verdict.Found(), "%s: %s", result.Spec, verdict.Detail)
		asked[keyword]++
	}
	for _, keyword := range notCarried {
		assert.Positive(t, asked[keyword], keyword)
	}
}

// only30Keyword is the one keyword a 3.0 schema alone writes, which no position
// written from 3.1 holds, and from31Positions are those positions.
const only30Keyword = "nullable"

var from31Positions = []string{"prefixItem", "patternProperty", "contentSchema"}

// keywords returns the keyword each construct of the cases writes.
func keywords(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, value := range openapicases.New().Axes()[2].Values {
		if value.Name != "none" && !strings.HasSuffix(value.Name, "~") {
			out = append(out, value.Name)
		}
	}
	require.NotEmpty(t, out)
	return out
}

// TestWrite_NamesTheNeighboursOfEachKeyword holds each case the presence
// oracle asks to its neighbours: the case leaving the keyword out, which
// differs in the construct alone, or beside a second named as a construct is
// that construct's case, or in a spelling writing the keywords on an entry,
// beside a second writing no member, none or a second $ref, which is an allOf
// entry, is wrap's; and for a
// keyword something carries the case writing its other value, one construct
// on. The case writing nothing and a keyword's other value name none. A second
// is held at one position (alone).
func TestWrite_NamesTheNeighboursOfEachKeyword(t *testing.T) {
	cases := openapicases.New()
	axes := cases.Axes()
	var kept, replaced []string
	each, err := harness.Each(cases)
	require.NoError(t, err)
	for _, at := range each {
		if !alone(at) {
			continue
		}
		doc, ok := cases.Write(at)
		if !ok {
			continue
		}
		name := axes[2].Values[at[2]].Name
		if name == "none" || strings.HasSuffix(name, "~") {
			assert.Nil(t, doc.Absent, "%v", at)
			assert.Nil(t, doc.Other, "%v", at)
			continue
		}
		with := func(neighbour []int) []int { return slices.Concat(at[:2], neighbour, at[3:]) }
		absent := with([]int{0})
		second := axes[3].Values[at[3]].Name
		row := slices.IndexFunc(axes[2].Values, func(value harness.Value) bool { return value.Name == second })
		switch s := axes[5].Values[at[5]].Name; {
		case second != "none" && row >= 0:
			absent[2], absent[3] = row, 0 // a second named as a construct, alone, is that construct's case
		case (second == "none" || second == "secondRef") && (s == "entry" || s == "entryFirst" || s == "besideEntry"):
			absent[5] = slices.IndexFunc(axes[5].Values, func(value harness.Value) bool { return value.Name == "wrap" })
		}
		assert.Equal(t, absent, doc.Absent, "%v", at)
		assert.Equal(t, doc.Carried, doc.Reason == "", "%v: a keyword nothing carries says why", at)
		if doc.Replaces {
			replaced = append(replaced, axes[1].Values[at[1]].Name+"/"+name)
		}
		if !doc.Carried {
			kept = append(kept, name)
			assert.Nil(t, doc.Other, "%v", at)
			continue
		}
		assert.Equal(t, with([]int{at[2] + 1}), doc.Other, "%v", at)
		assert.Equal(t, name+"~", axes[2].Values[doc.Other[2]].Name, "%v", at)
	}
	slices.Sort(kept)
	assert.Equal(t, notCarried, slices.Compact(kept))
	slices.Sort(replaced)
	assert.Equal(t, []string{"array/items", "enum/enum", "map/additionalProperties", "object/properties", "union/oneOf"},
		slices.Compact(replaced), "the keyword a target writes already is replaced, not added")
}
