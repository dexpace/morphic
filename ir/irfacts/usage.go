// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// UsageFlags is a bitset recording how a type is used across the API surface,
// from the client's side of the wire. [DocumentFacts.Usage] derives it and no
// type stores it, so the bits cannot disagree with the operations they
// describe. Input and Output say which way a value travels, Error and Multipart
// in what role, so a type in an error payload also carries the side it travels
// on.
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

// Usage returns how the type id is used across the API surface
// (docs/ir/derived-facts.md): 0 for a type no root reaches and for an id the
// document does not declare. The roots are the places an operation, the
// messages its binding names, a service's common errors, a channel or an auth
// scheme name a type. A request is written and a response read, and an
// operation the API itself starts, a webhook or a callback, swaps the two. A
// flag spreads along the type graph, and a field argument makes the type it
// names Input. Partial says when operations went unread.
func (x *DocumentFacts) Usage(id ir.TypeID) UsageFlags {
	return x.usageIndex()[id]
}

// usageStep is a vertex of the type graph and the flags arriving at it.
type usageStep struct {
	vertex int
	flags  UsageFlags
}

// newUsage derives the usage of every type of doc over typeGraphValue, the
// graph of its registry. A vertex passes on only the flags it had not reached
// before, so it is expanded at most once per flag and a cycle ends.
func newUsage(doc *ir.Document, typeGraphValue *typeGraph) map[ir.TypeID]UsageFlags {
	roots := usageRoots{}
	roots.document(doc)
	var pending []usageStep
	for id, flags := range roots {
		if v, ok := typeGraphValue.reference(id); ok && flags&spreadingUsage != 0 {
			pending = append(pending, usageStep{vertex: v, flags: flags & spreadingUsage})
		}
	}
	reached := make([]UsageFlags, len(typeGraphValue.next))
	for len(pending) > 0 {
		step := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		fresh := step.flags &^ reached[step.vertex]
		if fresh == 0 {
			continue
		}
		reached[step.vertex] |= fresh
		for _, w := range typeGraphValue.next[step.vertex] {
			pending = append(pending, usageStep{vertex: w, flags: fresh})
		}
		for _, w := range typeGraphValue.args[step.vertex] {
			pending = append(pending, usageStep{vertex: w, flags: UsageInput})
		}
	}
	usage := map[ir.TypeID]UsageFlags{}
	for v, flags := range reached {
		if flags != 0 {
			usage[typeGraphValue.typeOf(v)] |= flags
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
	ir.ForEachOperation(doc, func(operation ir.Operation) {
		r.operation(doc, &operation, callbacks[operation.ID] || isInbound(&operation))
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
func callbackOperations(doc *ir.Document) map[ir.OperationID]bool {
	callbacks := map[ir.OperationID]bool{}
	ir.ForEachOperation(doc, func(operation ir.Operation) {
		for _, b := range operation.Bindings.HTTP {
			for _, cb := range b.Callbacks {
				for _, id := range cb.Operations {
					callbacks[id] = true
				}
			}
		}
	})
	return callbacks
}

// isInbound reports whether a binding of operation marks it a request the API
// sends: a webhook or a callback. Its WebhookName may be empty, since an empty
// webhooks key is legal.
func isInbound(operation *ir.Operation) bool {
	return slices.ContainsFunc(operation.Bindings.HTTP, func(b ir.HTTPBinding) bool { return b.Inbound != "" })
}

// operation records what operation sends and what comes back. The client writes
// a request and reads a response, and the two swap when the API starts the
// operation, as it does a webhook or a callback.
func (r usageRoots) operation(doc *ir.Document, operation *ir.Operation, inbound bool) {
	request, response := UsageInput, UsageOutput
	if inbound {
		request, response = response, request
	}
	r.params(operation.Params, request)
	r.payload(operation.Request, request)
	r.stream(operation.RequestStream, request)
	if rpc := operation.Bindings.RPC; rpc != nil {
		r.ref(rpc.InputType, request)
	}
	for i := range operation.Responses {
		r.payload(operation.Responses[i].Payload, response)
		r.headers(operation.Responses[i].Headers, response)
	}
	if def := operation.Default; def != nil {
		// The default comes back like any response; whether it is an error the
		// IR does not say (docs/ir/responses.md), so it is not marked one.
		r.payload(def.Payload, response)
		r.headers(def.Headers, response)
	}
	r.stream(operation.ResponseStream, response)
	if lro := operation.LongRunning; lro != nil {
		r.ref(lro.PollingType, response)
		r.ref(lro.FinalType, response)
	}
	for i := range operation.Errors {
		r.errorCase(&operation.Errors[i], response)
	}
	r.messages(doc, operation.Bindings.Message)
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
	case ir.MessageDirectionSend:
		own, reply = UsageInput, UsageOutput
	case ir.MessageDirectionReceive:
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
	message, ok := doc.Messages[id]
	if !ok {
		return
	}
	r.payload(&message.Payload, flags)
	r.ref(message.Headers, flags)
}

// errorCase records the payload and headers of errorCase as errors travelling
// on side.
func (r usageRoots) errorCase(errorCase *ir.ErrorCase, side UsageFlags) {
	r.payload(errorCase.Payload, side|UsageError)
	r.headers(errorCase.Headers, side|UsageError)
}

// stream records the event and initial-message types of streamDetail, when
// there is one.
func (r usageRoots) stream(streamDetail *ir.StreamDetail, flags UsageFlags) {
	if streamDetail == nil {
		return
	}
	r.ref(streamDetail.Events, flags)
	r.ref(streamDetail.Initial, flags)
}

// params records the type of each parameter in ps, and the type its encoding
// says its value holds once decoded.
func (r usageRoots) params(ps []ir.Parameter, flags UsageFlags) {
	for i := range ps {
		r.ref(&ps[i].Type, flags)
		if encoding := ps[i].Encoding; encoding != nil {
			r.ref(encoding.Schema, flags)
		}
	}
}

// headers records the type of each header in hs, and the type its encoding
// says its value holds once decoded. A header holds no field arguments, so a
// header's Args are not read.
func (r usageRoots) headers(hs []ir.Header, flags UsageFlags) {
	for i := range hs {
		r.ref(&hs[i].Type, flags)
		if encoding := hs[i].Encoding; encoding != nil {
			r.ref(encoding.Schema, flags)
		}
	}
}

// payload records the types of each content of payload, when there is a
// payload.
func (r usageRoots) payload(payload *ir.Payload, flags UsageFlags) {
	if payload == nil {
		return
	}
	for i := range payload.Contents {
		r.content(&payload.Contents[i], flags)
	}
}

// content records a content's type, its stream item, its file contents, the
// holders of its catch-alls, which are among its type's shapes, and the headers
// of its parts. A multipart media type also marks the content's own type, and
// only that one.
func (r usageRoots) content(content *ir.Content, flags UsageFlags) {
	r.ref(&content.Type, flags)
	if strings.HasPrefix(strings.ToLower(content.MediaType), multipartPrefix) {
		r.ref(&content.Type, UsageMultipart)
	}
	r.ref(content.Item, flags)
	if content.File != nil {
		r.ref(content.File.Contents, flags)
	}
	if content.ItemEncoding != nil {
		r.headers(content.ItemEncoding.Headers, flags)
	}
	for _, part := range content.Encoding {
		r.headers(part.Headers, flags)
	}
	for holder, parts := range content.CatchAllEncoding {
		r.ref(&ir.TypeRef{Target: holder}, flags)
		if parts.Value != nil {
			r.headers(parts.Value.Headers, flags)
		}
		for _, part := range parts.Patterns {
			r.headers(part.Headers, flags)
		}
	}
}

// ref records the target of ref, if there is a reference with a target.
func (r usageRoots) ref(ref *ir.TypeRef, flags UsageFlags) {
	if ref != nil && ref.Target != "" {
		r[ref.Target] |= flags
	}
}
