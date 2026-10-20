package ir

import (
	"slices"
	"strings"
)

// UsageFlags is a bitset recording how a type is used across the API surface,
// from the client's side of the wire. [Facts.Usage] derives it from the
// operations and nothing stores it: no type carries one, so the bits cannot
// disagree with the operations they describe.
type UsageFlags uint32

// Usage bits.
const (
	// UsageInput marks a type the client writes: reachable from a request's
	// parameters, body or stream, or from a webhook's response.
	UsageInput UsageFlags = 1 << iota
	// UsageOutput marks a type the client reads: reachable from a response's
	// body, headers or stream, or from a webhook's request.
	UsageOutput
	// UsageError marks a type the client reads as an error: reachable from the
	// body or headers of an error case, or of a service's common errors.
	UsageError
	// UsageMultipart marks the type of a multipart body itself. It does not
	// reach the types nested in it, which travel as ordinary encoded parts.
	UsageMultipart
)

// propagatedUsage are the bits that spread from the types an operation names to
// the types they contain. UsageMultipart stays on the body's own type.
var propagatedUsage = [...]UsageFlags{UsageInput, UsageOutput, UsageError}

// multipartPrefix starts the media type of every multipart body.
const multipartPrefix = "multipart/"

// Usage returns how the type id is used across the API surface: 0 for a type
// no operation reaches and for an id the document does not declare.
//
// The roots are each operation's parameters, request, responses, streams and
// errors, and every service's common errors; a webhook's request and response
// swap sides. A flag spreads from a root to the types it contains, and a
// reached model with a discriminator, or an abstract one, reaches its
// [Facts.Subtypes] too. Channels and messages are not roots, as no compiler
// lowers them yet, and neither is an operation [ForEachOperation] does not
// visit, nested past [MaxGroupDepth].
func (f *Facts) Usage(id TypeID) UsageFlags {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usageIndex()[id]
}

// usageIndex returns the usage of every type, derived once for the document.
func (f *Facts) usageIndex() map[TypeID]UsageFlags {
	if f.usage != nil {
		return f.usage
	}
	roots := usageRoots{}
	roots.document(f.doc)
	usage := make(map[TypeID]UsageFlags, len(roots))
	for _, flag := range propagatedUsage {
		f.spread(usage, roots, flag)
	}
	for id, flags := range roots {
		if _, declared := f.types()[id]; declared && flags&UsageMultipart != 0 {
			usage[id] |= UsageMultipart
		}
	}
	f.usage = usage
	return usage
}

// spread flags with flag every type reachable from the roots carrying it. A
// type is expanded once per flag, so a cycle ends. Types the document does not
// declare are not flagged.
func (f *Facts) spread(usage map[TypeID]UsageFlags, roots usageRoots, flag UsageFlags) {
	var pending []TypeID
	for id, flags := range roots {
		if flags&flag != 0 {
			pending = append(pending, id)
		}
	}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		td, declared := f.types()[id]
		if !declared || usage[id]&flag != 0 {
			continue
		}
		usage[id] |= flag
		pending = appendContained(pending, td)
		if isPolymorphicBase(td) {
			pending = append(pending, f.descendants(id)...)
		}
	}
}

// isPolymorphicBase reports whether a value typed by td may be any of its
// subtypes: a model that declares a discriminator, or is abstract. A plain
// base names a value of exactly its own shape.
func isPolymorphicBase(td TypeDef) bool {
	m, ok := td.(*Model)
	return ok && m != nil && (m.Discriminator != nil || m.Abstract)
}

// usageRoots maps each type an operation names to the usage bits of the sides
// that name it.
type usageRoots map[TypeID]UsageFlags

// document records the roots of every operation and common error of doc.
func (r usageRoots) document(doc *Document) {
	if doc == nil {
		return
	}
	ForEachOperation(doc, func(op Operation) { r.operation(&op) })
	for i := range doc.Services {
		for j := range doc.Services[i].CommonErrors {
			r.errorCase(&doc.Services[i].CommonErrors[j])
		}
	}
}

// operation records what op sends and receives. A webhook's request is read by
// the client and its response written by it, so the two sides swap.
func (r usageRoots) operation(op *Operation) {
	request, response := UsageInput, UsageOutput
	if isWebhook(op) {
		request, response = response, request
	}
	for i := range op.Params {
		r.ref(&op.Params[i].Type, request)
	}
	r.payload(op.Request, request)
	r.stream(op.RequestStream, request)
	for i := range op.Responses {
		r.payload(op.Responses[i].Payload, response)
		r.headers(op.Responses[i].Headers, response)
	}
	r.stream(op.ResponseStream, response)
	for i := range op.Errors {
		r.errorCase(&op.Errors[i])
	}
}

// isWebhook reports whether op is an inbound webhook. The binding's IsWebhook
// says so; its WebhookName may be empty, since an empty webhooks key is legal.
func isWebhook(op *Operation) bool {
	return slices.ContainsFunc(op.Bindings.HTTP, func(b HTTPBinding) bool { return b.IsWebhook })
}

// errorCase records the payload and headers of e as errors.
func (r usageRoots) errorCase(e *ErrorCase) {
	r.payload(e.Payload, UsageError)
	r.headers(e.Headers, UsageError)
}

// stream records the event and initial-message types of s, when there is one.
func (r usageRoots) stream(s *StreamDetail, flag UsageFlags) {
	if s == nil {
		return
	}
	r.ref(s.Events, flag)
	r.ref(s.Initial, flag)
}

// headers records the type of each header in hs.
func (r usageRoots) headers(hs []Property, flag UsageFlags) {
	for i := range hs {
		r.ref(&hs[i].Type, flag)
	}
}

// payload records the types of each content of p, when there is a payload.
func (r usageRoots) payload(p *Payload, flag UsageFlags) {
	if p == nil {
		return
	}
	for i := range p.Contents {
		r.content(&p.Contents[i], flag)
	}
}

// content records a content's type, its stream item, its file contents and the
// headers of its parts. A multipart media type also marks the content's own
// type, and only that one.
func (r usageRoots) content(c *Content, flag UsageFlags) {
	r.ref(&c.Type, flag)
	if strings.HasPrefix(strings.ToLower(c.MediaType), multipartPrefix) {
		r.ref(&c.Type, UsageMultipart)
	}
	r.ref(c.Item, flag)
	if c.File != nil {
		r.ref(c.File.Contents, flag)
	}
	if c.ItemEncoding != nil {
		r.headers(c.ItemEncoding.Headers, flag)
	}
	for _, part := range c.Encoding {
		r.headers(part.Headers, flag)
	}
}

// ref records the target of ref, if there is a reference with a target.
func (r usageRoots) ref(ref *TypeRef, flag UsageFlags) {
	if ref != nil && ref.Target != "" {
		r[ref.Target] |= flag
	}
}
