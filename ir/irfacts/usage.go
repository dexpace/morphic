package irfacts

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// UsageFlags is a bitset recording how a type is used across the API surface,
// from the client's side of the wire. [Index.Usage] derives it and no type
// stores it, so the bits cannot disagree with the operations they describe.
// Input and Output say which way a value travels, Error and Multipart in what
// role, so a type in an error payload also carries the side it travels on.
type UsageFlags uint32

// Usage bits.
const (
	// UsageInput marks a type the client writes.
	UsageInput UsageFlags = 1 << iota
	// UsageOutput marks a type the client reads.
	UsageOutput
	// UsageError marks a type reached from the payload or headers of an error.
	UsageError
	// UsageMultipart marks the type of a multipart body itself, and the types
	// its alias scalars stand for. The types nested in it travel as ordinary
	// encoded parts and are not marked.
	UsageMultipart
)

// spreadingUsage are the bits that spread from a root to the types it holds.
const spreadingUsage = UsageInput | UsageOutput | UsageError

// multipartPrefix starts the media type of every multipart body.
const multipartPrefix = "multipart/"

// Usage returns how the type id is used across the API surface (ir-design
// §4.9): 0 for a type no root reaches and for an id the document does not
// declare. The roots are the places an operation, a service's common errors,
// a message, a channel or an auth scheme name a type. A request is written and
// a response read, and an operation the API itself starts, a webhook or a
// callback, swaps the two. A flag spreads along the type graph, and a field
// argument makes the type it names Input. Operations nested past
// ir.MaxGroupDepth are not read.
func (x *Index) Usage(id ir.TypeID) UsageFlags {
	return x.usageIndex()[id]
}

// usageStep is a vertex of the type graph and the flags arriving at it.
type usageStep struct {
	vertex int
	flags  UsageFlags
}

// newUsage derives the usage of every type of doc over g, the graph of its
// registry. A vertex passes on only the flags it had not reached before, so it
// is expanded at most once per flag and a cycle ends.
func newUsage(doc *ir.Document, g *typeGraph) map[ir.TypeID]UsageFlags {
	roots := usageRoots{}
	roots.document(doc)
	var pending []usageStep
	for id, flags := range roots {
		if v := g.reference(id); v >= 0 && flags&spreadingUsage != 0 {
			pending = append(pending, usageStep{vertex: v, flags: flags & spreadingUsage})
		}
	}
	reached := make([]UsageFlags, len(g.next))
	for len(pending) > 0 {
		step := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		fresh := step.flags &^ reached[step.vertex]
		if fresh == 0 {
			continue
		}
		reached[step.vertex] |= fresh
		for _, w := range g.next[step.vertex] {
			pending = append(pending, usageStep{vertex: w, flags: fresh})
		}
		for _, w := range g.args[step.vertex] {
			pending = append(pending, usageStep{vertex: w, flags: UsageInput})
		}
	}
	usage := map[ir.TypeID]UsageFlags{}
	for v, flags := range reached {
		if flags != 0 {
			usage[g.typeOf(v)] |= flags
		}
	}
	for id, flags := range roots {
		if flags&UsageMultipart != 0 {
			markMultipart(doc.Types, usage, id)
		}
	}
	return usage
}

// markMultipart marks id, and each type its alias scalars stand for, as the
// type of a multipart body. It stops at a type already marked, so a chain named
// by many bodies is followed once, and a cycle ends.
func markMultipart(types ir.TypeRegistry, usage map[ir.TypeID]UsageFlags, id ir.TypeID) {
	for {
		if _, declared := types[id]; !declared || usage[id]&UsageMultipart != 0 {
			return
		}
		usage[id] |= UsageMultipart
		base, alias := ir.AliasBase(types, id)
		if !alias {
			return
		}
		id = base
	}
}

// usageRoots maps each type a root names to the usage bits of the places that
// name it.
type usageRoots map[ir.TypeID]UsageFlags

// document records every root of doc: each operation's, each service's common
// errors, each channel's parameters and each auth scheme's credentials.
func (r usageRoots) document(doc *ir.Document) {
	if doc == nil {
		return
	}
	callbacks := callbackOperations(doc)
	ir.ForEachOperation(doc, func(op ir.Operation) {
		r.operation(doc, &op, callbacks[op.ID] || isWebhook(&op))
	})
	for i := range doc.Services {
		for j := range doc.Services[i].CommonErrors {
			r.errorCase(&doc.Services[i].CommonErrors[j], UsageOutput)
		}
	}
	for _, ch := range doc.Channels {
		r.params(ch.Params, UsageInput)
	}
	for _, scheme := range doc.Auth {
		r.ref(scheme.Credentials, UsageInput)
	}
}

// callbackOperations returns the operations an HTTP binding names among its
// callbacks: requests the API sends the client, not the other way round.
func callbackOperations(doc *ir.Document) map[ir.OpID]bool {
	callbacks := map[ir.OpID]bool{}
	ir.ForEachOperation(doc, func(op ir.Operation) {
		for _, b := range op.Bindings.HTTP {
			for _, cb := range b.Callbacks {
				for _, id := range cb.Operations {
					callbacks[id] = true
				}
			}
		}
	})
	return callbacks
}

// isWebhook reports whether op is an inbound webhook. The binding's IsWebhook
// says so; its WebhookName may be empty, since an empty webhooks key is legal.
func isWebhook(op *ir.Operation) bool {
	return slices.ContainsFunc(op.Bindings.HTTP, func(b ir.HTTPBinding) bool { return b.IsWebhook })
}

// operation records what op sends and what comes back. The client writes a
// request and reads a response, and the two swap when the API starts the
// operation, as it does a webhook or a callback.
func (r usageRoots) operation(doc *ir.Document, op *ir.Operation, inbound bool) {
	request, response := UsageInput, UsageOutput
	if inbound {
		request, response = response, request
	}
	r.params(op.Params, request)
	r.payload(op.Request, request)
	r.stream(op.RequestStream, request)
	if rpc := op.Bindings.RPC; rpc != nil {
		r.ref(rpc.InputType, request)
	}
	for i := range op.Responses {
		r.payload(op.Responses[i].Payload, response)
		r.headers(op.Responses[i].Headers, response)
	}
	r.stream(op.ResponseStream, response)
	if lro := op.LongRunning; lro != nil {
		r.ref(lro.PollingType, response)
		r.ref(lro.FinalType, response)
	}
	for i := range op.Errors {
		r.errorCase(&op.Errors[i], response)
	}
	r.messages(doc, op.Bindings.Message)
}

// messages records the messages a messaging binding sends or receives, and its
// reply's, which travel the other way. A direction other than send or receive
// says neither, so its messages are taken to travel both ways.
func (r usageRoots) messages(doc *ir.Document, b *ir.MessageBinding) {
	if b == nil {
		return
	}
	own, reply := UsageInput|UsageOutput, UsageInput|UsageOutput
	switch b.Direction {
	case ir.MsgDirectionSend:
		own, reply = UsageInput, UsageOutput
	case ir.MsgDirectionReceive:
		own, reply = UsageOutput, UsageInput
	default:
		// An unknown direction leaves both sides marked.
	}
	for _, id := range b.Messages {
		r.message(doc, id, own)
	}
	if b.Reply != nil {
		for _, id := range b.Reply.Messages {
			r.message(doc, id, reply)
		}
	}
}

// message records the payload and headers of the message id, if doc has one.
func (r usageRoots) message(doc *ir.Document, id ir.MessageID, flags UsageFlags) {
	m, ok := doc.Messages[id]
	if !ok {
		return
	}
	r.payload(&m.Payload, flags)
	r.ref(m.Headers, flags)
}

// errorCase records the payload and headers of e as errors travelling on side.
func (r usageRoots) errorCase(e *ir.ErrorCase, side UsageFlags) {
	r.payload(e.Payload, side|UsageError)
	r.headers(e.Headers, side|UsageError)
}

// stream records the event and initial-message types of s, when there is one.
func (r usageRoots) stream(s *ir.StreamDetail, flags UsageFlags) {
	if s == nil {
		return
	}
	r.ref(s.Events, flags)
	r.ref(s.Initial, flags)
}

// params records the type of each parameter in ps.
func (r usageRoots) params(ps []ir.Parameter, flags UsageFlags) {
	for i := range ps {
		r.ref(&ps[i].Type, flags)
	}
}

// headers records the type of each header in hs. A header holds no field
// arguments, so a header's Args are not read.
func (r usageRoots) headers(hs []ir.Property, flags UsageFlags) {
	for i := range hs {
		r.ref(&hs[i].Type, flags)
	}
}

// payload records the types of each content of p, when there is a payload.
func (r usageRoots) payload(p *ir.Payload, flags UsageFlags) {
	if p == nil {
		return
	}
	for i := range p.Contents {
		r.content(&p.Contents[i], flags)
	}
}

// content records a content's type, its stream item, its file contents and the
// headers of its parts. A multipart media type also marks the content's own
// type, and only that one.
func (r usageRoots) content(c *ir.Content, flags UsageFlags) {
	r.ref(&c.Type, flags)
	if strings.HasPrefix(strings.ToLower(c.MediaType), multipartPrefix) {
		r.ref(&c.Type, UsageMultipart)
	}
	r.ref(c.Item, flags)
	if c.File != nil {
		r.ref(c.File.Contents, flags)
	}
	if c.ItemEncoding != nil {
		r.headers(c.ItemEncoding.Headers, flags)
	}
	for _, part := range c.Encoding {
		r.headers(part.Headers, flags)
	}
}

// ref records the target of ref, if there is a reference with a target.
func (r usageRoots) ref(ref *ir.TypeRef, flags UsageFlags) {
	if ref != nil && ref.Target != "" {
		r[ref.Target] |= flags
	}
}
