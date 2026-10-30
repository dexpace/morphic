// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"math/bits"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// loggedDoc reaches every kind of step a path takes: fields, a promoted one
// among them, elements, map keys and map values, nested and side by side.
func loggedDoc() *ir.Document {
	doc := declaringDoc()
	m := &ir.Model{TypeCommon: populatedTypeCommon("t/x/P"), Properties: []ir.Property{populatedProperty(), {ID: "p/x/P/b"}}}
	doc.Types[m.ID] = m
	doc.Unmodeled = populatedUnmodeled()
	doc.Services[0].Renames = map[ir.TypeID]ir.Naming{"t/x/M": populatedNaming(), "t/x/P": {Source: "p"}}
	return doc
}

// recorded is what a walk showed a visitor at one value: the path's spelling,
// the field it ends in, and whether it ends in a map key.
type recorded struct {
	path   string
	owner  reflect.Type
	field  string
	ok     bool
	mapKey bool
}

// recordingOf returns what at shows of itself.
func recordingOf(at ir.WalkPath) recorded {
	owner, field, ok := at.Field()
	return recorded{path: at.String(), owner: owner, field: field, ok: ok, mapKey: at.IsMapKey()}
}

// TestPathLog_ReplaysEachPathAsTheWalkShowedIt holds a replayed path to the one
// the walk showed when it was recorded, at every value of a document whose
// paths share prefixes of every length and part ways at every kind of step,
// spelled alike and reading alike through Field and IsMapKey.
func TestPathLog_ReplaysEachPathAsTheWalkShowedIt(t *testing.T) {
	t.Parallel()
	var log ir.PathLog
	var shown []recorded
	ir.WalkValues(loggedDoc(), ir.DocumentPath, ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		shown = append(shown, recordingOf(at))
		log.Record(at)
		return true
	}))
	require.Equal(t, len(shown), log.Len())
	require.Contains(t, shown, recorded{path: "doc.Unmodeled[openapi:a-ext].key", mapKey: true}, "a map key is reached")

	var replayed []recorded
	for i, at := range log.All() {
		require.Len(t, replayed, i, "positions run in the order recorded")
		replayed = append(replayed, recordingOf(at))
	}
	assert.Equal(t, shown, replayed)
}

// TestPathLog_KeepsEachWalksRoot holds a log fed by two walks to the root each
// path was reached from, though the second walk's path shares its steps with
// the first's.
func TestPathLog_KeepsEachWalksRoot(t *testing.T) {
	t.Parallel()
	var log ir.PathLog
	var shown []string
	for _, root := range []string{"a", "b"} {
		ir.WalkValues(&ir.Document{Name: "x"}, root, ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
			if at.String() == root+".Name" {
				shown = append(shown, at.String())
				log.Record(at)
			}
			return true
		}))
	}
	var replayed []string
	for _, at := range log.All() {
		replayed = append(replayed, at.String())
	}
	assert.Equal(t, []string{"a.Name", "b.Name"}, shown)
	assert.Equal(t, shown, replayed)
}

// TestPathLog_KeepsTheTypeEachFieldIsSpelledOn holds a log fed by walks of two
// types to the struct each step names its field on: the second path ends in
// the field at the same position of its struct as the first path's does, so
// only the struct's type tells the two steps apart.
func TestPathLog_KeepsTheTypeEachFieldIsSpelledOn(t *testing.T) {
	t.Parallel()
	version, _ := reflect.TypeFor[ir.Document]().FieldByName("IRVersion")
	id, _ := reflect.TypeFor[ir.Channel]().FieldByName("ID")
	require.Equal(t, version.Index, id.Index, "the two fields share a position, or the owner decides nothing")

	var log ir.PathLog
	for _, walk := range []struct {
		root any
		want string
	}{{&ir.Document{IRVersion: "x"}, "a.IRVersion"}, {&ir.Channel{ID: "c/x"}, "a.ID"}} {
		ir.WalkValues(walk.root, "a", ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
			if at.String() == walk.want {
				log.Record(at)
			}
			return true
		}))
	}
	var replayed []string
	for _, at := range log.All() {
		replayed = append(replayed, at.String())
	}
	assert.Equal(t, []string{"a.IRVersion", "a.ID"}, replayed)
}

// TestPathLog_KeepsTheZeroPathAsEmpty pins the zero WalkPath, which no walk is
// behind: it is kept as the empty path it renders, beside a path a walk showed.
func TestPathLog_KeepsTheZeroPathAsEmpty(t *testing.T) {
	t.Parallel()
	var log ir.PathLog
	log.Record(ir.WalkPath{})
	ir.WalkValues(&ir.Document{Name: "x"}, ir.DocumentPath, ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		if at.String() == "doc.Name" {
			log.Record(at)
		}
		return true
	}))
	var replayed []string
	for _, at := range log.All() {
		replayed = append(replayed, at.String())
	}
	assert.Equal(t, []string{"", "doc.Name"}, replayed)
}

// TestPathLog_StopsWhereTheCallerStops holds a replay to yielding nothing after
// the caller breaks off.
func TestPathLog_StopsWhereTheCallerStops(t *testing.T) {
	t.Parallel()
	var log ir.PathLog
	ir.WalkValues(&ir.Document{Name: "x"}, ir.DocumentPath, ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		log.Record(at)
		return true
	}))
	require.Greater(t, log.Len(), 1)
	var seen []int
	for i := range log.All() {
		seen = append(seen, i)
		break
	}
	assert.Equal(t, []int{0}, seen)
}

// TestPathLog_RecordingCostsNoAllocation holds keeping a path at nothing once
// the log has grown, where keeping its String costs a string per value. Each
// visit is handed the walk's own path, so spelling one would count. It is not
// run in parallel: AllocsPerRun measures the process.
func TestPathLog_RecordingCostsNoAllocation(t *testing.T) {
	var log ir.PathLog
	recording := ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		log.Record(at)
		return true
	})
	assert.Empty(t, irtest.AllocatingVisits(loggedDoc(), recording))
	assert.Positive(t, log.Len(), "the visits recorded what they were shown")
}

// TestPathLog_KeepsASharedStepOnce holds the log to storing the steps a path
// shares with the one recorded before it once: a walk's paths share all but
// their last step or two, so storing each whole would hold several steps per
// path where this holds about one.
func TestPathLog_KeepsASharedStepOnce(t *testing.T) {
	t.Parallel()
	var log ir.PathLog
	ir.WalkValues(listOf(1000), ir.DocumentPath, ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		log.Record(at)
		return true
	}))
	require.Greater(t, log.Len(), 1000, "the walk reaches every element, or nothing is shared")
	assert.Less(t, log.StoredSteps(), 2*log.Len(), "%d steps for %d paths", log.StoredSteps(), log.Len())
}

// TestPathLog_GrowsGeometrically holds the log's growth to a factor well past
// append's: append grows a large slice by a quarter at a time, so a log built
// one path at a time would be copied several times over, which on a
// golden-spec document cost tens of megabytes. Recording n paths may grow each
// of its slices about log2(n) times. It is not run in parallel: AllocsPerRun
// measures the process.
func TestPathLog_GrowsGeometrically(t *testing.T) {
	const paths = 1 << 14
	doc := listOf(paths)
	recording := testing.AllocsPerRun(3, func() {
		var log ir.PathLog
		ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
			log.Record(at)
			return true
		}))
	})
	walking := testing.AllocsPerRun(3, func() {
		ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true }))
	})
	assert.Less(t, recording-walking, float64(3*(bits.Len(paths)+2)), "%v allocations to record", recording-walking)
}
