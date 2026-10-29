package ir_test

import (
	"math/bits"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// declaringDoc declares one node of every class that carries its own ID, so a
// test can ask what the value graph yields for each.
func declaringDoc() *ir.Document {
	return &ir.Document{
		Types: ir.TypeRegistry{
			"t/x/M": &ir.Model{ID: "t/x/M", Properties: []ir.Property{
				{ID: "p/x/M/f", Type: ir.TypeRef{Target: "t/x/M"}},
			}},
		},
		Channels: map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x"}},
		Messages: map[ir.MessageID]ir.Message{"m/x": {ID: "m/x"}},
		Auth:     map[ir.AuthID]ir.AuthScheme{"auth/x": {ID: "auth/x", Kind: ir.AuthKindAPIKey}},
		Services: []ir.Service{{
			ID:     "s/x",
			Groups: []ir.OperationGroup{{ID: "g/x", Operations: []ir.Operation{{ID: "op/x"}}}},
		}},
	}
}

// declaredBy indexes declarations by ID class name, so a case can assert on one
// class without depending on walk order.
func declaredBy(decls []declaredAt) map[string][]declaredAt {
	out := map[string][]declaredAt{}
	for _, d := range decls {
		out[d.Class.Name()] = append(out[d.Class.Name()], d)
	}
	return out
}

// TestIDDeclarations_ReachesEveryIDBearingNodeOnce pins the derivation the
// declared classes are resolved and held unique from: every node carrying an ID
// of its own contributes exactly one declaration, at the path it sits at.
//
// The "once" half is the load-bearing one. Every type node embeds TypeCommon and
// promotes its ID, so a walk counting promoted fields would report a sound
// document as one where every type ID is declared twice.
func TestIDDeclarations_ReachesEveryIDBearingNodeOnce(t *testing.T) {
	t.Parallel()
	decls, truncated := declaredIDs(declaringDoc())
	require.False(t, truncated, "a shallow document must not exhaust the walk")

	byClass := declaredBy(decls)
	want := map[string]declaredAt{
		"TypeID":    {IDDeclaration: ir.IDDeclaration{ID: "t/x/M"}, Path: "doc.Types[t/x/M]"},
		"PropID":    {IDDeclaration: ir.IDDeclaration{ID: "p/x/M/f"}, Path: "doc.Types[t/x/M].Properties[0]"},
		"ChannelID": {IDDeclaration: ir.IDDeclaration{ID: "c/x"}, Path: "doc.Channels[c/x]"},
		"MessageID": {IDDeclaration: ir.IDDeclaration{ID: "m/x"}, Path: "doc.Messages[m/x]"},
		"AuthID":    {IDDeclaration: ir.IDDeclaration{ID: "auth/x"}, Path: "doc.Auth[auth/x]"},
		"ServiceID": {IDDeclaration: ir.IDDeclaration{ID: "s/x"}, Path: "doc.Services[0]"},
		"GroupID":   {IDDeclaration: ir.IDDeclaration{ID: "g/x"}, Path: "doc.Services[0].Groups[0]"},
		"OpID":      {IDDeclaration: ir.IDDeclaration{ID: "op/x"}, Path: "doc.Services[0].Groups[0].Operations[0]"},
	}
	for class, w := range want {
		got := byClass[class]
		require.Len(t, got, 1, "%s is declared by exactly one node", class)
		assert.Equal(t, w.ID, got[0].ID, class)
		assert.Equal(t, w.Path, got[0].Path, class)
	}
	assert.Len(t, byClass, len(want), "these are every ID class a node declares")
}

// TestIDDeclarations_SkipsEmptyAndAbsentIDs holds the two ways a struct declares no
// identity: it carries no ID field at all, or it carries an empty one. An empty
// ID names nothing a reference could resolve to, and treating several of them as
// one another's duplicates would name the wrong defect.
func TestIDDeclarations_SkipsEmptyAndAbsentIDs(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{
		Name:     "no ID field anywhere on Document itself",
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{}}}}}},
	}
	decls, truncated := declaredIDs(doc)
	assert.False(t, truncated)
	assert.Empty(t, decls)
}

// TestIDDeclarations_NilDocumentDeclaresNothing keeps the derivation report-only:
// a nil document yields no declaration rather than panicking on the way to
// saying so, as IDReferences does.
func TestIDDeclarations_NilDocumentDeclaresNothing(t *testing.T) {
	t.Parallel()
	decls, truncated := declaredIDs(nil)
	assert.Empty(t, decls)
	assert.False(t, truncated)
}

// The shapes IDReader.DeclaredIDOf has to tell apart. The IR holds only the
// first two, an ID a struct declares and one it promotes, so a walk over a
// Document cannot exercise the other clauses of the predicate.
type (
	idDeclarer    struct{ ID ir.OpID }
	idPromoter    struct{ idDeclarer }
	plainID       struct{ ID string }
	noID          struct{ Name string }
	pointerID     struct{ ID *ir.OpID }
	ID            string
	embeddedID    struct{ ID }
	otherDeclarer struct{ ID ir.TypeID }
	twoPromoters  struct {
		idDeclarer
		otherDeclarer
	}
	shadowing struct {
		idDeclarer
		ID ir.TypeID
	}
	idNumber     int
	numberID     struct{ ID idNumber }
	lateDeclarer struct {
		Name    string
		Count   int
		Ignored ir.TypeID
		ID      ir.OpID
	}
	promoterThenOwn struct {
		idPromoter
		Name string
		ID   ir.GroupID
	}
)

// declaredShapes are the cases TestIDReader_ClassifiesEachShape reads: a value
// and what it declares.
func declaredShapes() []struct {
	name     string
	v        reflect.Value
	class    reflect.Type
	id       string
	declares bool
} {
	return []struct {
		name     string
		v        reflect.Value
		class    reflect.Type
		id       string
		declares bool
	}{
		{"a named string ID the type declares", reflect.ValueOf(idDeclarer{ID: "op/a"}),
			reflect.TypeFor[ir.OpID](), "op/a", true},
		{"an empty ID is still a declaration", reflect.ValueOf(idDeclarer{}),
			reflect.TypeFor[ir.OpID](), "", true},
		{"an ID reached only by promotion", reflect.ValueOf(idPromoter{}), nil, "", false},
		{"a plain string named ID", reflect.ValueOf(plainID{ID: "x"}), nil, "", false},
		{"no ID field", reflect.ValueOf(noID{Name: "x"}), nil, "", false},
		{"a pointer to an ID class", reflect.ValueOf(pointerID{}), nil, "", false},
		{"a named type of another kind", reflect.ValueOf(numberID{ID: 1}), nil, "", false},
		{"an embedded named string type called ID", reflect.ValueOf(embeddedID{ID: "e"}),
			reflect.TypeFor[ID](), "e", true},
		{"two promoted IDs", reflect.ValueOf(twoPromoters{}), nil, "", false},
		{"a direct ID beside a promoted one", reflect.ValueOf(shadowing{ID: "t/x"}),
			reflect.TypeFor[ir.TypeID](), "t/x", true},
		{"an ID after other fields, an ID class among them",
			reflect.ValueOf(lateDeclarer{Ignored: "t/x", ID: "op/late"}),
			reflect.TypeFor[ir.OpID](), "op/late", true},
		{"an ID after an embedded struct promoting another",
			reflect.ValueOf(promoterThenOwn{ID: "g/own"}),
			reflect.TypeFor[ir.GroupID](), "g/own", true},
		{"a value that is no struct", reflect.ValueOf(ir.OpID("op/a")), nil, "", false},
		{"the zero Value", reflect.Value{}, nil, "", false},
		{"a pointer to a struct", reflect.ValueOf(&idDeclarer{ID: "op/a"}), nil, "", false},
		{"another value of a type read before", reflect.ValueOf(idDeclarer{ID: "op/b"}),
			reflect.TypeFor[ir.OpID](), "op/b", true},
	}
}

// TestIDReader_ClassifiesEachShape pins each clause of the declared-ID
// predicate: the field is named ID, the struct declares it rather than
// promotes it, and its type is a named string. A value that is no struct
// declares nothing, and neither does the zero Value, so no caller can make the
// lookup panic. The field is found wherever it sits. One reader asked of every
// shape in turn answers each as a fresh one does, its memory of earlier types
// included.
func TestIDReader_ClassifiesEachShape(t *testing.T) {
	t.Parallel()
	var shared ir.IDReader
	for _, tc := range declaredShapes() {
		for reader, r := range map[string]*ir.IDReader{"fresh": new(ir.IDReader), "shared": &shared} {
			class, id, declares := r.DeclaredIDOf(tc.v)

			assert.Equal(t, tc.declares, declares, "%s, %s reader", tc.name, reader)
			assert.Equal(t, tc.class, class, "%s, %s reader", tc.name, reader)
			assert.Equal(t, tc.id, id, "%s, %s reader", tc.name, reader)
		}
	}
}

// TestIDReader_ReadsEachStructTypeOnce holds the rule for where a struct
// declares its identity to one reading per type, however many values of it a
// walk shows: reading it per value cost more than everything else a visitor
// does, and no result can show the difference.
func TestIDReader_ReadsEachStructTypeOnce(t *testing.T) {
	t.Parallel()
	doc := declaringDoc()
	doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations,
		ir.Operation{ID: "op/y"}, ir.Operation{ID: "op/z"})
	structs, types := 0, map[reflect.Type]bool{}
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if v.Kind() == reflect.Struct {
			structs++
			types[v.Type()] = true
		}
		return true
	}))
	require.Greater(t, structs, len(types), "the document must repeat a struct type, or there is nothing to save")

	reads := 0
	r := ir.CountingIDReader(&reads)
	var ops []string
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if class, id, _ := r.DeclaredIDOf(v); class == reflect.TypeFor[ir.OpID]() {
			ops = append(ops, id)
		}
		return true
	}))
	assert.Equal(t, len(types), reads)
	assert.Equal(t, []string{"op/x", "op/y", "op/z"}, ops, "a type read once still answers for each value")
}

// TestDeclaredIDField_CostsNoAllocation holds the rule an IDReader remembers to
// reading a type at no allocation. A type node embeds TypeCommon and declares no
// ID of its own, which is where asking reflect for the field by name searched
// the embedded struct and allocated five times. It is not run in parallel:
// AllocsPerRun measures the process.
func TestDeclaredIDField_CostsNoAllocation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		t        reflect.Type
		declares bool
	}{
		{"a type node, which embeds the struct declaring its ID", reflect.TypeFor[ir.Model](), false},
		{"another embedding one", reflect.TypeFor[ir.Union](), false},
		{"the struct the type nodes embed", reflect.TypeFor[ir.TypeCommon](), true},
		{"a struct declaring its own ID", reflect.TypeFor[ir.Operation](), true},
		{"a struct declaring none", reflect.TypeFor[ir.TypeRef](), false},
	} {
		_, class := ir.DeclaredIDField(tc.t)
		require.Equal(t, tc.declares, class != nil, tc.name)
		assert.Zero(t, testing.AllocsPerRun(10, func() { _, _ = ir.DeclaredIDField(tc.t) }), tc.name)
	}
}

// TestIDDeclarations_AllocatesNothingPerDeclaration holds a walk's per-value
// cost for its declarations at nothing, a declaration included: each keeps its
// path unspelled, where spelling it costs a string per declaration. Each visit
// is handed the walk's own path, so spelling one would count. It is not run in
// parallel: AllocsPerRun measures the process.
func TestIDDeclarations_AllocatesNothingPerDeclaration(t *testing.T) {
	doc := declaringDoc()
	decls, _ := declaredIDs(doc)
	require.NotEmpty(t, decls, "some value declares, or no declaration is measured")

	var visitor ir.IDDeclarations
	assert.Empty(t, irtest.AllocatingVisits(doc, &visitor))
}

// TestIDDeclarations_GrowsTheListGeometrically holds the list of declarations
// to a growth factor well past append's, as TestPathLog_GrowsGeometrically holds
// the log its paths are kept in: append grows a large slice by a quarter at a
// time, so a list built one declaration at a time is copied several times
// over. Beside a log recording the same paths, the list may grow fewer times
// than doubling would take, log2(n). It is not run in parallel: AllocsPerRun
// measures the process.
func TestIDDeclarations_GrowsTheListGeometrically(t *testing.T) {
	const declarations = 1 << 13
	enum := &ir.Enum{ID: "t/e"}
	for i := range declarations {
		enum.Members = append(enum.Members, ir.EnumMember{ID: ir.EnumMemberID("e/e/" + strconv.Itoa(i))})
	}
	doc := &ir.Document{Types: ir.TypeRegistry{enum.ID: enum}}
	declaring := testing.AllocsPerRun(3, func() {
		ir.WalkValues(doc, ir.DocumentPath, &ir.IDDeclarations{})
	})
	recording := testing.AllocsPerRun(3, func() {
		var ids ir.IDReader
		var log ir.PathLog
		ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
			if _, id, declares := ids.DeclaredIDOf(v); declares && id != "" {
				log.Record(at)
			}
			return true
		}))
	})
	assert.Less(t, declaring-recording, float64(bits.Len(declarations)-1), "%v allocations to list", declaring-recording)
}
