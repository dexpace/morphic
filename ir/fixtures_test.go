package ir_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPopulatedFixtures_LeaveNoFieldZero holds every populated* fixture to the
// "every field non-zero" its doc comment claims. The fixtures are the round-trip
// half of the JSON-contract tests, and a field a fixture leaves zero is a field
// those tests say nothing about: Encoding.Schema shipped with its codec
// unasserted because populatedEncoding was not extended with it, and retagging
// the field json:"-" left the package green.
//
// Sums and maps are deliberately out: populatedValue sets only the payload its
// Kind selects, and a map fixture has no fields to leave zero.
func TestPopulatedFixtures_LeaveNoFieldZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		fixture any
		// zeroOK names the fields a fixture leaves zero on purpose, against the
		// reason. An entry is a claim a reviewer has to agree with, which is the
		// point of spelling it here rather than dropping the fixture from the
		// table: the rest of its fields stay covered. It is asserted in both
		// directions, so an entry cannot outlive its reason.
		zeroOK map[string]string
	}{
		{"Naming", populatedNaming(), nil},
		{"Docs", populatedDocs(), nil},
		{"Provenance", populatedProvenance(), nil},
		{"Deprecation", populatedDeprecation(), nil},
		{"Availability", populatedAvailability(), nil},
		{"Constraints", populatedConstraints(), nil},
		{"Encoding", populatedEncoding(), nil},
		{"XMLHints", populatedXMLHints(), nil},
		{"TypeRef", populatedTypeRef(), nil},
		{"TypeCommon", populatedTypeCommon("t/openapi/components/schemas/User"), nil},
		{"Property", populatedProperty(), map[string]string{
			"EventPayload": "mutually exclusive with EventHeader, which the fixture sets; " +
				"neither field carries omitempty, so both serialize either way",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rv := reflect.Indirect(reflect.ValueOf(tc.fixture))
			require.Equal(t, reflect.Struct, rv.Kind(), "fixture must be a struct or a pointer to one")
			require.Positive(t, rv.NumField(), "a struct with no fields witnesses nothing")
			for i := range rv.NumField() {
				name := rv.Type().Field(i).Name
				if why, exempt := tc.zeroOK[name]; exempt {
					assert.Truef(t, rv.Field(i).IsZero(),
						"%s.%s is listed as deliberately zero (%s) but the fixture sets it; drop the entry",
						rv.Type().Name(), name, why)
					continue
				}
				assert.Falsef(t, rv.Field(i).IsZero(),
					"%s.%s is left zero, so the %s round-trip asserts nothing about it",
					rv.Type().Name(), name, rv.Type().Name())
			}
		})
	}
}
