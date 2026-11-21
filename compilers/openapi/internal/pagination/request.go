// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
)

// role is what a query parameter's name says it sends a page walk: where the
// walk continues under a strategy, or the page size.
type role struct {
	strategy        ir.PageStrategy
	carriesPageSize bool
}

// roleOf returns the role a canonical name gives a parameter, the zero role
// for a name giving none. It is the vocabulary
// docs/compilers/openapi.md#pagination-policy states, compared by canonical
// words, so `pageSize` and `page_size` are one name.
func roleOf(canonical string) role {
	switch canonical {
	case "page", "page_number":
		return role{strategy: ir.PageStrategyPage}
	case "offset":
		return role{strategy: ir.PageStrategyOffset}
	case "cursor":
		return role{strategy: ir.PageStrategyCursor}
	case "page_token", "next_token", "continuation_token":
		return role{strategy: ir.PageStrategyToken}
	case "per_page", "page_size", "limit":
		return role{carriesPageSize: true}
	default:
		return role{}
	}
}

// inputs are the places a walk sends: cursor carries where it continues
// under strategy, primitive being its primitive kind, and limit is the page
// size, nil where none is named. strict reports that the operation is a POST,
// whose page is read by the narrower rules a create cannot meet
// (pageKey.strict).
type inputs struct {
	strategy  ir.PageStrategy
	cursor    place
	primitive ir.PrimitiveKind
	limit     *place
	strict    bool
}

// readsNextCursor reports whether the walk sends a value a page hands back,
// rather than one it counts.
func (in inputs) readsNextCursor() bool {
	return in.strategy == ir.PageStrategyCursor || in.strategy == ir.PageStrategyToken
}

// requestInputs returns the inputs the places of operation's request name
// (docs/compilers/openapi.md#pagination-policy). named reports whether one
// names where a walk continues under what operation's method admits; where one
// does and they give no walk, why says what stops it: a method no walk is read
// for, two places naming a continuation or a page size, a POST failing a
// condition that tells it from a create, one of a type a walk cannot send, or a
// page sent as a string no number shows it counts.
func requestInputs(compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation) (in inputs, why string, named bool) {
	name, kind := methodOf(operation)
	continuations, limits := places(compiledDocumentView, operation, kind)
	switch {
	case len(continuations) == 0:
		return inputs{}, "", false
	case kind == methodOther:
		return inputs{}, fmt.Sprintf("the operation is a %s, and only a GET or a QUERY is safe to send again for "+
			"each page", name), true
	case kind == methodPost && !slices.ContainsFunc(continuations, func(placeValue place) bool { return placeValue.kind == inBody }):
		return inputs{}, postOutsideBody(continuations), true
	case len(continuations) > 1:
		return inputs{}, twoPlaces(continuations, "where a walk continues"), true
	case len(limits) > 1:
		return inputs{}, twoPlaces(limits, "the page size"), true
	}
	in = inputs{cursor: continuations[0], strategy: roleOf(continuations[0].canonical()).strategy,
		strict: kind == methodPost}
	if len(limits) == 1 {
		in.limit = &limits[0]
	}
	if in.strict {
		if why := postRequest(in); why != "" {
			return inputs{}, why, true
		}
	}
	if why := sendsItsInputs(compiledDocumentView, &in); why != "" {
		return inputs{}, why, true
	}
	if in.strict {
		if why := postAnswers(operation); why != "" {
			return inputs{}, why, true
		}
	}
	return in, "", true
}

// sendsItsInputs reads the primitive of in's cursor into in, and returns why
// in's places are no values a walk sends: a type it cannot send, or a page
// sent as a string no number shows it counts.
func sendsItsInputs(compiledDocumentView *compileddoc.CompiledDocumentView, in *inputs) string {
	primitive, ok := sendable(compiledDocumentView, in.cursor.typeID())
	if !ok {
		return notSendable(in.cursor, "where a walk continues")
	}
	in.primitive = primitive
	if in.strategy == ir.PageStrategyPage && primitive == ir.PrimitiveString && !in.cursor.numbered() {
		return fmt.Sprintf("%s names the page as a string with no whole number for its default or an example, "+
			"so it may be a token", in.cursor)
	}
	if in.limit != nil {
		if _, ok := sendable(compiledDocumentView, in.limit.typeID()); !ok {
			return notSendable(*in.limit, "the page size")
		}
	}
	return ""
}

// queryPlaced returns where placements put a parameter in the query, as one
// parameter or as the query string whole, the first that does, and false
// where none does.
func queryPlaced(placements []compileddoc.Placement) (ir.HTTPLocation, bool) {
	for _, at := range placements {
		if at.Location == ir.HTTPLocationQuery || at.Location == ir.HTTPLocationQuerystring {
			return at.Location, true
		}
	}
	return "", false
}

// sendable returns the primitive the type id stands for, read through alias
// scalars, when a walk can send it or count with it (sendableAs).
func sendable(compiledDocumentView *compileddoc.CompiledDocumentView, id ir.TypeID) (ir.PrimitiveKind, bool) {
	return sendableAs(compiledDocumentView.Stands(id))
}

// sendableAs returns td's primitive when a walk can send it or count with it:
// a number of any kind, which a value of it holding no integer leaves none to
// count with, or a string, which carries a number as its decimal text.
func sendableAs(td ir.TypeDef) (ir.PrimitiveKind, bool) {
	primitive, ok := td.(*ir.Primitive)
	if !ok {
		return "", false
	}
	primitiveKind := primitive.PrimitiveKind
	if primitiveKind == ir.PrimitiveString || primitiveKind == ir.PrimitiveNumber || primitiveKind.StrictlyWithin(ir.PrimitiveNumber) {
		return primitiveKind, true
	}
	return "", false
}

// numbered reports whether def or an example is a whole number, as its value
// or its text. A page sent as a string is counted only so, since some APIs
// name an opaque page token `page` too.
func numbered(def *ir.Value, examples []ir.Example) bool {
	if def != nil && wholeNumber(*def) {
		return true
	}
	return slices.ContainsFunc(examples, func(example ir.Example) bool { return example.Value != nil && wholeNumber(*example.Value) })
}

// wholeNumber reports whether value is a whole number, or a string holding one.
func wholeNumber(value ir.Value) bool {
	text := value.String
	if value.Kind == ir.ValueNumber {
		text = string(value.Number)
	} else if value.Kind != ir.ValueString {
		return false
	}
	decimal, err := ir.ParseDecimal(text)
	return err == nil && decimal.IsInt()
}

// notSendable says that placeValue names what a walk sends as what, and is of a
// type it cannot send: not a plain number or string, as an enum, a constant or
// an encoded string is not, whatever values it holds.
func notSendable(placeValue place, what string) string {
	return fmt.Sprintf("%s names %s, and its type is no plain number or string", placeValue, what)
}

// placeKind is where in a request a place sits, in the order a finding lists
// places.
type placeKind int

// The kinds of place.
const (
	// inQuery is a query parameter.
	inQuery placeKind = iota
	// inParamMember is a member of an object query parameter.
	inParamMember
	// inBody is a member of the request body's root.
	inBody
)

// place is a value of a request a walk may send: a query parameter, param; a
// member of an object query parameter, param's member; or a member of the
// request body's root, member, which every content holds at segments.
type place struct {
	kind     placeKind
	param    *ir.Parameter
	member   *ir.Property
	segments []ir.PropertyID
}

// canonical is the canonical name giving p its role: its member's, else its
// parameter's.
func (p place) canonical() string {
	if p.member != nil {
		return p.member.Name.Canonical
	}
	return p.param.Name.Canonical
}

// typeID is the type of the value p sends.
func (p place) typeID() ir.TypeID {
	if p.member != nil {
		return p.member.Type.Target
	}
	return p.param.Type.Target
}

// numbered reports whether p's member or parameter shows a whole number for
// its default or an example (numbered).
func (p place) numbered() bool {
	if p.member != nil && numbered(p.member.Default, p.member.Examples) {
		return true
	}
	return p.param != nil && numbered(p.param.Default, p.param.Examples)
}

// input is the ir.InputPath addressing p.
func (p place) input() *ir.InputPath {
	switch p.kind {
	case inBody:
		return &ir.InputPath{Segments: slices.Clone(p.segments)}
	case inParamMember:
		id := p.param.ID
		return &ir.InputPath{Param: &id, Segments: []ir.PropertyID{p.member.ID}}
	default:
		id := p.param.ID
		return &ir.InputPath{Param: &id}
	}
}

// String names p as a finding does.
func (p place) String() string {
	switch p.kind {
	case inBody:
		return fmt.Sprintf("request body member %q", p.member.EffectiveWireName())
	case inParamMember:
		return fmt.Sprintf("member %q of query parameter %q", p.member.EffectiveWireName(), p.param.Name.Source)
	default:
		return fmt.Sprintf("query parameter %q", p.param.Name.Source)
	}
}

// methodKind is what an operation's method admits a walk to be read from.
type methodKind int

// The kinds of method.
const (
	// methodSafe is a GET or a QUERY, which a pager sends again for each page.
	methodSafe methodKind = iota
	// methodPost is a POST, read as a walk only where it is told from a create.
	methodPost
	// methodOther is any other method, never read as a walk.
	methodOther
)

// methodOf returns the method of operation's first HTTP binding that is no GET
// or QUERY, and what it admits.
func methodOf(operation *ir.Operation) (string, methodKind) {
	for _, b := range operation.Bindings.HTTP {
		switch b.Method {
		case http.MethodGet, methodQuery:
		case http.MethodPost:
			return b.Method, methodPost
		default:
			return b.Method, methodOther
		}
	}
	return "", methodSafe
}

// places returns the places of operation's request naming where a walk
// continues and those naming the page size, under what kind admits: any
// method's query parameters and members of object query parameters, and, but
// for a method that is no GET, QUERY or POST, the request body's members. A
// POST's body offset is no place: a body offset is often an identifier a create
// takes.
func places(compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation, kind methodKind) (continuations, limits []place) {
	continuations, limits = paramPlaces(compiledDocumentView, operation)
	if kind == methodOther || operation.Request == nil || len(operation.Request.Contents) == 0 {
		return continuations, limits
	}
	for _, property := range compiledDocumentView.Members(operation.Request.Contents[0].Type.Target) {
		roleValue := roleOf(property.Name.Canonical)
		if roleValue == (role{}) || kind == methodPost && roleValue.strategy == ir.PageStrategyOffset {
			continue
		}
		segments, _, ok := runtimeexpr.BodyPath(compiledDocumentView, operation.Request, []string{property.EffectiveWireName()}, "request")
		if !ok {
			continue // a member some content does not hold is no value every request sends
		}
		placeValue := place{kind: inBody, member: property, segments: segments}
		if roleValue.carriesPageSize {
			limits = append(limits, placeValue)
		} else {
			continuations = append(continuations, placeValue)
		}
	}
	return continuations, limits
}

// paramPlaces returns the places operation's query parameters name. An object
// parameter, bound in the query or as the query string and standing for a
// model, is read through its members, one level down, where one of them
// names a role, a continuation or a page size, its own name then giving it
// none; otherwise a parameter bound in the query is read by its own name.
func paramPlaces(compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation) (continuations, limits []place) {
	if len(operation.Params) == 0 {
		return nil, nil
	}
	ix := compiledDocumentView.Params(operation)
	for i := range operation.Params {
		parameter := &operation.Params[i]
		location, ok := queryPlaced(ix.Placements(parameter.ID))
		if !ok {
			continue
		}
		mc, ml := memberPlaces(compiledDocumentView, parameter)
		if len(mc)+len(ml) > 0 {
			continuations, limits = append(continuations, mc...), append(limits, ml...)
			continue
		}
		roleValue := roleOf(parameter.Name.Canonical)
		switch {
		case roleValue == (role{}) || location != ir.HTTPLocationQuery:
		case roleValue.carriesPageSize:
			limits = append(limits, place{kind: inQuery, param: parameter})
		default:
			continuations = append(continuations, place{kind: inQuery, param: parameter})
		}
	}
	return continuations, limits
}

// memberPlaces returns the places the members of parameter name, where
// parameter's type stands for a model, and none otherwise.
func memberPlaces(compiledDocumentView *compileddoc.CompiledDocumentView, parameter *ir.Parameter) (continuations, limits []place) {
	for _, property := range compiledDocumentView.Members(parameter.Type.Target) {
		switch roleValue := roleOf(property.Name.Canonical); {
		case roleValue == (role{}):
		case roleValue.carriesPageSize:
			limits = append(limits, place{kind: inParamMember, param: parameter, member: property})
		default:
			continuations = append(continuations, place{kind: inParamMember, param: parameter, member: property})
		}
	}
	return continuations, limits
}

// twoPlaces says that places, two or more, each name what.
func twoPlaces(places []place, what string) string {
	return fmt.Sprintf("%s each name %s", placesNamed(places), what)
}

// placesNamed names places for a finding, so it reads the same in any
// declaration order: query parameters alone by their names, sorted, and
// otherwise each place, in the order of their kinds.
func placesNamed(places []place) string {
	if len(places) == 1 {
		return places[0].String()
	}
	if !slices.ContainsFunc(places, func(placeValue place) bool { return placeValue.kind != inQuery }) {
		names := make([]string, len(places))
		for i, place := range places {
			names[i] = strconv.Quote(place.param.Name.Source)
		}
		return "query parameters " + listed(names)
	}
	slices.SortFunc(places, func(a, b place) int {
		return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.String(), b.String()))
	})
	names := make([]string, len(places))
	for i, place := range places {
		names[i] = place.String()
	}
	return joined(names)
}

// postOutsideBody says why a POST whose continuations, none a member of its
// request body, is no walk: a create taking its path item's page parameters
// names them in the query, and would be sent once per page.
func postOutsideBody(continuations []place) string {
	verb := "names"
	if len(continuations) > 1 {
		verb = "each name"
	}
	return fmt.Sprintf("the operation is a POST, and %s %s where its walk continues: a POST is read as a walk "+
		"only where its request body does, as one taking its path item's parameters may be a create",
		placesNamed(continuations), verb)
}

// postRequest returns why the places of a POST's walk, in, do not tell it
// from a create: a page walk naming no page size in the body beside its page.
// Where the walk continues is a member of the body (postOutsideBody).
func postRequest(in inputs) string {
	if in.strategy == ir.PageStrategyPage && (in.limit == nil || in.limit.kind != inBody) {
		return "the operation is a POST whose request body names the page and not the page size, which a POST " +
			"read as a walk names beside it"
	}
	return ""
}

// postAnswers returns why operation, a POST, answers as no walk does: a success
// response declaring a body under a status other than 200, as a 201 says a
// resource was created (RFC 9110 §15.3.2). The least such status is named, so
// the finding reads the same in any order.
func postAnswers(operation *ir.Operation) string {
	var other []string
	for i := range operation.Responses {
		response := &operation.Responses[i]
		if pageBody(response.Payload) != nil && !okOnly(response.Conditions) {
			other = append(other, statusOf(response.Conditions))
		}
	}
	if len(other) == 0 {
		return ""
	}
	return fmt.Sprintf("the operation is a POST answering %s with a body: a POST is read as a walk only where "+
		"every success response declaring one is a 200, as a 201 says a resource was created", slices.Min(other))
}

// okOnly reports whether responseConditions selects the status 200 alone.
func okOnly(responseConditions ir.ResponseConditions) bool {
	return len(responseConditions.StatusCodes) == 1 && responseConditions.StatusCodes[0] == ir.StatusRange{From: http.StatusOK, To: http.StatusOK}
}

// statusOf spells the statuses responseConditions selects as a finding names
// them: a code, or a range from its bounds.
func statusOf(responseConditions ir.ResponseConditions) string {
	spelled := make([]string, len(responseConditions.StatusCodes))
	for i, statusRange := range responseConditions.StatusCodes {
		spelled[i] = strconv.Itoa(statusRange.From)
		if statusRange.To != statusRange.From {
			spelled[i] += "-" + strconv.Itoa(statusRange.To)
		}
	}
	return strings.Join(spelled, ", ")
}
