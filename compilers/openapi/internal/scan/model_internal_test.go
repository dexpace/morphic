package scan

import (
	"reflect"
	"testing"

	oas3core "github.com/speakeasy-api/openapi/jsonschema/oas3/core"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
)

// TestSubSchemaKeys_AreTheModelsSchemaFields holds the three sub-schema key
// sets to the library's schema model: each set is the keys of the fields
// holding one schema, a list of them or a map of them, and every field holding
// a schema is one of those three. A key the model lacks names raw YAML, where
// the resolver resolves nothing and its walk reads a trailing empty token
// otherwise, so a set wider than the model refuses documents that compile.
func TestSubSchemaKeys_AreTheModelsSchemaFields(t *testing.T) {
	t.Parallel()
	shapes := map[reflect.Type]map[string]bool{
		reflect.TypeFor[marshaller.Node[oas3core.JSONSchema]]():                            {},
		reflect.TypeFor[marshaller.Node[[]oas3core.JSONSchema]]():                          {},
		reflect.TypeFor[marshaller.Node[*sequencedmap.Map[string, oas3core.JSONSchema]]](): {},
	}
	// A type argument is spelled with its full import path, so this is how any
	// shape holding a schema spells it.
	model := reflect.TypeFor[oas3core.Schema]()
	schema := model.PkgPath() + "." + model.Name()
	for f := range model.Fields() {
		if keys, ok := shapes[f.Type]; ok {
			keys[f.Tag.Get("key")] = true
			continue
		}
		assert.NotContains(t, f.Type.String(), schema, "%s holds schemas in a shape the scan does not read", f.Name)
	}

	assert.Equal(t, shapes[reflect.TypeFor[marshaller.Node[oas3core.JSONSchema]]()], subSchemaObjectKeys)
	assert.Equal(t, shapes[reflect.TypeFor[marshaller.Node[[]oas3core.JSONSchema]]()], subSchemaListKeys)
	assert.Equal(t, shapes[reflect.TypeFor[marshaller.Node[*sequencedmap.Map[string, oas3core.JSONSchema]]]()],
		subSchemaMapKeys)
}
