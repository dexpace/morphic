package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// opWithSuccessStatus builds a document whose single operation declares one
// response and one HTTP binding carrying the given SuccessStatus map.
func opWithSuccessStatus(status map[int]int) *ir.Document {
	return docWithOperation(ir.Operation{
		ID:        "op",
		Responses: []ir.Response{{Name: ir.Naming{Source: "ok"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x", SuccessStatus: status,
		}}},
	})
}

// TestValidate_ResponseIndexOutOfRange drives both ends of the SuccessStatus
// bounds check: its keys index Operation.Responses and are ints like any other,
// so the type-driven walk cannot reach them either.
func TestValidate_ResponseIndexOutOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		status  map[int]int
		where   string
		message string
	}{
		{"negative index", map[int]int{-1: 200}, "op/bindings/http/0/successStatus/-1",
			"response index -1 at op/bindings/http/0/successStatus/-1 addresses none of the 1 declared responses"},
		{"index past the end", map[int]int{1: 202}, "op/bindings/http/0/successStatus/1",
			"response index 1 at op/bindings/http/0/successStatus/1 addresses none of the 1 declared responses"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found := withCode(validated(t, opWithSuccessStatus(tc.status)), "ir/response-index-out-of-range")
			require.Len(t, found, 1, "exactly the planted index must be out of range")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, tc.where, found[0].Provenance.Node)
			assert.Equal(t, tc.message, found[0].Message,
				"the message names the offending index, where it sits, and what it failed to address")
		})
	}
}

// TestValidate_ResponseIndexInRangeIsClean is the silent half: a key addressing a
// declared response reports nothing.
func TestValidate_ResponseIndexInRangeIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, validated(t, opWithSuccessStatus(map[int]int{0: 200})))
}

// TestValidate_ResponseIndexDiagnosticsAreDeterministic pins invariant 7 for
// SuccessStatus, whose keys Go ranges in randomized order: they are sorted before
// any diagnostic is built.
func TestValidate_ResponseIndexDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	doc := opWithSuccessStatus(map[int]int{3: 203, 1: 201, 2: 202})
	want := pointers(withCode(validated(t, doc), "ir/response-index-out-of-range"))
	require.Equal(t, []string{
		"op/bindings/http/0/successStatus/1",
		"op/bindings/http/0/successStatus/2",
		"op/bindings/http/0/successStatus/3",
	}, want)
	for range 8 {
		assert.Equal(t, want, pointers(withCode(validated(t, doc), "ir/response-index-out-of-range")))
	}
}

// TestValidate_ResponseIndexOutOfRangeIsPlacedAtItsBinding pins that an index
// out of range is reported at the binding holding it, the second here, after a
// first whose index is in range.
func TestValidate_ResponseIndexOutOfRangeIsPlacedAtItsBinding(t *testing.T) {
	t.Parallel()
	doc := opWithSuccessStatus(map[int]int{0: 200})
	op := firstOp(doc)
	op.Bindings.HTTP = append(op.Bindings.HTTP,
		ir.HTTPBinding{Method: "POST", URITemplate: "/x", SuccessStatus: map[int]int{1: 202}})

	assert.Equal(t, []string{"op/bindings/http/1/successStatus/1"},
		pointers(withCode(validated(t, doc), "ir/response-index-out-of-range")))
}
