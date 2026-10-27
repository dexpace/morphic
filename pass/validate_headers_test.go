package pass_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// headerSerialized is a header property that sets both fields a header alone
// carries.
func headerSerialized() ir.Property {
	return ir.Property{
		ID: "p/h", WireName: "X-Ids", Type: ir.TypeRef{Target: "t/prim/string"},
		Style: "simple", Explode: new(true),
	}
}

// TestValidate_HeaderSerializationOutsideAHeader plants each field on each
// position that holds a property and is no headers list, and requires one
// error at that property.
func TestValidate_HeaderSerializationOutsideAHeader(t *testing.T) {
	t.Parallel()
	style := func(p *ir.Property) { p.Style = "simple" }
	explode := func(p *ir.Property) { p.Explode = new(false) }
	cases := []struct {
		name  string
		where string
		plant func(d *ir.Document, set func(*ir.Property))
	}{
		{"model property", "t/m/properties/0", func(d *ir.Document, set func(*ir.Property)) {
			set(&d.Types["t/m"].(*ir.Model).Properties[0])
		}},
		{"resource identifier", "g/r/resource/identifiers/0", func(d *ir.Document, set func(*ir.Property)) {
			id := ir.Property{ID: "p/id", WireName: "id", Type: ir.TypeRef{Target: "t/prim/string"}}
			set(&id)
			resourceGroup(d).Resource.Identifiers = []ir.Property{id}
		}},
		{"resource property", "g/r/resource/properties/0", func(d *ir.Document, set func(*ir.Property)) {
			prop := ir.Property{ID: "p/state", WireName: "state", Type: ir.TypeRef{Target: "t/prim/string"}}
			set(&prop)
			resourceGroup(d).Resource.Properties = []ir.Property{prop}
		}},
	}
	for _, tc := range cases {
		for field, set := range map[string]func(*ir.Property){"style": style, "explode": explode} {
			t.Run(tc.name+" "+field, func(t *testing.T) {
				t.Parallel()
				doc := validDoc()
				tc.plant(doc, set)
				found := withCode(validated(t, doc), "ir/header-serialization-outside-header")
				require.Len(t, found, 1, "exactly the planted property must be reported")
				assert.Equal(t, ir.SeverityError, found[0].Severity)
				assert.Equal(t, tc.where, found[0].Provenance.Node)
			})
		}
	}
}

// resourceGroup returns the service's one group, named g/r and carrying a
// resource, creating both on first use.
func resourceGroup(doc *ir.Document) *ir.OperationGroup {
	g := &service(doc).Groups[0]
	g.ID = "g/r"
	if g.Resource == nil {
		g.Resource = &ir.ResourceInfo{}
	}
	return g
}

// TestValidate_HeaderSerializationOnAHeaderIsClean is the silent half: the same
// fields on every headers list report nothing.
func TestValidate_HeaderSerializationOnAHeaderIsClean(t *testing.T) {
	t.Parallel()
	payload := multipartPayload(map[ir.PropID]ir.PartEncoding{
		"p/m/a": {Headers: []ir.Property{headerSerialized()}},
	})
	doc := docWithOperation(ir.Operation{
		ID:        "op",
		Request:   &payload,
		Responses: []ir.Response{{Name: ir.Naming{Source: "ok"}, Headers: []ir.Property{headerSerialized()}}},
		Errors:    []ir.ErrorCase{{Name: ir.Naming{Source: "bad"}, Headers: []ir.Property{headerSerialized()}}},
		Bindings:  ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "POST", URITemplate: "/x"}}},
	})
	assert.Empty(t, withCode(validated(t, doc), "ir/header-serialization-outside-header"))
}

// TestHeaderSerialization_NamesEveryPropertyCarrier fails when the IR declares a
// field carrying an ir.Property that is neither a headers list nor a position
// checkHeaderSerialization judges, which would leave a new position unjudged
// with the suite green.
func TestHeaderSerialization_NamesEveryPropertyCarrier(t *testing.T) {
	t.Parallel()
	listed := []string{
		// The headers lists, where Style and Explode belong.
		"ErrorCase.Headers", "PartEncoding.Headers", "Response.Headers",
		// The positions checkHeaderSerialization judges.
		"Model.Properties", "ResourceInfo.Identifiers", "ResourceInfo.Properties",
	}
	slices.Sort(listed)

	found := fieldsCarrying(t, reflect.TypeFor[ir.Property]())
	require.NotEmpty(t, found, "the shape holds no ir.Property field at all")
	assert.Empty(t, cmp.Diff(found, listed),
		"every ir field carrying an ir.Property is a headers list or judged by "+
			"checkHeaderSerialization (-declared +listed)")
}
