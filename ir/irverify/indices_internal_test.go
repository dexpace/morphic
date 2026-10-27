package irverify

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// docWithServers builds a document declaring one server, so index 0 addresses it
// and every other index does not.
func docWithServers() *ir.Document {
	return &ir.Document{Servers: []ir.Server{{URLTemplate: "https://api.example.com"}}}
}

// indexViolations runs the index check and drops the truncation flag, which the
// cases below assert nothing about.
func indexViolations(doc *ir.Document) []Violation {
	vs, _ := runCheck(checkIndices, doc)
	return vs
}

func TestCheckIndices_ServiceServerIndexOutOfRange(t *testing.T) {
	doc := docWithServers()
	doc.Services = []ir.Service{{ID: "s/x", Servers: []int{-1, 1}}}

	got := indexViolations(doc)
	require.Len(t, got, 2, "both the negative index and the one past the end are reported")
	assert.Equal(t, "ir/server-index-out-of-range", got[0].Code)
	assert.Equal(t, "doc.Services[0].Servers[0]", got[0].Path)
	assert.Equal(t, "doc.Services[0].Servers[1]", got[1].Path)
}

func TestCheckIndices_ChannelServerIndexOutOfRange(t *testing.T) {
	doc := docWithServers()
	doc.Channels = map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x", Servers: []int{2}}}

	got := indexViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/server-index-out-of-range", got[0].Code)
	assert.Contains(t, got[0].Message, "none of the 1 declared servers")
}

func TestCheckIndices_ServerIndexWithNoDeclaredServers(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x", Servers: []int{0}}}}

	got := indexViolations(doc)
	require.Len(t, got, 1, "a document declaring no servers can satisfy no index")
	assert.Contains(t, got[0].Message, "none of the 0 declared servers")
}

func TestCheckIndices_ServerIndexInRangeIsClean(t *testing.T) {
	doc := docWithServers()
	doc.Services = []ir.Service{{ID: "s/x", Servers: []int{0}}}
	doc.Channels = map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x", Servers: []int{0}}}

	assert.Empty(t, indexViolations(doc))
}

// docWithSuccessStatus wraps one operation declaring a single response and one
// HTTP binding carrying the given SuccessStatus map.
func docWithSuccessStatus(status map[int]int) *ir.Document {
	return &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
			ID:        "op/x",
			Responses: []ir.Response{{}},
			Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				Method: "GET", SuccessStatus: status,
			}}},
		}}}},
	}}}
}

func TestCheckIndices_ResponseIndexOutOfRange(t *testing.T) {
	got := indexViolations(docWithSuccessStatus(map[int]int{-1: 200}))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/response-index-out-of-range", got[0].Code)
	assert.Contains(t, got[0].Message, "none of the 1 declared responses")

	got = indexViolations(docWithSuccessStatus(map[int]int{1: 202}))
	require.Len(t, got, 1)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].SuccessStatus[1]", got[0].Path)
}

// TestCheckIndices_ResponseIndexIsPlacedOnItsBinding holds the path to the
// binding the key sits on: here only the second binding is out of range.
func TestCheckIndices_ResponseIndexIsPlacedOnItsBinding(t *testing.T) {
	doc := docWithSuccessStatus(map[int]int{0: 200})
	op := &doc.Services[0].Groups[0].Operations[0]
	op.Bindings.HTTP = append(op.Bindings.HTTP, ir.HTTPBinding{Method: "POST", SuccessStatus: map[int]int{3: 201}})

	got := indexViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[1].SuccessStatus[3]", got[0].Path)
}

func TestCheckIndices_ResponseIndexInRangeIsClean(t *testing.T) {
	assert.Empty(t, indexViolations(docWithSuccessStatus(map[int]int{0: 200})))
}

// TestVerify_ReportsOutOfRangeIndices pins that Verify runs the check, not just
// the test: a dangling reference carried as an index has to reach a caller that
// only ever calls Verify.
func TestVerify_ReportsOutOfRangeIndices(t *testing.T) {
	doc := docWithServers()
	doc.Services = []ir.Service{{ID: "s/x", Name: ir.Naming{Source: "svc"}, Servers: []int{4}}}

	found := Verify(doc)
	codes := make([]string, 0, len(found))
	for _, v := range found {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "ir/server-index-out-of-range")
}

// indexCarrierFields is every ir field checkIndices reaches by name through
// reflection, with the shape it relies on: the field's own kind, and the element
// kind of a slice or the key kind of a map where the check calls Int() on one.
//
// checkIndices names its carriers because an integer index is invisible to a
// type-driven walk (see its comment). Naming them costs a coupling the compiler
// cannot check, and this is what checks it: a rename or a retype in ir fails
// here rather than panicking inside Verify on the first document that carries
// the field.
var indexCarrierFields = []struct {
	owner reflect.Type
	field string
	kind  reflect.Kind
	// inner is the element kind of a slice or the key kind of a map;
	// reflect.Invalid where the check does not look inside.
	inner reflect.Kind
}{
	{serviceType, "Servers", reflect.Slice, reflect.Int},
	{channelType, "Servers", reflect.Slice, reflect.Int},
	{operationType, "Responses", reflect.Slice, reflect.Invalid},
	{operationType, "Bindings", reflect.Struct, reflect.Invalid},
	{reflect.TypeFor[ir.OpBindings](), "HTTP", reflect.Slice, reflect.Struct},
	{reflect.TypeFor[ir.HTTPBinding](), "SuccessStatus", reflect.Map, reflect.Int},
}

// TestIndexCarrierFields_MatchTheIRShape fails when a field checkIndices reads
// by name is renamed or retyped in ir.
func TestIndexCarrierFields_MatchTheIRShape(t *testing.T) {
	t.Parallel()
	for _, c := range indexCarrierFields {
		f, ok := c.owner.FieldByName(c.field)
		require.True(t, ok, "%s has no field %s, which checkIndices reads by name",
			c.owner.Name(), c.field)
		require.Equal(t, c.kind, f.Type.Kind(), "%s.%s", c.owner.Name(), c.field)
		if c.inner == reflect.Invalid {
			continue
		}
		inner := f.Type.Elem()
		if c.kind == reflect.Map {
			inner = f.Type.Key()
		}
		assert.Equal(t, c.inner, inner.Kind(), "%s.%s element or key", c.owner.Name(), c.field)
	}
}

// integerFields classifies every integer-typed field the ir package declares as
// a reference (an index into a slice, needing an explicit bounds check) or a
// magnitude that addresses nothing. Neither checker's reflection walk can tell
// them apart, so the test below fails when ir grows an integer field nobody has
// classified.
//
// It stays hand-written because nothing can derive it: an ID-keyed registry is
// recognizable from Document's own shape (ir.KeyedClasses), while an
// index is an int like any other.
var integerFields = map[string]string{
	"Service.Servers":           "index into Document.Servers; bounds-checked by checkIndices",
	"Channel.Servers":           "index into Document.Servers; bounds-checked by checkIndices",
	"HTTPBinding.SuccessStatus": "keys index Operation.Responses; bounds-checked by checkIndices",
	"Provenance.Source":         "index into Document.Sources; bounds-checked by checkProvenance",

	// Deliberately unchecked. Index selects an element of the tuple a variant or
	// subtype happens to be, so the slice it addresses is not fixed by the
	// discriminator's own node: variants that are not tuples have no element list
	// at all. Bounding it needs a rule for that case which ir-design does not
	// state, so it is left out rather than guessed at.
	"Discriminator.Index": "tuple element position; addresses no one slice, see comment",

	// The ir package's own traversal machinery, which is not IR data at all: the
	// scan reaches every integer field the package declares, and classifying one
	// costs less than a filter that could hide a real index behind it.
	"refKey.ptr":      "address that identifies a reference the walk has met, not a position",
	"refKey.n":        "slice length that identifies a reference the walk has met, not a position",
	"descentRef.n":    "slice length that identifies a reference a traversal is inside, not a position",
	"insideRef.depth": "how many steps led the walk into a reference, not a position in any IR slice",
	"WalkPath.depth":  "how many of the walk's steps lead to a value, not a position in any IR slice",
	"pathStep.kind":   "how a step reaches its value, an enumeration rather than a position",
	"pathStep.index":  "the field or element a step took, set by the walk as it took it, not IR data",
	"valueWalk.all":   "the set of a walk's visitors, one bit each, not a position",
	"valueWalk.seen":  "for each pointer, the set of visitors that entered it, not a position",
	"valueWalk.shown": "how many values a walk has shown, which spaces its asks of a context, not a position",
	"entryFrame.base": "where a map's entries start on the traversal's own stack, not a position in any IR slice",
	"entryFrame.end":  "where a map's entries end on the traversal's own stack, not a position in any IR slice",
	"entryFrame.slot": "the first of a map's slots in the traversal's own copy storage, not a position in any IR slice",
	"entryStore.used": "how many slots of the traversal's own copy storage are in use, not a position in any IR slice",
	"flattener.met":   "the order a flattening met each model in, or a mark, not a position in any IR slice",
	"flattener.index": "where a wire name sits among a flattening's own members, not a position in any IR slice",
	"flattener.aliasOf": "where an alias scalar's chain end sits in a flattening's own aliasEnds, " +
		"not a position in any IR slice",
	"flattener.steps": "how much a flattening has read, not a position",
	"frame.next":      "the next composition entry a flattening follows, counted across Base, Implements and Mixins",
	"frame.num":       "the order a flattening met a model in, not a position in any IR slice",
	"frame.low":       "the least order of a model a flattening can reach unread, not a position in any IR slice",
	"frame.since":     "how many models a flattening had finished, a length of its own list, not a position",

	// The ir package's reading of its own types, which is not IR data either.
	"idField.index":       "the field through which a struct type declares its identity, read off the type",
	"keyedRegistry.index": "the Document field that keys a registry map, read off Document's type",

	"Property.WireID":       "protobuf field number / thrift id, not a position",
	"Variant.WireID":        "protobuf oneof field number or ordinal, not a position",
	"StatusRange.From":      "HTTP status code",
	"StatusRange.To":        "HTTP status code",
	"WireIDRange.From":      "wire ID bound, not a position",
	"WireIDRange.To":        "wire ID bound, not a position",
	"Constraints.Precision": "digit count",
	"Constraints.Scale":     "digit count",
	"Constraints.MinLength": "length bound",
	"Constraints.MaxLength": "length bound",
	"Constraints.MinItems":  "length bound",
	"Constraints.MaxItems":  "length bound",
	"Constraints.MinProps":  "property-count bound",
	"Constraints.MaxProps":  "property-count bound",
	"Position.Line":         "line inside a source file, not a position in any IR slice",
	"Position.Column":       "column inside a source file, not a position in any IR slice",
	"Decimal.exp":           "power of ten the leading significant digit carries, not a position",
}

// TestIntegerFields_AreAllClassified fails when ir declares an integer-typed
// struct field that integerFields does not account for. A new index reference
// added to the IR is invisible to every reflection walk in both checkers, so
// nothing else would notice it going unchecked.
func TestIntegerFields_AreAllClassified(t *testing.T) {
	t.Parallel()
	found := declaredIntegerFields(t)
	require.NotEmpty(t, found, "the ir package must declare integer fields")
	for _, name := range found {
		assert.Contains(t, integerFields, name,
			"ir.%s is an integer field that integerFields does not classify: "+
				"say whether it indexes a slice (and bounds-check it) or not", name)
	}
}

// declaredIntegerFields returns "Type.Field" for every struct field in the ir
// package whose type is built from an integer type.
func declaredIntegerFields(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, f := range declaredFields(t) {
		if mentionsInteger(t, f.field.Type(), 0) {
			names = append(names, f.key())
		}
	}
	return names
}

// mentionsInteger reports whether a field's type is built from an integer type,
// looking through pointers, slices, arrays, both halves of a map, and every named
// type whichever package declares it, so a `type Ordinal int` index cannot slip
// in unclassified.
//
// A sequence of bytes is raw data, never a list of positions, so it does not
// count: a field typed jsontext.Value, which ir aliases as RawValue, is a payload.
//
// depth bounds the walk. A type may recur through a pointer, slice or map, so
// exceeding the cap means a cycle, and failing beats reporting "not an integer".
func mentionsInteger(t *testing.T, typ types.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxTypeChain, "resolving a field type exceeded %d steps", maxTypeChain)
	switch u := typ.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsInteger != 0
	case *types.Pointer:
		return mentionsInteger(t, u.Elem(), depth+1)
	case *types.Slice:
		return !isByte(u.Elem()) && mentionsInteger(t, u.Elem(), depth+1)
	case *types.Array:
		return !isByte(u.Elem()) && mentionsInteger(t, u.Elem(), depth+1)
	case *types.Map:
		return mentionsInteger(t, u.Key(), depth+1) || mentionsInteger(t, u.Elem(), depth+1)
	default:
		return false // structs, interfaces, funcs, channels: no integer of their own
	}
}

// isByte reports whether typ is a byte-sized integer: byte, uint8, or a named
// type over either.
func isByte(typ types.Type) bool {
	basic, isBasic := typ.Underlying().(*types.Basic)
	return isBasic && basic.Kind() == types.Uint8
}

// TestCheckIndices_VisitingCostsNoAllocation holds the check's cost for an
// operation whose response indices are in range at nothing once it has met a
// SuccessStatus map: it reads keys into what it keeps, where
// reflect.MapIter.Key allocates a copy of each. It is not run in parallel:
// AllocsPerRun measures the process.
func TestCheckIndices_VisitingCostsNoAllocation(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
			ID:        "op/x",
			Responses: []ir.Response{{}, {}, {}},
			Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
				{Method: "GET", SuccessStatus: map[int]int{0: 200, 1: 201, 2: 202}},
				{Method: "PUT", SuccessStatus: map[int]int{2: 204}},
			}},
		}}}},
	}}}
	check := checkIndices(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "every index addresses a declared response")
}
