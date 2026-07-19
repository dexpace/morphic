package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestPtr_EscapesPerRFC6901(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		segments []string
		want     string
	}{
		{"plain", []string{"components", "schemas", "User"}, "/components/schemas/User"},
		{"slash in segment", []string{"paths", "/users/{id}", "get"}, "/paths/~1users~1{id}/get"},
		{"tilde in segment", []string{"components", "schemas", "a~b"}, "/components/schemas/a~0b"},
		{"tilde-slash order", []string{"x", "~/"}, "/x/~0~1"},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ptr(tc.segments...))
		})
	}
}

func TestRefTypeID_LocalAndExternal(t *testing.T) {
	t.Parallel()
	id, err := refTypeID("#/components/schemas/User")
	require.NoError(t, err)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/User"), id)

	id, err = refTypeID("#/paths/~1users/get/responses/200")
	require.NoError(t, err)
	assert.Equal(t, ir.TypeID("t/anon/paths/~1users/get/responses/200"), id)

	id, err = refTypeID("common.yaml#/components/schemas/Err")
	require.NoError(t, err)
	assert.Equal(t, ir.TypeID("t/openapi/ext/common.yaml#/components/schemas/Err"), id)

	_, err = refTypeID("")
	require.Error(t, err)
}
