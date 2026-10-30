package pass_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

// wide is how many entries each collection of soundDocument holds, past the
// eight a map holds before it allocates.
const wide = 12

// soundDocument returns a document Validate finds nothing wrong with, of n
// entities, each reaching every rule family with collections wider than a map
// holds unallocated: a model with properties, aliases and a discriminator
// routing to its subtypes, a union routing to its variants, an enum, a channel
// on a server with its messages, and an operation binding parameters,
// paginating to an item property the walk meets before its model, keyed by a
// token, with multipart request, response, error and default payloads and a
// message binding with a reply.
func soundDocument(n int) *ir.Document {
	doc := &ir.Document{
		IRVersion: ir.IRVersion, Name: "t", Version: "1",
		Types:    ir.TypeRegistry{"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}},
		Channels: map[ir.ChannelID]ir.Channel{},
		Messages: map[ir.MessageID]ir.Message{},
		Servers:  map[ir.ServerID]ir.Server{"server/x/a": {ID: "server/x/a", Name: ir.Naming{Source: "api"}, URLTemplate: "https://api.example.com"}},
	}
	group := ir.OperationGroup{ID: "g/x"}
	commonErrors := make([]ir.ErrorCase, 0, n)
	for i := range n {
		model := addEntityTypes(doc, i)
		messages := addEntityMessages(doc, i)
		group.Operations = append(group.Operations, entityOperation(i, model, messages))
		commonErrors = append(commonErrors, ir.ErrorCase{Payload: jsonPayload(model.ID)})
	}
	doc.Services = []ir.Service{{ID: "s/x", Servers: []ir.ServerID{"server/x/a"}, CommonErrors: commonErrors, Groups: []ir.OperationGroup{group}}}
	return doc
}

// addEntityTypes adds entity i's model, its subtypes, a union over them and an
// enum to doc, and returns the model.
func addEntityTypes(doc *ir.Document, i int) *ir.Model {
	str := ir.TypeRef{Target: "t/prim/string"}
	model := &ir.Model{ID: ir.TypeID(fmt.Sprintf("t/x/M%d", i)), Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{}}}
	union := &ir.Union{ID: ir.TypeID(fmt.Sprintf("t/x/U%d", i)), Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{}}}
	enum := &ir.Enum{ID: ir.TypeID(fmt.Sprintf("t/x/E%d", i))}
	for j := range wide {
		wire := fmt.Sprintf("w%d", j)
		model.Properties = append(model.Properties, ir.Property{
			ID: ir.PropID(fmt.Sprintf("p/x/M%d/%s", i, wire)), WireName: wire, Type: str,
			Name: ir.Naming{Source: wire, Aliases: []string{wire + "a", wire + "b"}},
		})
		sub := &ir.Model{ID: ir.TypeID(fmt.Sprintf("t/x/M%d/S%d", i, j)), Base: &ir.TypeRef{Target: model.ID}}
		doc.Types[sub.ID] = sub
		model.Discriminator.Mapping[wire] = sub.ID
		union.Variants = append(union.Variants, ir.Variant{Type: ir.TypeRef{Target: sub.ID}})
		union.Discriminator.Mapping[wire] = sub.ID
		enum.Members = append(enum.Members, ir.EnumMember{ID: ir.EnumMemberID(fmt.Sprintf("e/x/E%d/%s", i, wire))})
	}
	model.Discriminator.Property = model.Properties[0].ID
	model.Discriminator.Default = model.Discriminator.Mapping["w0"]
	for _, td := range []ir.TypeDef{model, union, enum} {
		doc.Types[td.Common().ID] = td
	}
	return model
}

// addEntityMessages adds entity i's channel and the messages it carries to doc,
// and returns the channel's ID and the messages'.
func addEntityMessages(doc *ir.Document, i int) entityMessages {
	ch := ir.Channel{ID: ir.ChannelID(fmt.Sprintf("c/x/C%d", i)), Servers: []ir.ServerID{"server/x/a"}}
	for j := range wide {
		id := ir.MessageID(fmt.Sprintf("m/x/C%d/M%d", i, j))
		doc.Messages[id] = ir.Message{ID: id, Payload: *jsonPayload("t/prim/string")}
		ch.Messages = append(ch.Messages, id)
	}
	doc.Channels[ch.ID] = ch
	return entityMessages{channel: ch.ID, messages: ch.Messages}
}

// entityMessages is the channel an entity's operation binds and the messages
// it carries.
type entityMessages struct {
	channel  ir.ChannelID
	messages []ir.MessageID
}

// entityOperation returns entity i's operation over model and the channel of
// messages.
func entityOperation(i int, model *ir.Model, messages entityMessages) ir.Operation {
	op := ir.Operation{
		ID:        ir.OpID(fmt.Sprintf("op/x/O%d", i)),
		Request:   multipartBody(model),
		Responses: []ir.Response{{Payload: jsonPayload(model.ID)}},
		Errors:    []ir.ErrorCase{{Payload: jsonPayload(model.ID)}},
		Default:   &ir.Response{Payload: jsonPayload(model.ID), Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{}}}},
	}
	binding := ir.HTTPBinding{SuccessStatus: map[int]int{0: 200}}
	for j := range wide {
		id := ir.ParamID(fmt.Sprintf("op/x/O%d/q%d", i, j))
		op.Params = append(op.Params, ir.Parameter{ID: id, Name: ir.Naming{Source: fmt.Sprintf("q%d", j)}})
		binding.ParamBindings = append(binding.ParamBindings, ir.HTTPParamBinding{Param: id, Location: ir.HTTPLocationQuery})
	}
	op.Bindings.HTTP = []ir.HTTPBinding{binding}
	op.Pagination = &ir.Pagination{
		InputCursor: &ir.ParamPath{Param: op.Params[0].ID}, InputLimit: &ir.ParamPath{Param: op.Params[1].ID},
		Items: &ir.PropPath{Segments: []ir.PropID{model.Properties[0].ID}},
	}
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: op.Params[2].ID}
	reply := messages.channel
	op.Bindings.Message = &ir.MessageBinding{
		Channel: messages.channel, Messages: messages.messages,
		Reply: &ir.Reply{Channel: &reply, Messages: messages.messages},
	}
	return op
}

// multipartBody returns a request body sending model as multipart parts,
// one encoding per property.
func multipartBody(model *ir.Model) *ir.Payload {
	content := ir.Content{MediaType: "multipart/form-data", Type: ir.TypeRef{Target: model.ID}, Encoding: map[ir.PropID]ir.PartEncoding{}}
	for _, p := range model.Properties {
		content.Encoding[p.ID] = ir.PartEncoding{}
	}
	return &ir.Payload{Contents: []ir.Content{content}}
}

// jsonPayload returns a JSON body of the type target.
func jsonPayload(target ir.TypeID) *ir.Payload {
	return &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: target}}}}
}

// TestValidate_ASoundValueCostsNothing holds the walk Validate takes, and each
// rule family once the run's index is built, to costing less than one
// allocation more per entity added when the document doubles, so a location
// or a kept path is spelled, and a list or a map made, only for a diagnostic:
// each entity reaches every family with collections wider than a map holds
// unallocated. A map or a log the run keeps grows in steps, far fewer.
// Building the index allocates per type it indexes, which this leaves out
// (GitHub #1150). It is not run in parallel: AllocsPerRun measures the
// process.
func TestValidate_ASoundValueCostsNothing(t *testing.T) {
	const entities = 50
	few, many := soundDocument(entities), soundDocument(2*entities)
	require.Empty(t, validated(t, many), "the document is sound, or what is measured is a report")

	pair := soundDocument(2) // every kind of value the walk meets
	require.NotEmpty(t, pass.KeptPropIDs(t, pair), "an item property is kept for its model to settle")
	assert.Empty(t, irtest.AllocatingVisits(pair, pass.WalkVisitors(t, pair)...), "the walk's visits")
	walked := pass.WalkAllocs(t, many) - pass.WalkAllocs(t, few)
	assert.Less(t, walked, float64(entities), "the walk: %v allocations more", walked)
	fewCosts, manyCosts := pass.FamilyAllocs(t, few), pass.FamilyAllocs(t, many)
	require.Len(t, manyCosts, pass.Families())
	for i := range manyCosts {
		assert.Less(t, manyCosts[i]-fewCosts[i], float64(entities), "family %d: %v allocations for %v",
			i, manyCosts[i], fewCosts[i])
	}
}
