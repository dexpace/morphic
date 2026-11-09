// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// seedLayer offers the keys of the service layer's named entities: each
// service, group and operation, and each auth scheme. An operation another one
// declares as a callback, with no name of its own, is keyed through the
// declaring operation instead.
func (c *correlator) seedLayer() {
	callbacks := callbackOperations(c.doc)
	ops := c.seedGroups()
	for i := range c.doc.Services {
		svc := &c.doc.Services[i]
		c.offer(named("service", svc.Name), string(svc.ID), func(c *correlator, key string) {
			c.errorCases(key, svc.CommonErrors)
			c.servers(key, svc.Servers)
		})
	}
	for _, op := range ops {
		if !callbacks[op.ID] || op.Name.Source != "" {
			c.offerOperation(operationKey(op), op)
		}
	}
	for id, scheme := range c.doc.Auth {
		credentials := scheme.Credentials
		c.offer(named("auth", scheme.Name), string(id), func(c *correlator, key string) {
			c.ref(part(key, "credentials"), credentials)
		})
	}
}

// named is the segment of kind naming what n's source declares, and "" when
// the source declares no name, which keys nothing.
func named(kind string, n ir.Naming) string {
	if n.Source == "" {
		return ""
	}
	return segment(kind, n.Source)
}

// seedGroups offers each group's key, its ID, which a rule mints from a name
// and never from a position, and returns the operations the groups hold, each
// once, in declaration order. It walks the groups ir.ForEachGroup reaches.
func (c *correlator) seedGroups() []*ir.Operation {
	var ops []*ir.Operation
	ir.ForEachGroup(c.doc, func(g ir.OperationGroup) {
		c.offer(segment("group", string(g.ID)), string(g.ID), nil)
		for i := range g.Operations {
			op := &g.Operations[i]
			if _, seen := c.ops[op.ID]; !seen {
				c.ops[op.ID] = op
				ops = append(ops, op)
			}
		}
	})
	return ops
}

// operationKey is an operation's own key: its declared name, else its method
// and route with each variable written {}, a webhook's event name standing for
// the route. An operation with neither has none.
func operationKey(op *ir.Operation) string {
	if op.Name.Source != "" {
		return segment("op", op.Name.Source)
	}
	if len(op.Bindings.HTTP) == 0 {
		return ""
	}
	b := &op.Bindings.HTTP[0]
	if b.IsWebhook {
		return segment("webhook", b.Method+" "+b.WebhookName)
	}
	return segment("route", b.Method+" "+eraseVariables(b.URITemplate))
}

// eraseVariables writes each expression of an RFC 6570 template as {}, so a
// route keeps its key when a variable is renamed.
func eraseVariables(template string) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(template[open:], '}')
		if end < 0 {
			break
		}
		b.WriteString(template[:open])
		b.WriteString("{}")
		template = template[open+end+1:]
	}
	b.WriteString(template)
	return b.String()
}

// offerOperation offers key to op, and, when op takes it, keys what op names.
func (c *correlator) offerOperation(key string, op *ir.Operation) {
	c.offer(key, string(op.ID), func(c *correlator, key string) { c.operation(key, op) })
}

// operation offers the keys of what op names: its parameters, request,
// responses, errors, streams, long-running types and servers, and the
// operations it declares as callbacks.
func (c *correlator) operation(key string, op *ir.Operation) {
	c.params(key, op)
	c.payload(part(key, "request"), op.Request)
	c.stream(part(key, "requestStream"), op.RequestStream)
	if rpc := op.Bindings.RPC; rpc != nil {
		c.ref(part(key, "input"), rpc.InputType)
	}
	for i := range op.Responses {
		r := &op.Responses[i]
		at := child(key, "response", conditions(r.Conditions))
		c.payload(at, r.Payload)
		c.headers(at, r.Headers)
	}
	if def := op.Default; def != nil {
		c.payload(part(key, "default"), def.Payload)
		c.headers(part(key, "default"), def.Headers)
	}
	c.stream(part(key, "responseStream"), op.ResponseStream)
	if lro := op.LongRunning; lro != nil {
		c.ref(part(key, "polling"), lro.PollingType)
		c.ref(part(key, "final"), lro.FinalType)
	}
	c.errorCases(key, op.Errors)
	c.servers(key, op.Servers)
	c.callbacks(key, op)
}

// callbacks offers each unnamed operation op declares as a callback its key:
// op's, then the method and the runtime expression keying it.
func (c *correlator) callbacks(key string, op *ir.Operation) {
	for _, b := range op.Bindings.HTTP {
		for _, cb := range b.Callbacks {
			for _, id := range cb.Operations {
				target, ok := c.ops[id]
				if !ok || target.Name.Source != "" || len(target.Bindings.HTTP) == 0 {
					continue
				}
				method := target.Bindings.HTTP[0].Method
				c.offerOperation(child(key, "callback", method+" "+cb.Expression), target)
			}
		}
	}
}

// params offers each parameter of op its key: op's, then where the parameter
// travels and its wire name, a header's in lower case, or its name where no
// binding places it. A parameter offers its type the key of its schema.
func (c *correlator) params(key string, op *ir.Operation) {
	for i := range op.Params {
		p := &op.Params[i]
		c.offer(paramKey(key, op, p), string(p.ID), func(c *correlator, key string) {
			c.ref(part(key, "schema"), &p.Type)
		})
	}
}

// paramKey is the key of p, a parameter of op keyed key.
func paramKey(key string, op *ir.Operation, p *ir.Parameter) string {
	for _, b := range op.Bindings.HTTP {
		for _, pb := range b.ParamBindings {
			if pb.Param != p.ID || pb.Location == ir.HTTPLocationHost {
				continue
			}
			wire := pb.WireName
			if wire == "" {
				wire = p.Name.Source
			}
			if pb.Location == ir.HTTPLocationHeader {
				wire = strings.ToLower(wire)
			}
			return child(key, "param", string(pb.Location)+" "+wire)
		}
	}
	if p.Name.Source == "" {
		return ""
	}
	return child(key, "arg", p.Name.Source)
}

// conditions spells the status ranges a response applies to, each as its code
// or its bounds, "" when it applies whatever the status.
func conditions(rc ir.ResponseConditions) string {
	spelled := make([]string, 0, len(rc.StatusCodes))
	for _, r := range rc.StatusCodes {
		if r.From == r.To {
			spelled = append(spelled, strconv.Itoa(r.From))
			continue
		}
		spelled = append(spelled, strconv.Itoa(r.From)+"-"+strconv.Itoa(r.To))
	}
	return strings.Join(spelled, ",")
}

// errorCases offers the keys of the types each error case of es names, under
// key, by the case's conditions.
func (c *correlator) errorCases(key string, es []ir.ErrorCase) {
	for i := range es {
		at := child(key, "error", conditions(es[i].Conditions))
		c.payload(at, es[i].Payload)
		c.headers(at, es[i].Headers)
	}
}

// servers offers each server ids names the key of the scope naming it, key,
// then its URL template.
func (c *correlator) servers(key string, ids []ir.ServerID) {
	for _, id := range ids {
		if s, ok := c.doc.Servers[id]; ok {
			c.offer(child(key, "server", s.URLTemplate), string(id), nil)
		}
	}
}

// payload offers the keys of what each content of p names, by media type.
func (c *correlator) payload(key string, p *ir.Payload) {
	if p == nil {
		return
	}
	for i := range p.Contents {
		ct := &p.Contents[i]
		at := child(key, "content", ct.MediaType)
		c.ref(at, &ct.Type)
		c.ref(part(at, "item"), ct.Item)
		if ct.File != nil {
			c.ref(part(at, "file"), ct.File.Contents)
		}
		if ct.ItemEncoding != nil {
			c.headers(part(at, "item"), ct.ItemEncoding.Headers)
		}
	}
}

// stream offers the keys of a stream's event and initial-message types.
func (c *correlator) stream(key string, s *ir.StreamDetail) {
	if s == nil {
		return
	}
	c.ref(part(key, "events"), s.Events)
	c.ref(part(key, "initial"), s.Initial)
}

// headers offers each header of hs its key, key then the header's name in
// lower case, and, when it takes it, keys its type and what its encoding
// decodes to.
func (c *correlator) headers(key string, hs []ir.Header) {
	for i := range hs {
		h := &hs[i].Property
		c.offer(child(key, "header", strings.ToLower(h.EffectiveWireName())), string(h.ID), func(c *correlator, key string) {
			c.ref(part(key, "type"), &h.Type)
			c.decoded(key, h.Encoding)
		})
	}
}
