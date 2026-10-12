package operation

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFirstDeclared_KeepsEachKeysFirstDeclaration pins the index precedence
// reads a farther layer through: one entry per key, the first declaration of
// it, which is the one a collision finding names.
func TestFirstDeclared_KeepsEachKeysFirstDeclaration(t *testing.T) {
	t.Parallel()
	got := firstDeclared([]itemConstruct{
		{key: "k", rel: "/first"},
		{key: "other", rel: "/other"},
		{key: "k", rel: "/second"},
	})

	assert.Len(t, got, 2)
	assert.Equal(t, jsontext.Pointer("/first"), got["k"].rel, "a repeated key keeps its first declaration")
	assert.Equal(t, jsontext.Pointer("/other"), got["other"].rel)
	assert.Empty(t, firstDeclared(nil))
}
