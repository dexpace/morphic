package irverify_test

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir/irverify"
)

// readEntries returns what r reads of map m, key by key, ranging over it as a
// check does.
func readEntries(r *irverify.EntryReader, m reflect.Value) map[string]int64 {
	read := map[string]int64{}
	held := r.HeldFor(m)
	for iter := m.MapRange(); iter.Next(); {
		key, value := held.Read(iter)
		read[key.String()] = value.Int()
	}
	return read
}

// TestEntryReader_ReadsEveryEntryOfEveryMap holds the reader to each entry of
// each map it is handed, the second map of a type read into what the first was.
func TestEntryReader_ReadsEveryEntryOfEveryMap(t *testing.T) {
	t.Parallel()
	var r irverify.EntryReader
	first, second := map[string]int64{"a": 1, "b": 2}, map[string]int64{"c": 3}
	assert.Equal(t, first, readEntries(&r, reflect.ValueOf(first)))
	assert.Equal(t, second, readEntries(&r, reflect.ValueOf(second)))
	assert.Empty(t, readEntries(&r, reflect.ValueOf(map[string]int64{})))
}

// TestEntryReader_ReadsAMapBehindAnUnexportedField holds the reader to a map it
// cannot copy into what it keeps, reflect refusing to read such a map that way.
func TestEntryReader_ReadsAMapBehindAnUnexportedField(t *testing.T) {
	t.Parallel()
	entries := map[string]int64{"a": 1, "b": 2}
	hidden := reflect.ValueOf(struct{ m map[string]int64 }{m: entries}).Field(0)
	var r irverify.EntryReader
	assert.Equal(t, entries, readEntries(&r, hidden))
}

// TestEntryReader_ReadingAllocatesNothing holds the reason the reader exists:
// once it has met a map type, reading a map of it allocates nothing, where
// reflect.MapIter.Key and Value allocate a copy of each entry. It is not run in
// parallel: AllocsPerRun measures the process.
func TestEntryReader_ReadingAllocatesNothing(t *testing.T) {
	few, many := map[string]int64{"a": 1}, map[string]int64{}
	for i := range 32 {
		many[strconv.Itoa(i)] = int64(i)
	}
	var r irverify.EntryReader
	for _, m := range []reflect.Value{reflect.ValueOf(few), reflect.ValueOf(many)} {
		assert.Zero(t, testing.AllocsPerRun(10, func() {
			held := r.HeldFor(m)
			for iter := m.MapRange(); iter.Next(); {
				key, value := held.Read(iter)
				_, _ = key.String(), value.Int()
			}
		}), "a map of %d entries", m.Len())
	}
}
