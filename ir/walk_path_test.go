package ir_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// listOf returns a document whose one example is a list of n string values, so
// a walk of it reaches n elements, each by an index.
func listOf(n int) *ir.Document {
	list := make([]ir.Value, n)
	for i := range list {
		list[i] = ir.Value{Kind: ir.ValueString, Str: "s"}
	}
	return &ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{
		ID:       "t/m",
		Examples: []ir.Example{{Value: &ir.Value{Kind: ir.ValueList, List: list}}},
	}}}
}

// TestWalkPath_NothingIsRenderedForAVisitThatReadsNoPath holds the walk to
// building no path a visitor does not ask for (GitHub #813). Every element of a
// list is reached by an index, which the walk used to format into a string as it
// went, so a walk of a thousand elements cost a thousand allocations more than a
// walk of ten. A visitor that never reads its path must find the two equal. It is
// not run in parallel: AllocsPerRun measures the process.
func TestWalkPath_NothingIsRenderedForAVisitThatReadsNoPath(t *testing.T) {
	short, long := listOf(10), listOf(1000)
	var reached int
	visit := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool {
		reached++
		return true
	})
	ir.WalkValues(long, "doc", visit)
	require.Greater(t, reached, 1000, "the walk must reach every element, or the count compares nothing")

	assert.Equal(t,
		testing.AllocsPerRun(10, func() { _ = ir.WalkValues(short, "doc", visit) }),
		testing.AllocsPerRun(10, func() { _ = ir.WalkValues(long, "doc", visit) }),
		"990 more elements must cost no more allocations when no path is read")
}

// TestWalkPath_StringAndAppendToRenderAlike holds the two renderings to one
// spelling at every value of a document, AppendTo after a prefix it keeps.
func TestWalkPath_StringAndAppendToRenderAlike(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Name: "api", Types: ir.TypeRegistry{"t/x/M": &ir.Model{
		ID:       "t/x/M",
		Examples: []ir.Example{{Value: &ir.Value{Kind: ir.ValueList, List: []ir.Value{{Kind: ir.ValueNull}}}}},
	}}}
	var visits int
	ir.WalkValues(doc, "doc", ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		visits++
		assert.Equal(t, at.String(), string(at.AppendTo(nil)))
		assert.Equal(t, "prefix:"+at.String(), string(at.AppendTo([]byte("prefix:"))))
		return true
	}))
	assert.Positive(t, visits)
}

// TestWalkPath_ZeroValueIsEmpty pins the zero WalkPath, which no walk is behind:
// it renders nothing, rather than dereferencing a walk that is not there.
func TestWalkPath_ZeroValueIsEmpty(t *testing.T) {
	t.Parallel()
	var p ir.WalkPath
	assert.Empty(t, p.String())
	assert.Equal(t, []byte("kept"), p.AppendTo([]byte("kept")))
}

// TestWalkPath_LongPathsRenderWhole holds a path longer than String's stack
// buffer to rendering whole: one past it only costs an allocation more.
func TestWalkPath_LongPathsRenderWhole(t *testing.T) {
	t.Parallel()
	long := string(make([]byte, 600))
	var got string
	ir.WalkValues(map[string]int{long: 1}, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Kind() == reflect.Int {
			got = at.String()
		}
		return true
	}))
	assert.Equal(t, "doc["+long+"]", got)
}

// Map key types fmt spells each its own way under %v.
type (
	stringerKey  string
	keyError     string
	formatterKey int
	plainKey     string
)

func (plainKey) Unrelated() {}

func (k stringerKey) String() string { return "<" + string(k) + ">" }

func (k keyError) Error() string { return "error " + string(k) }

func (k formatterKey) Format(f fmt.State, _ rune) { _, _ = fmt.Fprintf(f, "#%d", int(k)) }

// TestWalkPath_MapKeysAreSpelledAsFmtSpellsThem holds a key's path segment to
// fmt's %v of the key, which is what the walk spelled before it rendered keys
// itself: plainly for a string or integer with none of fmt's methods, and
// through the method otherwise. Behind an unexported field fmt cannot call one,
// so it spells the key plainly there too.
func TestWalkPath_MapKeysAreSpelledAsFmtSpellsThem(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		root any
		want string
	}{
		{"a string", map[string]int{"a": 1}, "doc[a]"},
		{"a named string", map[ir.TypeID]int{"t/x/A": 1}, "doc[t/x/A]"},
		{"a named string with methods, none of them fmt's", map[plainKey]int{"p": 1}, "doc[p]"},
		{"a negative int", map[int]int{-7: 1}, "doc[-7]"},
		{"an int8", map[int8]int{-8: 1}, "doc[-8]"},
		{"a uint", map[uint]int{9: 1}, "doc[9]"},
		{"a uint8", map[uint8]int{255: 1}, "doc[255]"},
		{"a uintptr", map[uintptr]int{3: 1}, "doc[3]"},
		{"a bool", map[bool]int{true: 1}, "doc[true]"},
		{"a float", map[float64]int{1.5: 1}, "doc[1.5]"},
		{"a Stringer", map[stringerKey]int{"s": 1}, "doc[<s>]"},
		{"an error", map[keyError]int{"e": 1}, "doc[error e]"},
		{"a Formatter", map[formatterKey]int{4: 1}, "doc[#4]"},
		{"a Stringer behind an unexported field", struct{ m map[stringerKey]int }{map[stringerKey]int{"u": 1}}, "doc.m[u]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			ir.WalkValues(tt.root, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
				if v.Kind() == reflect.Int && v.Int() == 1 {
					got = append(got, at.String())
				}
				return true
			}))
			assert.Equal(t, []string{tt.want}, got)
		})
	}
}

// countdownKey spells itself as the distance from 100, so its spellings sort in
// the opposite order to its values.
type countdownKey int

func (k countdownKey) String() string { return fmt.Sprint(100 - int(k)) }

// TestWalkPath_EntriesAreOrderedByTheirSpelling holds the walk's order to the
// spelling a path carries, so a key type spelling itself through a method is
// ordered by that spelling and not by its value.
func TestWalkPath_EntriesAreOrderedByTheirSpelling(t *testing.T) {
	t.Parallel()
	var got []string
	ir.WalkValues(map[countdownKey]int{1: 1, 2: 1}, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Kind() == reflect.Int && v.Type() != reflect.TypeFor[countdownKey]() {
			got = append(got, at.String())
		}
		return true
	}))
	assert.Equal(t, []string{"doc[98]", "doc[99]"}, got)
}
