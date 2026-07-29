package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestService_JSONContract pins Service's omitempty contract: Name, Docs,
// Auth, and Provenance carry no omitempty. Auth is the load-bearing one — an
// empty non-nil slice ("explicitly public") must differ from nil ("no
// service-level default"), so the field cannot be dropped from the wire just
// because it happens to be empty. It also pins that a fully populated
// Service — inheritance, groups, auth, common errors, protocols, renames, and
// server indices — round-trips.
func TestService_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Service{},
		`{"name":{},"docs":{},"auth":null,"provenance":{"source":0}}`,
		ir.Service{
			ID:        "s/openapi/petstore",
			Name:      populatedNaming(),
			Docs:      populatedDocs(),
			Version:   "1.2.0",
			Namespace: []string{"com", "example", "petstore"},
			Extends:   []ir.ServiceID{"s/base1", "s/base2"},
			Groups: []ir.OperationGroup{
				{Name: ir.Naming{Source: "pets"}},
				{Name: ir.Naming{Source: "owners"}},
			},
			Auth: []ir.AuthRequirement{
				{Schemes: []ir.SchemeUse{{Scheme: "auth/apiKey"}}},
			},
			CommonErrors: []ir.ErrorCase{
				{Type: populatedTypeRef(), Fault: "client"},
				{Type: populatedTypeRef(), Fault: "server"},
			},
			Protocols: []ir.ProtocolDecl{
				{Name: "aws.restJson1", Options: populatedRawConfig()},
				{Name: "grpc"},
			},
			Renames: map[ir.TypeID]ir.Naming{
				"t/z": {Source: "ZRenamed"},
				"t/a": {Source: "ARenamed"},
			},
			Servers:    []int{0, 1},
			Unmodeled:  populatedUnmodeled(),
			Provenance: populatedProvenance(),
		})
}

// TestService_AuthEmptyNonNilRoundTrips mirrors the Operation and Server
// versions of this test: Service.Auth must serialize an empty non-nil slice
// as [] and must not collapse it to nil on decode.
func TestService_AuthEmptyNonNilRoundTrips(t *testing.T) {
	t.Parallel()
	svc := ir.Service{Auth: []ir.AuthRequirement{}}
	raw, err := json.Marshal(svc)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"auth":[]`)

	var back ir.Service
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Auth)
	assert.Empty(t, back.Auth)
}

// TestService_RenamesDeterministic pins Class C for Service's TypeID-keyed
// map: Renames must marshal with keys in sorted order on every run, even
// though the key type is TypeID rather than string.
func TestService_RenamesDeterministic(t *testing.T) {
	t.Parallel()
	svc := ir.Service{
		Renames: map[ir.TypeID]ir.Naming{
			"t/z-type": {Source: "Z"},
			"t/m-type": {Source: "M"},
			"t/a-type": {Source: "A"},
			"t/q-type": {Source: "Q"},
			"t/b-type": {Source: "B"},
		},
	}
	got := assertDeterministicMarshal(t, svc)
	assert.Contains(t, got,
		`"renames":{"t/a-type":{"source":"A"},"t/b-type":{"source":"B"},"t/m-type":{"source":"M"},`+
			`"t/q-type":{"source":"Q"},"t/z-type":{"source":"Z"}}`)
}

// TestProtocolDecl_JSONContract pins ProtocolDecl's omitempty contract (both
// fields are optional) and that a populated ProtocolDecl round-trips.
func TestProtocolDecl_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ProtocolDecl{}, `{}`,
		ir.ProtocolDecl{Name: "aws.restJson1", Options: populatedRawConfig()})
}

// TestOperationGroup_JSONContract pins OperationGroup's omitempty contract
// (Name and Docs carry no omitempty, every other field is optional including
// the recursive Groups slice) and that a fully populated OperationGroup —
// nested sub-groups, operations, resource info, and availability —
// round-trips, including recursion through Groups.
func TestOperationGroup_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.OperationGroup{}, `{"name":{},"docs":{}}`, ir.OperationGroup{
		Name: populatedNaming(),
		Docs: populatedDocs(),
		Groups: []ir.OperationGroup{
			{Name: ir.Naming{Source: "sub1"}},
			{Name: ir.Naming{Source: "sub2"}},
		},
		Operations: []ir.Operation{
			{ID: "op/list", Name: ir.Naming{Source: "list"}, Bindings: ir.OpBindings{}, Auth: []ir.AuthRequirement{}},
		},
		Resource: &ir.ResourceInfo{
			Identifiers: []ir.Property{populatedProperty()},
			Lifecycle:   map[string]ir.OpID{"create": "op/create", "read": "op/read"},
		},
		Availability: populatedAvailability(),
		Unmodeled:    populatedUnmodeled(),
	})
}

// TestResourceInfo_JSONContract pins ResourceInfo's contract that NoReplace
// carries no omitempty ("put may replace" is a declared fact about the
// resource, not an absence) and that a fully populated ResourceInfo —
// identifiers, properties, lifecycle map, and both operation-id slices —
// round-trips.
func TestResourceInfo_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ResourceInfo{}, `{"noReplace":false}`, ir.ResourceInfo{
		Identifiers:   []ir.Property{{ID: "p/id"}},
		Properties:    []ir.Property{{ID: "p/name"}, {ID: "p/age"}},
		Lifecycle:     map[string]ir.OpID{"create": "op/create", "read": "op/read", "delete": "op/delete"},
		NoReplace:     true,
		InstanceOps:   []ir.OpID{"op/get", "op/update"},
		CollectionOps: []ir.OpID{"op/list", "op/create"},
	})
}

// TestResourceInfo_LifecycleDeterministic pins Class C for ResourceInfo's map
// field: Lifecycle must marshal with keys in sorted order on every run.
func TestResourceInfo_LifecycleDeterministic(t *testing.T) {
	t.Parallel()
	info := ir.ResourceInfo{
		Lifecycle: map[string]ir.OpID{
			"z-op": "op/z",
			"m-op": "op/m",
			"a-op": "op/a",
			"q-op": "op/q",
			"b-op": "op/b",
		},
	}
	got := assertDeterministicMarshal(t, info)
	assert.Contains(t, got,
		`"lifecycle":{"a-op":"op/a","b-op":"op/b","m-op":"op/m","q-op":"op/q","z-op":"op/z"}`)
}
