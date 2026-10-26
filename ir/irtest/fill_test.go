package irtest_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestFill_GivesEveryTypeIDOneOfItsOwn(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{}
	irtest.Fill(t, doc)

	seen := map[string]string{}
	ir.WalkValues(doc, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		path := at.String()
		if v.Kind() == reflect.String && v.Type() == reflect.TypeFor[ir.TypeID]() {
			require.NotEmpty(t, v.String(), "%s was left empty", path)
			if first, met := seen[v.String()]; met {
				assert.Failf(t, "a type ID is shared", "%s and %s both hold %q", first, path, v.String())
			}
			seen[v.String()] = path
		}
		return true
	}))
	assert.NotEmpty(t, seen)
	assert.Len(t, doc.Services, 1, "a slice gets one element")
	assert.Len(t, doc.Messages, 1, "a map keyed by a string gets one entry")
	require.NotNil(t, doc.Contact, "a pointer is allocated")
	assert.Empty(t, doc.Contact.Name, "a string that is no type ID is left empty")
}

func TestFill_StopsNestingAStructTypeAtItsBound(t *testing.T) {
	t.Parallel()
	var v ir.Value
	irtest.Fill(t, &v)
	assert.Nil(t, v.Bytes, "bytes are left alone")
	depth := 0
	for cur := &v; len(cur.List) > 0; cur = &cur.List[0] {
		depth++
	}
	assert.Equal(t, 3, depth, "a Value nests three deep along one path, then stays zero")
}

func TestFill_RefusesAnythingButANonNilPointer(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"a value":       ir.Document{},
		"a nil pointer": (*ir.Document)(nil),
		"nothing":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingT{}
			done := make(chan struct{})
			go func() {
				defer close(done)
				irtest.FillOn(rec, v)
			}()
			<-done
			assert.True(t, rec.aborted, "fill must stop rather than fill what it cannot reach")
			assert.Contains(t, rec.fatalf, "want a non-nil pointer")
		})
	}
}
