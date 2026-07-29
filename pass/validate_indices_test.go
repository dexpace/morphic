package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// serverDoc is validDoc with one declared server, so index 0 addresses it and
// every other index does not.
func serverDoc() *ir.Document {
	doc := validDoc()
	doc.Servers = []ir.Server{{Name: ir.Naming{Source: "prod"}, URLTemplate: "https://api.example.com"}}
	return doc
}

// TestValidate_ServerIndexOutOfRange drives both ends of the bounds check on both
// carriers. Service.Servers and Channel.Servers are plain []int, so the
// type-driven walk cannot see them: before checkServerIndices every case here
// passed validation and left an emitter to panic on the index.
func TestValidate_ServerIndexOutOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		where string
		plant func(doc *ir.Document)
	}{
		{"service negative index", "s/servers/0", func(d *ir.Document) {
			service(d).Servers = []int{-1}
		}},
		{"service index past the end", "s/servers/0", func(d *ir.Document) {
			service(d).Servers = []int{1}
		}},
		{"channel negative index", "chan/a/servers/0", func(d *ir.Document) {
			putChannel(d, func(c *ir.Channel) { c.Servers = []int{-1} })
		}},
		{"channel index past the end", "chan/a/servers/0", func(d *ir.Document) {
			putChannel(d, func(c *ir.Channel) { c.Servers = []int{1} })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := serverDoc()
			tc.plant(doc)
			found := withCode(pass.Validate(doc), "ir/server-index-out-of-range")
			require.Len(t, found, 1, "exactly the planted index must be out of range")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, tc.where, found[0].Provenance.Pointer)
		})
	}
}

// TestValidate_ServerIndexWithNoDeclaredServers pins the degenerate case an
// emitter is likeliest to hit: a document that declares no servers at all, whose
// service still claims one.
func TestValidate_ServerIndexWithNoDeclaredServers(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	require.Empty(t, doc.Servers)
	service(doc).Servers = []int{0}

	found := withCode(pass.Validate(doc), "ir/server-index-out-of-range")
	require.Len(t, found, 1)
	assert.Contains(t, found[0].Message, "none of the 0 declared servers")
}

// TestValidate_ServerIndicesInRangeAreClean is the silent half of the proof: an
// index that does address a declared server reports nothing, on either carrier.
func TestValidate_ServerIndicesInRangeAreClean(t *testing.T) {
	t.Parallel()
	doc := serverDoc()
	service(doc).Servers = []int{0}
	putChannel(doc, func(c *ir.Channel) { c.Servers = []int{0} })
	assert.Empty(t, pass.Validate(doc))
}

// TestValidate_ServerIndexDiagnosticsAreDeterministic pins invariant 7 for the
// channel half of the check, which iterates a map: channels are visited in sorted
// key order, so the diagnostics come out the same on every run.
func TestValidate_ServerIndexDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	doc := serverDoc()
	doc.Channels = map[ir.ChannelID]ir.Channel{
		"chan/c": {ID: "chan/c", Servers: []int{7}},
		"chan/a": {ID: "chan/a", Servers: []int{8}},
		"chan/b": {ID: "chan/b", Servers: []int{9}},
	}
	want := pointers(withCode(pass.Validate(doc), "ir/server-index-out-of-range"))
	require.Equal(t, []string{"chan/a/servers/0", "chan/b/servers/0", "chan/c/servers/0"}, want)
	for range 8 {
		assert.Equal(t, want, pointers(withCode(pass.Validate(doc), "ir/server-index-out-of-range")))
	}
}

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
		name   string
		status map[int]int
		where  string
	}{
		{"negative index", map[int]int{-1: 200}, "op/bindings/http/0/successStatus/-1"},
		{"index past the end", map[int]int{1: 202}, "op/bindings/http/0/successStatus/1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found := withCode(pass.Validate(opWithSuccessStatus(tc.status)), "ir/response-index-out-of-range")
			require.Len(t, found, 1, "exactly the planted index must be out of range")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, tc.where, found[0].Provenance.Pointer)
		})
	}
}

// TestValidate_ResponseIndexInRangeIsClean is the silent half: a key addressing a
// declared response reports nothing.
func TestValidate_ResponseIndexInRangeIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, pass.Validate(opWithSuccessStatus(map[int]int{0: 200})))
}

// TestValidate_ResponseIndexDiagnosticsAreDeterministic pins invariant 7 for
// SuccessStatus, whose keys Go ranges in randomized order: they are sorted before
// any diagnostic is built.
func TestValidate_ResponseIndexDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	doc := opWithSuccessStatus(map[int]int{3: 203, 1: 201, 2: 202})
	want := pointers(withCode(pass.Validate(doc), "ir/response-index-out-of-range"))
	require.Equal(t, []string{
		"op/bindings/http/0/successStatus/1",
		"op/bindings/http/0/successStatus/2",
		"op/bindings/http/0/successStatus/3",
	}, want)
	for range 8 {
		assert.Equal(t, want, pointers(withCode(pass.Validate(doc), "ir/response-index-out-of-range")))
	}
}
