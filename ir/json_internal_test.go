package ir

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTypeDef_MarkerMethods exercises the unexported sealed-interface marker on
// every concrete TypeDef kind. The markers carry no behavior; calling them keeps
// the sealed-sum contract honest and observable to coverage.
func TestTypeDef_MarkerMethods(t *testing.T) {
	t.Parallel()
	defs := []TypeDef{
		&Primitive{},
		&Scalar{},
		&Model{},
		&Union{},
		&Enum{},
		&List{},
		&MapT{},
		&Tuple{},
		&Literal{},
		&External{},
		&Any{},
	}
	for _, td := range defs {
		t.Run(string(td.Kind()), func(t *testing.T) {
			t.Parallel()
			// typeDef is the unexported sealing marker on the TypeDef interface;
			// invoking it keeps the sealed-sum contract observable. The bodies are
			// empty (zero statements), so this documents rather than covers them.
			td.typeDef()
		})
	}
}

// TestStringMember_Branches pins every return path of stringMember directly:
// the document and registry decoders reach it only with input a decoder has
// already read, so its refusals of malformed input are otherwise unexercised.
func TestStringMember_Branches(t *testing.T) {
	t.Parallel()

	t.Run("malformed input at the first token", func(t *testing.T) {
		t.Parallel()
		_, _, err := stringMember(jsontext.Value(""), "irVersion")
		require.Error(t, err)
	})

	t.Run("not an object", func(t *testing.T) {
		t.Parallel()
		_, found, err := stringMember(jsontext.Value("42"), "irVersion")
		require.Error(t, err)
		assert.False(t, found)
		assert.ErrorContains(t, err, "want a JSON object, got")
	})

	t.Run("malformed key mid-scan", func(t *testing.T) {
		t.Parallel()
		// PeekKind sees the opening quote and enters the loop, but the key
		// string itself is never closed.
		_, _, err := stringMember(jsontext.Value(`{"ab`), "irVersion")
		require.Error(t, err)
	})

	t.Run("skipping a non-matching key fails on a malformed value", func(t *testing.T) {
		t.Parallel()
		_, _, err := stringMember(jsontext.Value(`{"other":`), "irVersion")
		require.Error(t, err)
	})

	t.Run("matching key has a malformed value", func(t *testing.T) {
		t.Parallel()
		_, _, err := stringMember(jsontext.Value(`{"irVersion":`), "irVersion")
		require.Error(t, err)
	})

	t.Run("member present but not a string", func(t *testing.T) {
		t.Parallel()
		_, found, err := stringMember(jsontext.Value(`{"irVersion":123}`), "irVersion")
		require.Error(t, err)
		assert.False(t, found)
		assert.ErrorContains(t, err, `"irVersion" is a JSON number, not a string`)
	})

	t.Run("member found on the first key", func(t *testing.T) {
		t.Parallel()
		v, found, err := stringMember(jsontext.Value(`{"irVersion":"0.5.0"}`), "irVersion")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "0.5.0", v)
	})

	t.Run("member found after skipping others", func(t *testing.T) {
		t.Parallel()
		v, found, err := stringMember(jsontext.Value(`{"a":1,"b":{"nested":true},"irVersion":"0.5.0"}`), "irVersion")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "0.5.0", v)
	})

	t.Run("an object that never ends after a whole member", func(t *testing.T) {
		t.Parallel()
		// Every member reads cleanly, so only the missing close can report it;
		// without that read this was indistinguishable from an absent member.
		_, found, err := stringMember(jsontext.Value(`{"a":1`), "irVersion")
		require.Error(t, err)
		assert.False(t, found)
	})

	t.Run("member absent from an otherwise well-formed object", func(t *testing.T) {
		t.Parallel()
		v, found, err := stringMember(jsontext.Value(`{"a":1,"b":2}`), "irVersion")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, v)
	})
}

// TestNamed pins named's rewrite in isolation: it retags a *json.SemanticError
// whose GoType is exactly the internal type it is given to read P instead, and
// leaves every other error — nil, a non-SemanticError, or a SemanticError
// naming an unrelated type — untouched.
func TestNamed(t *testing.T) {
	t.Parallel()

	t.Run("rewrites a SemanticError naming the internal type", func(t *testing.T) {
		t.Parallel()
		type alias Model
		serr := &json.SemanticError{GoType: reflect.TypeFor[kinded[alias]]()}
		got := named[Model](serr, reflect.TypeFor[kinded[alias]]())
		gotSerr, ok := errors.AsType[*json.SemanticError](got)
		require.True(t, ok)
		assert.Equal(t, reflect.TypeFor[Model](), gotSerr.GoType)
	})

	t.Run("leaves a SemanticError naming an unrelated type untouched", func(t *testing.T) {
		t.Parallel()
		type alias Model
		serr := &json.SemanticError{GoType: reflect.TypeFor[int]()}
		got := named[Model](serr, reflect.TypeFor[kinded[alias]]())
		gotSerr, ok := errors.AsType[*json.SemanticError](got)
		require.True(t, ok)
		assert.Equal(t, reflect.TypeFor[int](), gotSerr.GoType,
			"GoType must be left alone when it doesn't name the internal type")
	})

	t.Run("leaves a non-SemanticError untouched", func(t *testing.T) {
		t.Parallel()
		type alias Model
		plain := errors.New("boom")
		got := named[Model](plain, reflect.TypeFor[kinded[alias]]())
		assert.Same(t, plain, got)
	})

	t.Run("leaves nil untouched", func(t *testing.T) {
		t.Parallel()
		type alias Model
		// marshalKinded calls named on every successful marshal, err included,
		// so this is the most common call of all — worth pinning directly
		// rather than leaving it to be exercised only incidentally.
		got := named[Model](nil, reflect.TypeFor[kinded[alias]]())
		assert.NoError(t, got)
	})
}

// TestDecodeTypeDef pins decodeTypeDef's own contract directly: it returns a
// nil TypeDef alongside every error, and it threads its opts parameter
// through to the nested decode rather than deciding leniency itself.
func TestDecodeTypeDef(t *testing.T) {
	t.Parallel()

	t.Run("decodes a known kind", func(t *testing.T) {
		t.Parallel()
		td, err := decodeTypeDef("t/x", jsontext.Value(`{"kind":"primitive","prim":"string"}`), nil)
		require.NoError(t, err)
		require.NotNil(t, td)
		assert.Equal(t, KindPrimitive, td.Kind())
		prim, ok := td.(*Primitive)
		require.True(t, ok)
		assert.Equal(t, PrimString, prim.Prim)
	})

	t.Run("refuses an entry with no kind member", func(t *testing.T) {
		t.Parallel()
		td, err := decodeTypeDef("t/x", jsontext.Value(`{}`), nil)
		require.Error(t, err)
		assert.Nil(t, td)
		assert.ErrorContains(t, err, `no "kind" member`)
	})

	t.Run("refuses a non-string kind", func(t *testing.T) {
		t.Parallel()
		td, err := decodeTypeDef("t/x", jsontext.Value(`{"kind":123}`), nil)
		require.Error(t, err)
		assert.Nil(t, td)
		assert.ErrorContains(t, err, `is a JSON number, not a string`)
	})

	t.Run("refuses a non-object entry", func(t *testing.T) {
		t.Parallel()
		td, err := decodeTypeDef("t/x", jsontext.Value(`123`), nil)
		require.Error(t, err)
		assert.Nil(t, td)
		assert.ErrorContains(t, err, "reading kind tag:")
	})

	t.Run("refuses an unknown kind", func(t *testing.T) {
		t.Parallel()
		td, err := decodeTypeDef("t/x", jsontext.Value(`{"kind":"nope"}`), nil)
		require.Error(t, err)
		assert.Nil(t, td)
		assert.ErrorContains(t, err, `unknown kind "nope"`)
	})

	t.Run("prefixes a body mismatch with the id and kind", func(t *testing.T) {
		t.Parallel()
		td, err := decodeTypeDef("t/x", jsontext.Value(`{"kind":"primitive","prim":123}`), nil)
		require.Error(t, err)
		assert.Nil(t, td)
		assert.ErrorContains(t, err, "t/x (primitive):")
	})

	t.Run("threads opts through to the body decode", func(t *testing.T) {
		t.Parallel()
		raw := jsontext.Value(`{"kind":"primitive","prim":"string","bogus":1}`)

		_, err := decodeTypeDef("t/x", raw, nil)
		assert.NoError(t, err, "an unknown member is ignored when opts carries no RejectUnknownMembers")

		_, err = decodeTypeDef("t/x", raw, json.RejectUnknownMembers(true))
		require.Error(t, err, "the same unknown member must be refused once opts asks for it")
		assert.ErrorContains(t, err, "t/x (primitive):")
	})
}
