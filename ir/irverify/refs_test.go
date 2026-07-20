package irverify

import (
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
	sites := collectRefs(doc)
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
	for _, s := range collectRefs(doc) {
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

func TestCheckReferentialIntegrity_DanglingChannelAndMessageRefs(t *testing.T) {
	// A channel references a message by identity; a discriminator maps to a type.
	ch := ir.Channel{ID: "c/x/Ch", Messages: []ir.MessageID{"m/x/missing"}}
	doc := &ir.Document{
		Channels: map[ir.ChannelID]ir.Channel{ch.ID: ch},
	}
	got := checkReferentialIntegrity(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/dangling-message-ref", got[0].Code)
}
