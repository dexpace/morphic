package irverify

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestCheckRawPayloads_VisitingCostsNoAllocation holds the check's cost per
// payload map at nothing once it has met the map's type: it reads entries into
// what it keeps, where reflect.MapIter.Key and Value allocate a copy of each,
// two per entry of every Unmodeled map a document holds. It is not run in
// parallel: AllocsPerRun measures the process.
func TestCheckRawPayloads_VisitingCostsNoAllocation(t *testing.T) {
	kept := func(keys ...string) ir.Unmodeled {
		u := ir.Unmodeled{}
		for _, key := range keys {
			u[key] = ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"kept":true}`)}
		}
		return u
	}
	doc := &ir.Document{
		Unmodeled: kept("openapi:x-a", "openapi:x-b", "openapi:x-c"),
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
