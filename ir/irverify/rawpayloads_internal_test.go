package irverify

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckRawPayloads_VisitingCostsNoAllocation holds the check's cost per
// payload map at nothing once it has met the map's type: it reads entries into
// what it keeps, where reflect.MapIter.Key and Value allocate a copy of each,
// two per entry of every Unmodeled map a document holds, and reads every
// payload through one decoder. A pooled decoder per payload costs one
// allocation for each a race build's pool drops, so a map of many entries
// almost always costs some. It is not run in parallel: AllocsPerRun measures
// the process.
func TestCheckRawPayloads_VisitingCostsNoAllocation(t *testing.T) {
	kept := func(keys ...string) ir.Unmodeled {
		u := ir.Unmodeled{}
		for _, key := range keys {
			u[key] = ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"kept":[1,"a"]}`)}
		}
		return u
	}
	many := make([]string, 0, 40)
	for i := range 40 {
		many = append(many, "openapi:x-"+strconv.Itoa(i))
	}
	doc := &ir.Document{
		Unmodeled: kept(many...),
		Types: ir.TypeRegistry{
			"t/a": &ir.Model{ID: "t/a", Unmodeled: kept("openapi:x-d")},
			"t/b": &ir.Model{ID: "t/b", Unmodeled: kept("openapi:x-e", "openapi:x-f")},
		},
		Channels: map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x", Bindings: map[string]ir.RawConfig{
			"kafka": {"topic": ir.RawValue(`"t"`), "partitions": ir.RawValue(`3`)},
			"amqp":  {"exchange": ir.RawValue(`"e"`)},
		}}},
	}
	check := checkRawPayloads(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every payload is a JSON value and every entry routable")
}

// TestPayloadReader_ReadsAsIsValidDoes holds payloadReader to the rules
// jsontext.Value.IsValid applies by default, over every string of up to five
// bytes drawn from the ones JSON's grammar turns on, an ill-formed byte among
// them, and over the shapes no short string reaches. One reader reads them
// all, so what one payload leaves in it reaching the next would show too.
func TestPayloadReader_ReadsAsIsValidDoes(t *testing.T) {
	t.Parallel()
	const alphabet, longest = "{}[]\",:1a \xff", 5
	size, width := 1, 1
	for range longest {
		width *= len(alphabet)
		size += width
	}
	payloads, shorter := make([]string, 1, size), []string{""}
	for range longest {
		longer := make([]string, 0, len(alphabet)*len(shorter))
		for _, s := range shorter {
			for i := range len(alphabet) {
				longer = append(longer, s+alphabet[i:i+1])
			}
		}
		payloads, shorter = append(payloads, longer...), longer
	}
	payloads = append(payloads, `{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `"\ud800"`, `"\u00e9"`, " 1 ", "1 2",
		`{"a":1}x`, `[1,]`, "01", "-0", "1e999", "\t\n1\r\n", strings.Repeat("[", 20000)+strings.Repeat("]", 20000))
	var reader payloadReader
	var valid int
	for _, p := range payloads {
		want := jsontext.Value(p).IsValid()
		require.Equal(t, want, reader.valid([]byte(p)), "%q", p[:min(len(p), 40)])
		if want {
			valid++
		}
	}
	assert.Positive(t, valid, "some payload is a JSON value")
}
