package graphql

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// lower builds and writes a document's service tree and its registries but the
// type registry, as a compiler lowering a source does, which draws nothing.
func lower(doc *ir.Document, op ir.Operation, ch ir.Channel, id ir.ChannelID) {
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}}}
	doc.Services[0].Groups[0].Operations[0].Docs.Summary = "lowered"
	group := &doc.Services[0].Groups[0]
	group.Operations = append(group.Operations, op)
	slices.SortFunc(group.Operations, func(ir.Operation, ir.Operation) int { return 0 })
	doc.Channels = map[ir.ChannelID]ir.Channel{}
	doc.Channels[id] = ch
	doc.Messages = make(map[ir.MessageID]ir.Message)
	maps.DeleteFunc(doc.Auth, func(ir.AuthID, ir.AuthScheme) bool { return true })
}
