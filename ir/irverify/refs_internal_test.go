package irverify

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// docWithRef builds a model whose property points at target via a TypeRef.
func docWithRef(target ir.TypeID) *ir.Document {
	holder := &ir.Model{
		ID: "t/x/Holder",
		Properties: []ir.Property{{
			ID:   "p/x/Holder/f",
			Type: ir.TypeRef{Target: target},
		}},
	}
	return &ir.Document{Types: ir.TypeRegistry{holder.ID: holder}}
}

// keptRefs returns the references a refsCollector keeps from one walk of doc.
func keptRefs(doc *ir.Document) []refSite {
	refs := newRefsCollector(doc)
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(refs.visit))
	return refs.sites
}

func TestRefsCollector_KeepsAnUnresolvedTypeRefTarget(t *testing.T) {
	sites := keptRefs(docWithRef("t/x/Missing"))
	require.Len(t, sites, 1, "reflection walk should discover the property TypeRef target")
	assert.Equal(t, refSite{
		idType: reflect.TypeFor[ir.TypeID](),
		id:     "t/x/Missing",
		path:   "doc.Types[t/x/Holder].Properties[0].Type.Target",
	}, sites[0])
}

// TestRefsCollector_KeepsNoResolvedKeyedRef holds the collector to settling a
// reference into a Document-keyed registry as the walk reaches it: one that
// resolves is not kept, so its path is never rendered.
func TestRefsCollector_KeepsNoResolvedKeyedRef(t *testing.T) {
	doc := docWithRef("t/x/Holder")
	assert.Empty(t, keptRefs(doc), "the holder's own ID and the target both resolve in types")
}

// TestRefsCollector_KeepsEveryDeclaredClassRef holds the other half: a declared
// class's registry is complete only once the walk is over, so a reference to one
// is kept whether or not it will resolve, the declaring node's own ID included.
func TestRefsCollector_KeepsEveryDeclaredClassRef(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{
		ID:     "s/x",
		Groups: []ir.OperationGroup{{ID: "g/x/G", Operations: []ir.Operation{{ID: "op/x/A", OverloadOf: new(ir.OpID("op/x/A"))}}}},
	}}}
	kept := keptRefs(doc)
	ids := make([]string, 0, len(kept))
	for _, s := range kept {
		ids = append(ids, s.path+"="+s.id)
	}
	assert.Equal(t, []string{
		"doc.Services[0].ID=s/x",
		"doc.Services[0].Groups[0].ID=g/x/G",
		"doc.Services[0].Groups[0].Operations[0].ID=op/x/A",
		"doc.Services[0].Groups[0].Operations[0].OverloadOf=op/x/A",
	}, ids)
}

func TestRefsCollector_SkipsEmptyIDs(t *testing.T) {
	assert.Empty(t, keptRefs(docWithRef("")), "an empty target is no reference")
}

// refViolations runs the reference check and drops the truncation flag, which
// the cases below assert nothing about.
func refViolations(doc *ir.Document) []Violation {
	vs, _ := runCheck(checkReferentialIntegrity, doc)
	return vs
}

func TestCheckReferentialIntegrity_DanglingTypeRef(t *testing.T) {
	got := refViolations(docWithRef("t/x/Missing"))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-type-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in types")
}

func TestCheckReferentialIntegrity_ResolvedRefIsClean(t *testing.T) {
	doc := docWithRef("t/x/Target")
	doc.Types["t/x/Target"] = &ir.Any{ID: "t/x/Target"}
	assert.Empty(t, refViolations(doc))
}

func TestCheckReferentialIntegrity_DanglingAuthRef(t *testing.T) {
	// A service references an auth scheme via AuthRequirement -> SchemeUse.Scheme
	// (an ir.AuthID). "a/missing" resolves in no Document.Auth entry.
	doc := &ir.Document{
		Services: []ir.Service{{
			Auth: []ir.AuthRequirement{{
				Schemes: []ir.SchemeUse{{Scheme: "a/missing"}},
			}},
		}},
	}
	got := refViolations(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-auth-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in auth")
}

func TestCheckReferentialIntegrity_DanglingMessageRef(t *testing.T) {
	// A channel references a message by identity; "m/x/missing" resolves in no
	// Document.Messages entry. The channel's own ID "c/x/Ch" is a definition (map
	// key + node ID) that resolves cleanly, so only the message branch fires.
	ch := ir.Channel{ID: "c/x/Ch", Messages: []ir.MessageID{"m/x/missing"}}
	doc := &ir.Document{
		Channels: map[ir.ChannelID]ir.Channel{ch.ID: ch},
	}
	got := refViolations(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-message-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in messages")
}

func TestCheckReferentialIntegrity_DanglingChannelRef(t *testing.T) {
	// An operation's message binding targets channel "c/missing", which resolves
	// in no Document.Channels entry, exercising the channels registry branch.
	doc := &ir.Document{
		Services: []ir.Service{{
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{
					ID:       "op/x/Send",
					Bindings: ir.OpBindings{Message: &ir.MessageBinding{Channel: "c/missing"}},
				}},
			}},
		}},
	}
	got := refViolations(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-channel-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in channels")
}

// docWithOverload builds a service whose one operation overloads target.
func docWithOverload(target ir.OpID) *ir.Document {
	return &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{
			{ID: "op/x/Get", OverloadOf: &target},
		}}},
	}}}
}

// TestCheckReferentialIntegrity_DanglingOpRef drives the class no map on Document
// covers. An operation is declared inside the Service→OperationGroup tree, so
// every OpID reference in the IR resolved against nothing until the registry was
// derived from those declarations instead (GitHub #50).
func TestCheckReferentialIntegrity_DanglingOpRef(t *testing.T) {
	got := refViolations(docWithOverload("op/x/Missing"))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-op-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in op declarations")
}

// TestCheckReferentialIntegrity_ResolvedOpRefIsClean is the other half: an
// operation's own ID resolves against its own declaration, and so does a
// reference naming it.
func TestCheckReferentialIntegrity_ResolvedOpRefIsClean(t *testing.T) {
	assert.Empty(t, refViolations(docWithOverload("op/x/Get")))
}

// TestCheckReferentialIntegrity_DanglingServiceRef drives the other class with no
// map: Document.Services is a slice, so a service extending an undeclared one
// went unreported the same way.
func TestCheckReferentialIntegrity_DanglingServiceRef(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x", Extends: []ir.ServiceID{"s/missing"}}}}

	got := refViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-service-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in service declarations")
}

func TestCheckReferentialIntegrity_ResolvedServiceRefIsClean(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{
		{ID: "s/base"},
		{ID: "s/x", Extends: []ir.ServiceID{"s/base"}},
	}}
	assert.Empty(t, refViolations(doc))
}

// TestCheckReferentialIntegrity_DanglingOpRefWithNoOperationDeclared drives the
// class no node in the document declares. A Smithy resource names its lifecycle
// operations, so an OpID reference exists in a document holding no ir.Operation
// at all — and a registry set read off the declarations rather than off the IR's
// shape would have no entry for the class, which is not "nothing declares this
// ID" but "this is no reference", the silence GitHub #50 is about.
func TestCheckReferentialIntegrity_DanglingOpRefWithNoOperationDeclared(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{
			Resource: &ir.ResourceInfo{InstanceOps: []ir.OpID{"op/x/Missing"}},
		}},
	}}}

	got := refViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-op-ref", got[0].Code)
	assert.Contains(t, got[0].Message, "does not resolve in op declarations")
}

// TestCheckReferentialIntegrity_TruncatedWalkClaimsNoDeclarations holds the one
// direction a declaration-derived registry can be wrong in. The document declares
// the operation its shallow reference names, but past the walk cap, so a registry
// built from what the walk reached answers "not declared" for a node that is —
// and a false dangling report is worse than the silence it replaced. The
// registries Document keys maps by are unaffected: they are read off the field,
// not off the walk.
func TestCheckReferentialIntegrity_TruncatedWalkClaimsNoDeclarations(t *testing.T) {
	deep, target := irtest.NestedOverloadDoc(ir.MaxWalkDepth)
	vs, truncated := runCheck(checkReferentialIntegrity, deep)
	assert.True(t, truncated, "the walk must report that it was cut short")
	assert.Empty(t, vs, "%s is declared past the cap, not undeclared", target)

	// The other half: the same shape within the cap does resolve, so the case
	// above is not passing on a walk that collects no reference at all.
	shallow, _ := irtest.NestedOverloadDoc(0)
	assert.Empty(t, refViolations(shallow))
}

// TestCheckReferentialIntegrity_PropIDStaysOutOfTheWalk pins the one class left
// out of the registries. A PropID names a position inside its model, and
// pass.Validate resolves one against the properties a document declares; adding a
// document-wide answer here would report one defect twice, under two codes.
func TestCheckReferentialIntegrity_PropIDStaysOutOfTheWalk(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
			ID:         "op/x/List",
			Pagination: &ir.Pagination{Items: &ir.PropPath{Segments: []ir.PropID{"p/x/ghost"}}},
		}}}},
	}}}
	assert.Empty(t, refViolations(doc))
}

func TestCheckReferentialIntegrity_DanglingRenameKey(t *testing.T) {
	// Service.Renames is map[TypeID]Naming: the KEY is a reference into
	// Document.Types. "t/x/ghost" resolves in no type, so the reference-typed map
	// key must be reported even though the entry's Naming value is well-formed.
	doc := &ir.Document{
		Types: ir.TypeRegistry{"t/x/M": &ir.Any{ID: "t/x/M"}},
		Services: []ir.Service{{
			Renames: map[ir.TypeID]ir.Naming{"t/x/ghost": {Source: "Ghost"}},
		}},
	}
	got := refViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-type-ref", got[0].Code)
	assert.Equal(t, "t/x/ghost", refIDInMessage(got[0].Message))
}

// refIDInMessage extracts the reference ID from a dangling-ref message of the
// form `reference "<id>" does not resolve in <registry>`. The ID is quoted in
// the message (GitHub #400) because it is document-derived text, so it is read
// back the way strconv.Quote wrote it rather than split on the first space —
// an ID or a registry key it embeds may itself hold one.
func refIDInMessage(msg string) string {
	const prefix = "reference "
	rest, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return ""
	}
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return ""
	}
	id, err := strconv.Unquote(quoted)
	if err != nil {
		return ""
	}
	return id
}

// aliasRuns is how many times TestVerify_AliasedPointerIsDeterministic repeats
// the same call. Go randomizes map iteration per range statement, so a two-entry
// registry disagrees with itself within a few runs; the flaw this pins split
// 431/69 across two outputs over this many.
const aliasRuns = 500

// TestVerify_AliasedPointerIsDeterministic pins invariant 7 against pointer
// aliasing. Both models share one *TypeRef, which the walk descends into at
// whichever registry entry it reaches first, so the surviving violation names
// t/a's Base on one run and t/b's on the next unless the walk's order is fixed.
// Verify's final sort cannot repair that: it is the violation set that varies,
// not merely its order.
func TestVerify_AliasedPointerIsDeterministic(t *testing.T) {
	t.Parallel()
	shared := &ir.TypeRef{Target: "t/ghost/aliased"}
	a := &ir.Model{ID: "t/a", Base: shared}
	b := &ir.Model{ID: "t/b", Base: shared}
	doc := &ir.Document{Types: ir.TypeRegistry{a.ID: a, b.ID: b}}

	want := fmt.Sprintf("%+v", Verify(doc))
	require.Contains(t, want, "t/ghost/aliased", "the shared target must dangle, or nothing is being compared")

	for i := range aliasRuns {
		require.Equal(t, want, fmt.Sprintf("%+v", Verify(doc)), "run %d disagrees with the first", i)
	}
}
