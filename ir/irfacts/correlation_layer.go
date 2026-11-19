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
// declares as a callback is keyed through the declaring operation instead.
func (c *correlator) seedLayer() {
	operations := c.seedGroups()
	for i := range c.doc.Services {
		svc := &c.doc.Services[i]
		c.offer(named("service", svc.Name), string(svc.ID), func(correlatorValue *correlator, key string) {
			correlatorValue.errorCases(key, svc.CommonErrors)
			correlatorValue.servers(key, svc.Servers)
		})
	}
	c.partHeaders = indexPartHeaders(c.doc.Services, operations)
	callbacks := callbackOperations(c.doc)
	routes := map[string]int{}
	for _, operation := range operations {
		if !callbacks[operation.ID] {
			routes[routeKey(operation)]++
		}
	}
	for _, operation := range operations {
		if !callbacks[operation.ID] {
			c.offerOperation(operationKey(operation, routes), operation)
		}
	}
	for id, scheme := range c.doc.Auth {
		credentials := scheme.Credentials
		c.offer(named("auth", scheme.Name), string(id), func(correlatorValue *correlator, key string) {
			correlatorValue.ref(part(key, "credentials"), credentials)
		})
	}
}

// named is the segment of kind naming what naming's source declares, and ""
// when the source declares no name, which keys nothing.
func named(kind string, naming ir.Naming) string {
	if naming.Source == "" {
		return ""
	}
	return segment(kind, naming.Source)
}

// seedGroups offers each group's key, its ID, which a rule mints from a name
// and never from a position, and returns the operations the groups hold, each
// once, in declaration order. It walks the groups ir.ForEachGroup reaches, and
// DocumentFacts.Partial says when that is not all of them.
func (c *correlator) seedGroups() []*ir.Operation {
	var operations []*ir.Operation
	ir.ForEachGroup(c.doc, func(operationGroup ir.OperationGroup) {
		c.offer(segment("group", string(operationGroup.ID)), string(operationGroup.ID), nil)
		for i := range operationGroup.Operations {
			operation := &operationGroup.Operations[i]
			if _, seen := c.operations[operation.ID]; !seen {
				c.operations[operation.ID] = operation
				operations = append(operations, operation)
			}
		}
	})
	return operations
}

// operationKey is operation's own key: its route, unless no HTTP binding places operation or
// another operation shares the route, as routes counts them, and then its
// declared name. An operation with neither has none.
func operationKey(operation *ir.Operation, routes map[string]int) string {
	if route := routeKey(operation); route != "" && routes[route] == 1 {
		return route
	}
	return named("op", operation.Name)
}

// routeKey is operation's method and route, each variable written {}, a webhook's
// event name standing for the route, and "" when no HTTP binding places operation.
func routeKey(operation *ir.Operation) string {
	if len(operation.Bindings.HTTP) == 0 {
		return ""
	}
	b := &operation.Bindings.HTTP[0]
	if b.Inbound == ir.HTTPInboundWebhook {
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

// offerOperation offers key to operation, and, when operation takes it, keys what operation names.
func (c *correlator) offerOperation(key string, operation *ir.Operation) {
	c.offer(key, string(operation.ID), func(correlatorValue *correlator, key string) { correlatorValue.operation(key, operation) })
}

// operation offers the keys of what operation names: its parameters, request,
// responses, errors, streams, long-running types and servers, and the
// operations it declares as callbacks.
func (c *correlator) operation(key string, operation *ir.Operation) {
	c.params(key, operation)
	c.payload(part(key, "request"), operation.Request)
	c.stream(part(key, "requestStream"), operation.RequestStream)
	if rpc := operation.Bindings.RPC; rpc != nil {
		c.ref(part(key, "input"), rpc.InputType)
	}
	for i := range operation.Responses {
		response := &operation.Responses[i]
		at := child(key, "response", conditions(response.Conditions))
		c.payload(at, response.Payload)
		c.headers(at, response.Headers)
		c.linkServers(at, response.Links)
	}
	if def := operation.Default; def != nil {
		c.payload(part(key, "default"), def.Payload)
		c.headers(part(key, "default"), def.Headers)
		c.linkServers(part(key, "default"), def.Links)
	}
	c.stream(part(key, "responseStream"), operation.ResponseStream)
	if lro := operation.LongRunning; lro != nil {
		c.ref(part(key, "polling"), lro.PollingType)
		c.ref(part(key, "final"), lro.FinalType)
	}
	c.errorCases(key, operation.Errors)
	c.servers(key, operation.Servers)
	c.callbacks(key, operation)
}

// callbacks offers each operation operation declares as a callback its key: operation's,
// then the callback's name and the method, whether or not the operation has a
// name of its own. The runtime expression is a detail of where the request
// goes, so a rewritten one keeps the key, unless the name keys two operations
// of one method, which only their expressions tell apart.
func (c *correlator) callbacks(key string, operation *ir.Operation) {
	targets := c.callbackTargets(operation)
	shared := make(map[[2]string]int, len(targets))
	for _, callbackTarget := range targets {
		shared[[2]string{callbackTarget.name, callbackTarget.method}]++
	}
	for _, callbackTarget := range targets {
		at := child(key, "callback", callbackTarget.name)
		if shared[[2]string{callbackTarget.name, callbackTarget.method}] > 1 {
			at = child(at, "expression", callbackTarget.expression)
		}
		c.offerOperation(child(at, "method", callbackTarget.method), callbackTarget.operation)
	}
}

// callbackTarget is an operation one of op's callbacks lists, with what keys
// it: the callback's name, its runtime expression and the operation's method.
type callbackTarget struct {
	operation                *ir.Operation
	name, expression, method string
}

// callbackTargets returns the operations operation's callbacks list that the document
// declares with an HTTP binding, in the order operation lists them.
func (c *correlator) callbackTargets(operation *ir.Operation) []callbackTarget {
	var targets []callbackTarget
	for _, b := range operation.Bindings.HTTP {
		for _, cb := range b.Callbacks {
			for _, id := range cb.Operations {
				target, ok := c.operations[id]
				if !ok || len(target.Bindings.HTTP) == 0 {
					continue
				}
				targets = append(targets, callbackTarget{target, cb.Name, cb.Expression, target.Bindings.HTTP[0].Method})
			}
		}
	}
	return targets
}

// params offers each parameter of operation its key: operation's, then where the parameter
// travels and its wire name, or its prefix under one, a header's in lower
// case, or its name where no binding places it. A parameter offers its type the
// key of its schema.
func (c *correlator) params(key string, operation *ir.Operation) {
	for i := range operation.Params {
		parameter := &operation.Params[i]
		c.offer(paramKey(key, operation, parameter), string(parameter.ID), func(correlatorValue *correlator, key string) {
			correlatorValue.ref(part(key, "schema"), &parameter.Type)
			correlatorValue.decoded(key, parameter.Encoding)
		})
	}
}

// paramKey is the key of parameter, a parameter of operation keyed key. A host binding
// is passed over: it fills a host-prefix label in addition to the binding at
// the parameter's modeled location (docs/ir/http-binding.md), which places it.
// A binding under a prefix names no field of its own, so it is keyed by where
// it travels and its prefix, in a segment of another kind than a wire name's
// (docs/ir/http-binding.md#prefix-bindings).
func paramKey(key string, operation *ir.Operation, parameter *ir.Parameter) string {
	for _, b := range operation.Bindings.HTTP {
		for _, pb := range b.ParamBindings {
			if pb.Param != parameter.ID || pb.Location == ir.HTTPLocationHost {
				continue
			}
			if pb.Prefix != nil {
				return child(part(key, string(pb.Location)), "prefix", wireCase(pb.Location, *pb.Prefix))
			}
			wire := pb.WireName
			if wire == "" {
				wire = parameter.Name.Source
			}
			return child(key, "param", string(pb.Location)+" "+wireCase(pb.Location, wire))
		}
	}
	if parameter.Name.Source == "" {
		return ""
	}
	return child(key, "arg", parameter.Name.Source)
}

// wireCase is name as a key compares it at location: a header's in lower case,
// as HTTP compares a field name (RFC 9110 §5.1), and any other as written.
func wireCase(location ir.HTTPLocation, name string) string {
	if location == ir.HTTPLocationHeader {
		return strings.ToLower(name)
	}
	return name
}

// conditions spells the status ranges a response applies to, each as its code
// or its bounds, "" when it applies whatever the status.
func conditions(rc ir.ResponseConditions) string {
	spelled := make([]string, 0, len(rc.StatusCodes))
	for _, statusRange := range rc.StatusCodes {
		if statusRange.From == statusRange.To {
			spelled = append(spelled, strconv.Itoa(statusRange.From))
			continue
		}
		spelled = append(spelled, strconv.Itoa(statusRange.From)+"-"+strconv.Itoa(statusRange.To))
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
		c.linkServers(at, es[i].Links)
	}
}

// servers offers each server ids names the key of the scope naming it, key,
// then its URL template.
func (c *correlator) servers(key string, ids []ir.ServerID) {
	for _, id := range ids {
		if server, ok := c.doc.Servers[id]; ok {
			c.offer(child(key, "server", server.URLTemplate), string(id), nil)
		}
	}
}

// linkServers offers each server a link of links names the key of the response
// carrying the link, key, then the link's name and the server's URL template.
// A link names no other entity it does not reach through an ID keyed
// elsewhere.
func (c *correlator) linkServers(key string, links []ir.OperationLink) {
	for i := range links {
		if serverID := links[i].Server; serverID != nil {
			c.servers(child(key, "link", links[i].Name.Source), []ir.ServerID{*serverID})
		}
	}
}

// payload offers the keys of what each content of payload names, by media type.
func (c *correlator) payload(key string, payload *ir.Payload) {
	if payload == nil {
		return
	}
	for i := range payload.Contents {
		ct := &payload.Contents[i]
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
func (c *correlator) stream(key string, streamDetail *ir.StreamDetail) {
	if streamDetail == nil {
		return
	}
	c.ref(part(key, "events"), streamDetail.Events)
	c.ref(part(key, "initial"), streamDetail.Initial)
}

// indexPartHeaders indexes the headers each multipart part declares by the property
// the part encodes, over every payload of operations and services.
func indexPartHeaders(services []ir.Service, operations []*ir.Operation) map[ir.PropertyID][]ir.Header {
	parts := map[ir.PropertyID][]ir.Header{}
	add := func(payload *ir.Payload) {
		if payload == nil {
			return
		}
		for i := range payload.Contents {
			for property, enc := range payload.Contents[i].Encoding {
				parts[property] = append(parts[property], enc.Headers...)
			}
		}
	}
	addErrors := func(es []ir.ErrorCase) {
		for i := range es {
			add(es[i].Payload)
		}
	}
	for i := range services {
		addErrors(services[i].CommonErrors)
	}
	for _, operation := range operations {
		add(operation.Request)
		for i := range operation.Responses {
			add(operation.Responses[i].Payload)
		}
		if operation.Default != nil {
			add(operation.Default.Payload)
		}
		addErrors(operation.Errors)
	}
	return parts
}

// headers offers each header of hs its key, key then the header's name in
// lower case, or its prefix for one under a prefix, and, when it takes it, keys
// its type and what its encoding decodes to.
func (c *correlator) headers(key string, hs []ir.Header) {
	for i := range hs {
		property := &hs[i].Property
		c.offer(headerKey(key, &hs[i]), string(property.ID), func(correlatorValue *correlator, key string) {
			correlatorValue.ref(part(key, "type"), &property.Type)
			correlatorValue.decoded(key, property.Encoding)
		})
	}
}

// headerKey extends key by what names header on the wire, in lower case: its
// name, or the prefix of one under a prefix, which names no field of its own
// (docs/ir/responses.md#prefix-headers). The two segments differ in kind, so a
// prefix and a header of the same name never share a key.
func headerKey(key string, header *ir.Header) string {
	if header.Prefix != nil {
		return child(key, "prefix", strings.ToLower(*header.Prefix))
	}
	return child(key, "header", strings.ToLower(header.EffectiveWireName()))
}
