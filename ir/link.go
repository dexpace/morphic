// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// OperationLink says how a response leads to another request: the operation
// a caller can call next, and where each value that request needs comes from
// (docs/ir/responses.md#links). It is a fact about the exchange the response
// ends, so a Response or an ErrorCase carries it.
type OperationLink struct {
	// Name is how the source names the link, which a follow helper is named
	// from.
	Name Naming `json:"name"`
	// Operation is the operation the link leads to.
	Operation OpID `json:"operation"`
	// Params are the values the link gives Operation's parameters, in source
	// order, each parameter once. One it gives nothing is the caller's to pass.
	Params []LinkParam `json:"params,omitempty"`
	// Body is the value the link gives Operation's request body; nil = none.
	Body *LinkValue `json:"body,omitzero"`
	// Server is the server Operation is called on when the link names one;
	// nil = Operation's own.
	Server *ServerID `json:"server,omitzero"`
	// Docs is the link's documentation.
	Docs Docs `json:"docs"`
	// Provenance is where the link is declared.
	Provenance Provenance `json:"provenance"`
	// Unmodeled holds what the source writes on the link that the IR does not
	// model, kept verbatim.
	Unmodeled Unmodeled `json:"unmodeled,omitempty"`
}

// LinkParam is the value a link gives one parameter of the operation it leads
// to.
type LinkParam struct {
	// Param is a parameter of the link's Operation.
	Param ParamID `json:"param"`
	// Value is what the parameter is given.
	Value LinkValue `json:"value"`
}

// LinkValue is one value a link passes on: a constant, or a value of the
// exchange the response ends. Kind says which, and only the field it names is
// set, as a Value's kind selects its payload.
type LinkValue struct {
	// Kind says where the value comes from.
	Kind LinkValueKind `json:"kind"`
	// Constant is the value itself, set when Kind is LinkConstant.
	Constant *Value `json:"constant,omitzero"`
	// Request is a value of the request the response answers, a parameter of
	// the operation carrying the response or its request body, set when Kind is
	// LinkRequest.
	Request *InputPath `json:"request,omitzero"`
	// Response is a value of the response, a header or its body, set when
	// Kind is LinkResponse. Its Root is nil: the path roots in the response
	// carrying the link.
	Response *PropPath `json:"response,omitzero"`
}

// LinkValueKind says where a LinkValue comes from.
type LinkValueKind string

// The places a link reads a value from.
const (
	// LinkConstant is a value the link states.
	LinkConstant LinkValueKind = "constant"
	// LinkRequest is a value of the request the response answers.
	LinkRequest LinkValueKind = "request"
	// LinkResponse is a value of the response.
	LinkResponse LinkValueKind = "response"
	// LinkURL is the URL the request was sent to.
	LinkURL LinkValueKind = "url"
	// LinkMethod is the method the request was sent with.
	LinkMethod LinkValueKind = "method"
	// LinkStatus is the response's status code.
	LinkStatus LinkValueKind = "status"
)
