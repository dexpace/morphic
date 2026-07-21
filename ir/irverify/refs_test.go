package irverify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// docWithRef builds a model whose property points at target via a TypeRef.
func docWithRef(target ir.TypeID) *ir.Document {
	holder := &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/x/Holder"},
		Properties: []ir.Property{{
			ID:   "p/x/Holder/f",
			Type: ir.TypeRef{Target: target},
		}},
	}
	return &ir.Document{Types: ir.TypeRegistry{holder.ID: holder}}
}

func TestCollectRefs_FindsTypeRefTarget(t *testing.T) {
	doc := docWithRef("t/x/Missing")
	sites, _ := collectRefs(doc)
	var found bool
	for _, s := range sites {
		if s.id == "t/x/Missing" && s.registry == "types" {
			found = true
		}
	}
	assert.True(t, found, "reflection walk should discover the property TypeRef target")
}

func TestCollectRefs_SkipsEmptyIDs(t *testing.T) {
	doc := docWithRef("") // empty target must not be collected
	sites, _ := collectRefs(doc)
	for _, s := range sites {
		assert.NotEqual(t, "", s.id)
	}
}

func TestCheckReferentialIntegrity_DanglingTypeRef(t *testing.T) {
	got := checkReferentialIntegrity(docWithRef("t/x/Missing"))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-type-ref", got[0].Code)
}

func TestCheckReferentialIntegrity_ResolvedRefIsClean(t *testing.T) {
	doc := docWithRef("t/x/Target")
	doc.Types["t/x/Target"] = &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x/Target"}}
	assert.Empty(t, checkReferentialIntegrity(doc))
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
	got := checkReferentialIntegrity(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-auth-ref", got[0].Code)
}

func TestCheckReferentialIntegrity_DanglingMessageRef(t *testing.T) {
	// A channel references a message by identity; "m/x/missing" resolves in no
	// Document.Messages entry. The channel's own ID "c/x/Ch" is a definition (map
	// key + node ID) that resolves cleanly, so only the message branch fires.
	ch := ir.Channel{ID: "c/x/Ch", Messages: []ir.MessageID{"m/x/missing"}}
	doc := &ir.Document{
		Channels: map[ir.ChannelID]ir.Channel{ch.ID: ch},
	}
	got := checkReferentialIntegrity(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-message-ref", got[0].Code)
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
	got := checkReferentialIntegrity(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-channel-ref", got[0].Code)
}

func TestCheckReferentialIntegrity_DanglingRenameKey(t *testing.T) {
	// Service.Renames is map[TypeID]Naming: the KEY is a reference into
	// Document.Types. "t/x/ghost" resolves in no type, so the reference-typed map
	// key must be reported even though the entry's Naming value is well-formed.
	doc := &ir.Document{
		Types: ir.TypeRegistry{"t/x/M": &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x/M"}}},
		Services: []ir.Service{{
			Renames: map[ir.TypeID]ir.Naming{"t/x/ghost": {Source: "Ghost"}},
		}},
	}
	got := checkReferentialIntegrity(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/dangling-type-ref", got[0].Code)
	assert.Equal(t, "t/x/ghost", refIDInMessage(got[0].Message))
}

func TestCheckReferentialIntegrity_DeepValueTreeReportsTruncation(t *testing.T) {
	// A Value tree nested past maxWalkDepth must not be silently under-checked: the
	// bounded walk reports truncation rather than claiming the document is clean.
	v := ir.Value{Kind: ir.ValueNull}
	for range 2 * maxWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	deep := v
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID:       "t/x/M",
		Examples: []ir.Example{{Value: &deep}},
	}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	got := checkReferentialIntegrity(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/walk-truncated", got[0].Code)
}

func TestResolves_UnknownRegistryIsUnresolved(t *testing.T) {
	// A refSite naming a registry the resolver does not handle falls through to the
	// default arm and reports the reference as unresolved.
	assert.False(t, resolves(&ir.Document{}, refSite{id: "x", registry: "bogus"}))
}

func TestCollectRefs_SharedPointerVisitedOnce(t *testing.T) {
	// The same *TypeRef reachable through two template arguments must trip the
	// cycle guard: the walk descends into it once and skips the repeat visit, so
	// its target is discovered exactly once.
	shared := &ir.TypeRef{Target: "t/x/Shared"}
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID: "t/x/M",
		Instantiation: &ir.TemplateInstantiation{Args: []ir.TemplateArg{
			{Type: shared},
			{Type: shared},
		}},
	}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	sites, truncated := collectRefs(doc)
	assert.False(t, truncated)

	var count int
	for _, s := range sites {
		if s.id == "t/x/Shared" {
			count++
		}
	}
	assert.Equal(t, 1, count, "the shared pointer's target is collected once, not per reference")
}

// refIDInMessage extracts the reference ID from a dangling-ref message of the
// form "reference <id> does not resolve in <registry>".
func refIDInMessage(msg string) string {
	const prefix = "reference "
	rest, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, " ")
	return id
}
