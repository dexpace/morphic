// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import "github.com/dexpace/morphic/compilers/openapi/internal/oas"

// kindStates names the state each object of the rule table is read at; a kind
// it leaves out is read as an object the binder holds no state for (sObject).
var kindStates = map[oas.Kind]state{
	oas.Document: sDocument, oas.Components: sComponents, oas.Paths: sPaths, oas.PathItem: sPathItem,
	oas.Operation: sOperation, oas.Parameter: sParameter, oas.RequestBody: sRequestBody,
	oas.MediaType: sMediaType, oas.Encoding: sEncoding, oas.Responses: sResponses, oas.Response: sResponse,
	oas.Callback: sCallback, oas.Example: sExample, oas.Link: sLink, oas.Header: sHeader, oas.Schema: sSchema,
	oas.SecurityScheme: sSecurityScheme, oas.SecurityRequirement: sSecurityRequirement,
}

// The containers of each state, keyed by what they hold: a map of it, and a
// list of it.
var (
	mapsOf = map[state]state{
		sSchema: sSchemaMap, sPathItem: sPathItemMap, sOperation: sOperationMap, sParameter: sParameterMap,
		sHeader: sHeaderMap, sRequestBody: sRequestBodyMap, sResponse: sResponseMap, sMediaType: sMediaTypeMap,
		sEncoding: sEncodingMap, sCallback: sCallbackMap, sExample: sExampleMap, sLink: sLinkMap,
		sSecurityScheme: sSecuritySchemeMap, sRaw: sRawEntries,
	}
	listsOf = map[state]state{
		sSchema: sSchemaList, sParameter: sParameterList, sEncoding: sEncodingList,
		sSecurityRequirement: sSecurityList, sRaw: sRawEntries,
	}
)

// objectFields maps the fields each object of spec defines, at any minor, to
// the state each leads to (leads), for every object state but a container's,
// whose keys are entries, and sSchema's, whose keywords are its dialect's
// (schemaFields). Every kind kindStates names is an object of the table
// (TestObjectFields_ReadsEveryObjectOfTheTable).
func objectFields(spec oas.Spec) map[state]map[string]state {
	out := make(map[state]map[string]state, len(kindStates))
	for kind, s := range kindStates {
		if _, container := entries[s]; container || s == sSchema {
			continue
		}
		o, _ := spec.Object(kind)
		fs := make(map[string]state, len(o.Fields))
		for _, f := range o.Fields {
			fs[f.Name] = leads(f.Shape, f.Of)
		}
		out[s] = fs
	}
	return out
}

// leads is the state a field of shape, holding objects of kind of where its
// shape holds objects, leads to: raw YAML for an unchecked value, a value for
// a scalar, an object for strings, and for an object of a kind the binder
// reads, that kind's state or the map or list of it.
func leads(shape oas.Shape, of oas.Kind) state {
	switch shape {
	case oas.Any:
		return sRaw
	case oas.String, oas.Boolean, oas.Number, oas.PositiveNumber, oas.Natural:
		return sValue
	case oas.RawMap, oas.RawList:
		return sRawEntries
	case oas.Nested:
		if s, ok := kindStates[of]; ok {
			return s
		}
	case oas.Map:
		return containerOf(mapsOf, of)
	case oas.List:
		return containerOf(listsOf, of)
	}
	return sObject
}

// containerOf is the container state in gives for the state an object of kind
// of is read at, or sObject where the binder reads no such container.
func containerOf(in map[state]state, of oas.Kind) state {
	if s, ok := kindStates[of]; ok {
		if c, ok := in[s]; ok {
			return c
		}
	}
	return sObject
}
