package ir_test

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// walkedRefs walks doc with the IDReferences built for it and returns them, with
// whether the walk was cut short.
func walkedRefs(doc *ir.Document) (*ir.IDReferences, bool) {
	refs := ir.NewIDReferences(doc)
	truncated := ir.WalkValues(doc, ir.DocumentPath, refs)
	return refs, truncated
}

// spelled returns each of refs as "path=id", in the order given.
func spelled(refs []ir.IDReference) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Path+"="+ref.ID)
	}
	return out
}

// refIDs returns the ID each of refs names, in the order given.
func refIDs(refs []ir.IDReference) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.ID)
	}
	return out
}

// docWithRef builds a model whose property points at target via a TypeRef.
func docWithRef(target ir.TypeID) *ir.Document {
	holder := &ir.Model{
		ID:         "t/x/Holder",
		Properties: []ir.Property{{ID: "p/x/Holder/f", Type: ir.TypeRef{Target: target}}},
	}
	return &ir.Document{Types: ir.TypeRegistry{holder.ID: holder}}
}

// TestKeyedClasses_DerivedFromDocumentShape pins the derivation that replaces a
// hand-written registry table in each checker: every ID-keyed map on Document is
// a registry, labelled by the field that declares it, and a map keyed by plain
// string — Unmodeled keys on a source construct's name, not an identity — is
// not.
func TestKeyedClasses_DerivedFromDocumentShape(t *testing.T) {
	t.Parallel()
	want := map[reflect.Type]string{
		reflect.TypeFor[ir.TypeID]():    "types",
		reflect.TypeFor[ir.ChannelID](): "channels",
		reflect.TypeFor[ir.MessageID](): "messages",
		reflect.TypeFor[ir.AuthID]():    "auth",
		reflect.TypeFor[ir.ServerID]():  "servers",
	}
	classes := ir.KeyedClasses()
	refs := ir.NewIDReferences(&ir.Document{})
	for class, label := range want {
		assert.True(t, classes[class], "%s names a Document registry", class.Name())
		assert.Equal(t, label, refs.Label(class))
	}
	assert.False(t, classes[reflect.TypeFor[string]()], "a plain string key is a name, not an identity")
	assert.Len(t, classes, len(want), "Document declares exactly these ID-keyed registries")
}

// TestNewIDReferences_LabelsTheRegistryOfEveryClass pins which registry each
// class of ID resolves in. A class Document keys a map by resolves in that map;
// every other class resolves against what the walk declares, whether or not the
// document declares any; and PropID, a position inside its model, has no
// registry at all. Every class a node of the IR declares is listed, which the
// IR's type graph holds the table to.
func TestNewIDReferences_LabelsTheRegistryOfEveryClass(t *testing.T) {
	t.Parallel()
	refs := ir.NewIDReferences(&ir.Document{})
	want := map[reflect.Type]string{
		reflect.TypeFor[ir.TypeID]():       "types",
		reflect.TypeFor[ir.ChannelID]():    "channels",
		reflect.TypeFor[ir.MessageID]():    "messages",
		reflect.TypeFor[ir.AuthID]():       "auth",
		reflect.TypeFor[ir.ServerID]():     "servers",
		reflect.TypeFor[ir.OpID]():         "op declarations",
		reflect.TypeFor[ir.ServiceID]():    "service declarations",
		reflect.TypeFor[ir.GroupID]():      "group declarations",
		reflect.TypeFor[ir.ParamID]():      "param declarations",
		reflect.TypeFor[ir.EnumMemberID](): "enummember declarations",
		reflect.TypeFor[ir.PropID]():       "",
	}
	for class, label := range want {
		assert.Equal(t, label, refs.Label(class), class.Name())
	}
	for class := range ir.IDClasses() {
		assert.Contains(t, want, class, "a node declares %s", class.Name())
	}
}

// TestIDReferences_KeepsAnUnresolvedKeyedRef holds a reference into a
// map-keyed registry that does not resolve to being kept as the walk reaches
// it, at the path it sits at.
func TestIDReferences_KeepsAnUnresolvedKeyedRef(t *testing.T) {
	t.Parallel()
	refs, _ := walkedRefs(docWithRef("t/x/Missing"))
	assert.Equal(t, []ir.IDReference{{
		Class: reflect.TypeFor[ir.TypeID](),
		ID:    "t/x/Missing",
		Path:  "doc.Types[t/x/Holder].Properties[0].Type.Target",
	}}, refs.Kept())
}

// TestIDReferences_KeepsNoResolvedKeyedRef holds the walk to settling a
// reference into a map-keyed registry as it reaches it: one that resolves is not
// kept, so its path is never rendered.
func TestIDReferences_KeepsNoResolvedKeyedRef(t *testing.T) {
	t.Parallel()
	refs, _ := walkedRefs(docWithRef("t/x/Holder"))
	assert.Empty(t, refs.Kept(), "the holder's own ID and the target both resolve in types")
}

// TestIDReferences_KeepsOnlyWhatIsNotYetDeclared holds the other half: a
// declared class's registry is complete only once the walk is over, so a
// reference to an identity not yet declared is kept, dangling or not. One the
// walk has read a declaration of resolves whatever comes later and is not kept,
// a node's own ID included, which the walk reaches just after the node.
func TestIDReferences_KeepsOnlyWhatIsNotYetDeclared(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{ID: "g/x/G", Operations: []ir.Operation{
			{ID: "op/x/A", OverloadOf: new(ir.OpID("op/x/B"))},
			{ID: "op/x/B", OverloadOf: new(ir.OpID("op/x/A"))},
			{ID: "op/x/C", OverloadOf: new(ir.OpID("op/x/Missing"))},
		}}},
	}}}
	refs, _ := walkedRefs(doc)
	assert.Equal(t, []string{
		"doc.Services[0].Groups[0].Operations[0].OverloadOf=op/x/B",
		"doc.Services[0].Groups[0].Operations[2].OverloadOf=op/x/Missing",
	}, spelled(refs.Kept()))
	assert.Equal(t, []string{"doc.Services[0].Groups[0].Operations[2].OverloadOf=op/x/Missing"},
		spelled(refs.Unresolved(false)), "the forward reference resolves once the walk is over")
}

// TestIDReferences_SkipsEmptyIDs holds an empty ID to being neither a reference
// nor a declaration: some positions spell "none" with one, and nothing can
// resolve to one.
func TestIDReferences_SkipsEmptyIDs(t *testing.T) {
	t.Parallel()
	refs, _ := walkedRefs(docWithRef(""))
	assert.Empty(t, refs.Kept(), "an empty target is no reference")

	refs, _ = walkedRefs(&ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
		Operations: []ir.Operation{{OverloadOf: new(ir.OpID(""))}},
	}}}}})
	assert.Empty(t, refs.Kept())
	assert.False(t, refs.Declares(reflect.TypeFor[ir.OpID](), ""), "an empty ID declares nothing")
}

// TestIDReferences_UnresolvedSettlesEachRegistry drives each registry both ways:
// a reference resolving in it is clean, one naming nothing it holds is reported
// at the path it sits at. A map key is a reference too, since Service.Renames
// keys name types.
func TestIDReferences_UnresolvedSettlesEachRegistry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		doc    *ir.Document
		dangle []string
	}{
		{"a type reference resolving", docWithRef("t/x/Holder"), []string{}},
		{"a type reference dangling", docWithRef("t/x/Missing"),
			[]string{"doc.Types[t/x/Holder].Properties[0].Type.Target=t/x/Missing"}},
		{"an auth reference dangling", &ir.Document{Services: []ir.Service{{
			Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: "a/missing"}}}},
		}}}, []string{"doc.Services[0].Auth[0].Schemes[0].Scheme=a/missing"}},
		{"a message reference dangling", &ir.Document{Channels: map[ir.ChannelID]ir.Channel{
			"c/x": {ID: "c/x", Messages: []ir.MessageID{"m/missing"}},
		}}, []string{"doc.Channels[c/x].Messages[0]=m/missing"}},
		{"a channel reference dangling", &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{ID: "op/x", Bindings: ir.OpBindings{
				Message: &ir.MessageBinding{Channel: "c/missing"},
			}}},
		}}}}}, []string{"doc.Services[0].Groups[0].Operations[0].Bindings.Message.Channel=c/missing"}},
		{"an operation reference resolving", &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{ID: "op/x/A"}, {ID: "op/x/B", OverloadOf: new(ir.OpID("op/x/A"))}},
		}}}}}, []string{}},
		{"an operation reference dangling", &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{ID: "op/x/B", OverloadOf: new(ir.OpID("op/x/Missing"))}},
		}}}}}, []string{"doc.Services[0].Groups[0].Operations[0].OverloadOf=op/x/Missing"}},
		{"a service reference resolving", &ir.Document{Services: []ir.Service{
			{ID: "s/base"}, {ID: "s/x", Extends: []ir.ServiceID{"s/base"}},
		}}, []string{}},
		{"a service reference dangling", &ir.Document{Services: []ir.Service{
			{ID: "s/x", Extends: []ir.ServiceID{"s/missing"}},
		}}, []string{"doc.Services[0].Extends[0]=s/missing"}},
		{"a reference to a class nothing declares", &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
			Resource: &ir.ResourceInfo{InstanceOps: []ir.OpID{"op/x/Missing"}},
		}}}}}, []string{"doc.Services[0].Groups[0].Resource.InstanceOps[0]=op/x/Missing"}},
		{"a map key dangling", &ir.Document{
			Types:    ir.TypeRegistry{"t/x/M": &ir.Any{ID: "t/x/M"}},
			Services: []ir.Service{{Renames: map[ir.TypeID]ir.Naming{"t/x/ghost": {Source: "Ghost"}}}},
		}, []string{"doc.Services[0].Renames[t/x/ghost].key=t/x/ghost"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refs, truncated := walkedRefs(tc.doc)
			require.False(t, truncated)
			assert.Equal(t, tc.dangle, spelled(refs.Unresolved(false)))
		})
	}
}

// TestIDReferences_UnresolvedKeepsWalkOrder holds the references reported to
// the order the walk reached them, whichever registry each resolves in, so a
// checker's own sort is what decides the order it reports in.
func TestIDReferences_UnresolvedKeepsWalkOrder(t *testing.T) {
	t.Parallel()
	doc := docWithRef("t/x/Missing")
	doc.Services = []ir.Service{{ID: "s/x", Extends: []ir.ServiceID{"s/b", "s/a"}}}
	refs, _ := walkedRefs(doc)

	assert.Equal(t, []string{"s/b", "s/a", "t/x/Missing"}, refIDs(refs.Unresolved(false)),
		"Services comes before Types on Document")
}

// TestIDReferences_TruncatedWalkClaimsNoDeclaredClass holds the one direction a
// declaration-derived registry can be wrong in. The operation the shallow
// reference names is declared past the walk cap, so the declarations the walk
// read say "not declared" of a node that is. A map-keyed registry is read off
// the document, not the walk, and still answers.
func TestIDReferences_TruncatedWalkClaimsNoDeclaredClass(t *testing.T) {
	t.Parallel()
	deep, target := irtest.NestedOverloadDoc(ir.MaxWalkDepth)
	deep.Types = docWithRef("t/x/Missing").Types
	refs, truncated := walkedRefs(deep)
	require.True(t, truncated, "the walk must be cut short")

	assert.Equal(t, []string{"t/x/Missing"}, refIDs(refs.Unresolved(true)),
		"%s is declared past the cap, not undeclared", target)

	// The truncation is what spares the target: told the walk was whole, the
	// same references report it, so the case above is not passing on a walk that
	// kept nothing.
	assert.Contains(t, refIDs(refs.Unresolved(false)), string(target))

	shallow, _ := irtest.NestedOverloadDoc(0)
	refs, _ = walkedRefs(shallow)
	assert.Empty(t, refs.Unresolved(false), "within the cap the same shape resolves")
}

// TestIDReferences_PropIDIsDeclaredButNeverKept pins the one class no registry
// resolves. A PropID names a position inside its model, so a document-wide
// answer would report one defect twice where a tighter check knows the model;
// the declarations are still answered, for the check that holds a PropID to a
// declared property.
func TestIDReferences_PropIDIsDeclaredButNeverKept(t *testing.T) {
	t.Parallel()
	doc := docWithRef("t/x/Holder")
	doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
		ID:         "op/x/List",
		Pagination: &ir.Pagination{Items: &ir.PropPath{Segments: []ir.PropID{"p/x/ghost"}}},
	}}}}}}
	refs, _ := walkedRefs(doc)

	propID := reflect.TypeFor[ir.PropID]()
	for _, ref := range refs.Kept() {
		assert.NotEqual(t, propID, ref.Class, "no PropID is kept: %s", ref.Path)
	}
	assert.True(t, refs.Declares(propID, "p/x/Holder/f"))
	assert.False(t, refs.Declares(propID, "p/x/ghost"))
}

// TestIDReferences_DeclaresOnlyWhatNoMapKeys holds Declares to the identities a
// reference of a class no Document map keys resolves against: an operation's,
// a parameter's and a field argument's among them. A map-keyed class resolves
// against its map, so a node declaring one is not asked about.
func TestIDReferences_DeclaresOnlyWhatNoMapKeys(t *testing.T) {
	t.Parallel()
	doc := declaringDoc()
	doc.Services[0].Groups[0].Operations[0].Params = []ir.Parameter{{ID: "param/x/op"}}
	model, ok := doc.Types["t/x/M"].(*ir.Model)
	require.True(t, ok)
	model.Properties[0].Args = []ir.Parameter{{ID: "param/x/arg"}}
	refs, _ := walkedRefs(doc)

	for _, tc := range []struct {
		class    reflect.Type
		id       string
		declares bool
	}{
		{reflect.TypeFor[ir.OpID](), "op/x", true},
		{reflect.TypeFor[ir.ServiceID](), "s/x", true},
		{reflect.TypeFor[ir.GroupID](), "g/x", true},
		{reflect.TypeFor[ir.ParamID](), "param/x/op", true},
		{reflect.TypeFor[ir.ParamID](), "param/x/arg", true},
		{reflect.TypeFor[ir.OpID](), "op/x/ghost", false},
		{reflect.TypeFor[ir.TypeID](), "t/x/M", false},
		{reflect.TypeFor[ir.ChannelID](), "c/x", false},
		{reflect.TypeFor[ir.AuthID](), "auth/x", false},
	} {
		assert.Equal(t, tc.declares, refs.Declares(tc.class, tc.id), "%s %s", tc.class.Name(), tc.id)
	}
}

// TestNewIDReferences_EachIsItsOwn holds two IDReferences of one document apart:
// what one walk gathers is that walk's, so a checker running beside another
// shares nothing with it.
func TestNewIDReferences_EachIsItsOwn(t *testing.T) {
	t.Parallel()
	doc := declaringDoc()
	walked, _ := walkedRefs(doc)
	unwalked := ir.NewIDReferences(doc)

	opID := reflect.TypeFor[ir.OpID]()
	assert.True(t, walked.Declares(opID, "op/x"))
	assert.False(t, unwalked.Declares(opID, "op/x"), "a walk another IDReferences took declares nothing here")
	assert.Empty(t, unwalked.Kept())
}

// TestNewIDReferences_NilDocumentKeysNothing keeps building one report-only: a
// nil document keys nothing rather than panicking on the way to saying so, its
// classes are still labelled, since they come from Document's type, and a walk
// of it reaches nothing to keep.
func TestNewIDReferences_NilDocumentKeysNothing(t *testing.T) {
	t.Parallel()
	refs, truncated := walkedRefs(nil)
	assert.False(t, truncated)
	assert.Empty(t, refs.Unresolved(false))
	assert.Equal(t, "types", refs.Label(reflect.TypeFor[ir.TypeID]()))

	refs = ir.NewIDReferences(nil)
	ir.WalkValues(docWithRef("t/x/Holder"), ir.DocumentPath, refs)
	assert.Equal(t, []string{
		"doc.Types[t/x/Holder].key=t/x/Holder",
		"doc.Types[t/x/Holder].ID=t/x/Holder",
		"doc.Types[t/x/Holder].Properties[0].Type.Target=t/x/Holder",
	}, spelled(refs.Kept()), "a map a nil document keys holds nothing a reference resolves to")
}

// TestIDReferences_SettlingCostsNoAllocation holds the walk's per-value cost at
// nothing for what it settles as it goes: a reference resolving in a map-keyed
// registry, a struct declaring no identity or one already recorded, and a
// reference to a declared identity, a node's own included. Each is handed the
// walk's own path, so rendering one for a reference not kept would count.
// Reading a registry through reflect allocated twice per resolving lookup, and a
// golden-spec IR holds hundreds of thousands. It is not run in parallel:
// AllocsPerRun measures the process.
func TestIDReferences_SettlingCostsNoAllocation(t *testing.T) {
	doc := declaringDoc()
	group := &doc.Services[0].Groups[0]
	group.Operations = append(group.Operations, ir.Operation{ID: "op/y", OverloadOf: new(ir.OpID("op/x"))})
	refs := ir.NewIDReferences(doc)

	assert.Empty(t, irtest.AllocatingVisits(doc, refs), "every value of a sound document settles")
	assert.Empty(t, refs.Kept(), "nothing visited may dangle")
	assert.True(t, refs.Declares(reflect.TypeFor[ir.OpID](), "op/y"), "the walk reached the last operation")
}

// TestNewIDReferences_ReadsKeysWithoutAllocatingEach holds reading a registry's
// keys to one reused value: what building one allocates does not grow by one
// per key, as copying each key out of the map would. It is not run in parallel:
// AllocsPerRun measures the process.
func TestNewIDReferences_ReadsKeysWithoutAllocatingEach(t *testing.T) {
	allocs := func(types int) float64 {
		doc := &ir.Document{Types: ir.TypeRegistry{}}
		for i := range types {
			id := ir.TypeID("t/x/M" + strconv.Itoa(i))
			doc.Types[id] = &ir.Any{ID: id}
		}
		return testing.AllocsPerRun(5, func() { _ = ir.NewIDReferences(doc) })
	}
	const types = 1024
	assert.Less(t, allocs(types)-allocs(0), float64(types)/16, "reading %d keys allocates per key", types)
}

// TestRefNoun_NamesTheReferenceClass pins the singular both checkers spell a
// dangling-reference code with, so one defect reads under one code whichever of
// them reports it. PropID is included though it addresses no registry: the noun
// is derived from the type, and pass.Validate reports ir/dangling-prop-ref with
// it.
func TestRefNoun_NamesTheReferenceClass(t *testing.T) {
	t.Parallel()
	for want, idType := range map[string]reflect.Type{
		"type":    reflect.TypeFor[ir.TypeID](),
		"channel": reflect.TypeFor[ir.ChannelID](),
		"message": reflect.TypeFor[ir.MessageID](),
		"auth":    reflect.TypeFor[ir.AuthID](),
		"prop":    reflect.TypeFor[ir.PropID](),
	} {
		assert.Equal(t, want, ir.RefNoun(idType))
	}
}
