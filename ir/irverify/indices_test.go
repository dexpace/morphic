package irverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// docWithServers builds a document declaring one server, so index 0 addresses it
// and every other index does not.
func docWithServers() *ir.Document {
	return &ir.Document{Servers: []ir.Server{{URLTemplate: "https://api.example.com"}}}
}

func TestCheckIndices_ServiceServerIndexOutOfRange(t *testing.T) {
	doc := docWithServers()
	doc.Services = []ir.Service{{ID: "s/x", Servers: []int{-1, 1}}}

	got := checkIndices(doc)
	require.Len(t, got, 2, "both the negative index and the one past the end are reported")
	assert.Equal(t, "ir/server-index-out-of-range", got[0].Code)
	assert.Equal(t, "doc.Services[0].Servers[0]", got[0].Path)
	assert.Equal(t, "doc.Services[0].Servers[1]", got[1].Path)
}

func TestCheckIndices_ChannelServerIndexOutOfRange(t *testing.T) {
	doc := docWithServers()
	doc.Channels = map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x", Servers: []int{2}}}

	got := checkIndices(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/server-index-out-of-range", got[0].Code)
	assert.Contains(t, got[0].Message, "none of the 1 declared servers")
}

func TestCheckIndices_ServerIndexWithNoDeclaredServers(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x", Servers: []int{0}}}}

	got := checkIndices(doc)
	require.Len(t, got, 1, "a document declaring no servers can satisfy no index")
	assert.Contains(t, got[0].Message, "none of the 0 declared servers")
}

func TestCheckIndices_ServerIndexInRangeIsClean(t *testing.T) {
	doc := docWithServers()
	doc.Services = []ir.Service{{ID: "s/x", Servers: []int{0}}}
	doc.Channels = map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x", Servers: []int{0}}}

	assert.Empty(t, checkIndices(doc))
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
	got := checkIndices(docWithSuccessStatus(map[int]int{-1: 200}))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/response-index-out-of-range", got[0].Code)
	assert.Contains(t, got[0].Message, "none of the 1 declared responses")

	got = checkIndices(docWithSuccessStatus(map[int]int{1: 202}))
	require.Len(t, got, 1)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].SuccessStatus[1]", got[0].Path)
}

func TestCheckIndices_ResponseIndexInRangeIsClean(t *testing.T) {
	assert.Empty(t, checkIndices(docWithSuccessStatus(map[int]int{0: 200})))
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

// integerFields classifies every integer-typed field the ir package declares.
//
// An integer field is either a reference — an index into a slice, which no
// type-driven walk can recognize and which therefore needs an explicit bounds
// check — or a magnitude that addresses nothing. The distinction is invisible to
// both checkers' reflection walks, so it is recorded here and this test fails
// when the ir package grows an integer field that nobody has classified. That is
// the drift guard the index checks need, in the same spirit as the one over
// refKindByType.
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

	"Value.Bytes":           "byte-string payload, not a sequence of positions",
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
}

// integerTypes names the Go integer types an ir field can be built from.
var integerTypes = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true,
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
// package's production sources whose type is built from an integer type.
func declaredIntegerFields(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		ast.Inspect(f, func(node ast.Node) bool {
			ts, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			names = append(names, integerFieldsOf(ts)...)
			return true
		})
	}
	return names
}

// integerFieldsOf returns the integer-typed fields of one struct declaration.
func integerFieldsOf(ts *ast.TypeSpec) []string {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	var names []string
	for _, field := range st.Fields.List {
		if !mentionsInteger(field.Type) {
			continue
		}
		for _, name := range field.Names {
			names = append(names, ts.Name.Name+"."+name.Name)
		}
	}
	return names
}

// mentionsInteger reports whether a field's type expression is built from an
// integer type, looking through pointers, slices and both halves of a map.
func mentionsInteger(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return integerTypes[t.Name]
	case *ast.StarExpr:
		return mentionsInteger(t.X)
	case *ast.ArrayType:
		return mentionsInteger(t.Elt)
	case *ast.MapType:
		return mentionsInteger(t.Key) || mentionsInteger(t.Value)
	default:
		return false // named types, selectors, interfaces: not integers themselves
	}
}
