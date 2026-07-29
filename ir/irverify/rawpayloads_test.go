package irverify_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// allReasons is the closed reason set from ir/preserved.go, listed by name so
// deleting or renaming a constant breaks the build here. ReasonOutOfScope is
// included even though no compiler writes it yet: it is declared, so the
// verifier must accept it — an unwritten reason is a gap in the compilers, not
// in the enum.
var allReasons = []ir.UnmodeledReason{
	ir.ReasonVendorExtension, ir.ReasonValidationOnly, ir.ReasonDegradedLowering,
	ir.ReasonNoIRHome, ir.ReasonOutOfScope,
}

// docPreserving hangs p off validDoc's model, an Unmodeled site the walk reaches
// through the Types registry.
func docPreserving(p ir.Unmodeled) *ir.Document {
	doc := validDoc()
	doc.Types["t/x/Model"].Common().Unmodeled = p
	return doc
}

// TestVerify_EveryDeclaredReasonIsClean pins the negative half of the reason
// check: none of the declared values may be reported, or the check would fail
// documents it exists to pass.
func TestVerify_EveryDeclaredReasonIsClean(t *testing.T) {
	p := ir.Unmodeled{}
	for _, r := range allReasons {
		p["openapi:"+string(r)] = ir.UnmodeledEntry{Reason: r, Value: ir.RawValue(`1`)}
	}
	assert.Empty(t, irverify.Verify(docPreserving(p)))
}

// TestVerify_EmptyPreserveReasonIsAViolation is the mutation the audit found
// the verifier blind to: an entry that round-trips clean and passes
// pass.Validate while carrying a reason no consumer's switch can route.
func TestVerify_EmptyPreserveReasonIsAViolation(t *testing.T) {
	doc := docPreserving(ir.Unmodeled{
		"openapi:x-rate-limit": {Value: ir.RawValue(`100`)},
	})
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-unmodeled-reason", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/Model].Unmodeled[openapi:x-rate-limit]", got[0].Path)
}

// TestVerify_UnknownPreserveReasonIsAViolation covers the other way a bare
// string enum goes wrong: a value that deserializes happily but names no
// declared reason.
func TestVerify_UnknownPreserveReasonIsAViolation(t *testing.T) {
	doc := docPreserving(ir.Unmodeled{
		"openapi:x-rate-limit": {Reason: "totally_invented", Value: ir.RawValue(`100`)},
	})
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/unknown-unmodeled-reason", got[0].Code)
	assert.Contains(t, got[0].Message, "totally_invented")
}

// TestVerify_EmptyPreservedKeyIsAViolation checks the key half alone: a valid
// reason must not mask an unlookupable key.
func TestVerify_EmptyPreservedKeyIsAViolation(t *testing.T) {
	doc := docPreserving(ir.Unmodeled{
		"": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)},
	})
	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/empty-unmodeled-key", got[0].Code)
	assert.Equal(t, `doc.Types[t/x/Model].Unmodeled[""]`, got[0].Path)
}

// TestVerify_EmptyKeyAndReasonReportBoth pins that the two defects are checked
// independently, so a doubly-broken entry names both rather than stopping at
// the first.
func TestVerify_EmptyKeyAndReasonReportBoth(t *testing.T) {
	doc := docPreserving(ir.Unmodeled{"": {Value: ir.RawValue(`1`)}})
	codes := codesOf(irverify.Verify(doc))
	assert.Contains(t, codes, "ir/empty-unmodeled-key")
	assert.Contains(t, codes, "ir/empty-unmodeled-reason")
}

// TestVerify_PreservedIsCheckedBelowTheTopLevel confirms the check rides the
// generic walk rather than a hand-listed set of carriers: the same defect must
// be found on a nested Unmodeled map no registry walk would reach.
func TestVerify_PreservedIsCheckedBelowTheTopLevel(t *testing.T) {
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID: "s/x/S",
		Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{
				ID:        "o/x/S/op",
				Unmodeled: ir.Unmodeled{"openapi:x-internal": {Value: ir.RawValue(`true`)}},
			}},
		}},
	}}
	assert.Contains(t, codesOf(irverify.Verify(doc)), "ir/empty-unmodeled-reason")
}

// badPayloads is every shape of ir.RawValue that is not a JSON value. Empty is
// the one a nil guard lets through, and nil is the one that marshals but does
// not come back equal.
var badPayloads = map[string]ir.RawValue{
	"malformed": ir.RawValue(`{not json}`),
	"empty":     {}, // non-nil and zero length: the state a nil guard admits
	"nil":       nil,
}

// TestVerify_InvalidPreservedValueIsAViolation covers the field the preserved
// check used to skip: an entry whose key and reason are both fine, carrying
// bytes the document cannot be marshaled with.
func TestVerify_InvalidPreservedValueIsAViolation(t *testing.T) {
	for name, payload := range badPayloads {
		t.Run(name, func(t *testing.T) {
			doc := docPreserving(ir.Unmodeled{
				"openapi:x-rate-limit": {Reason: ir.ReasonVendorExtension, Value: payload},
			})
			got := irverify.Verify(doc)
			require.Len(t, got, 1)
			assert.Equal(t, "ir/invalid-raw-value", got[0].Code)
			assert.Equal(t, "doc.Types[t/x/Model].Unmodeled[openapi:x-rate-limit]", got[0].Path)
			assert.Contains(t, got[0].Message, "unmodeled entry")
		})
	}
}

// TestVerify_ValidPreservedValuesAreClean pins the negative half across the JSON
// value kinds a preserved construct can be, so the check cannot pass by
// rejecting everything.
func TestVerify_ValidPreservedValuesAreClean(t *testing.T) {
	p := ir.Unmodeled{}
	for i, raw := range []string{`null`, `true`, `12.5`, `"s"`, `[1,2]`, `{"a":{"b":[]}}`, ` 1 `} {
		p["openapi:x-"+string(rune('a'+i))] = ir.UnmodeledEntry{
			Reason: ir.ReasonVendorExtension,
			Value:  ir.RawValue(raw),
		}
	}
	assert.Empty(t, irverify.Verify(docPreserving(p)))
}

// rawConfigCarriers builds one document per field that carries an ir.RawConfig,
// each holding payload at that field, paired with the path the violation must
// name. RawConfig is the half of the escape hatch nothing guarded: no compiler
// writes one yet, and these five fields are where an AsyncAPI compiler will.
func rawConfigCarriers(payload ir.RawValue) map[string]struct {
	doc  *ir.Document
	path string
} {
	cfg := map[string]ir.RawConfig{"kafka": {"clientId": payload}}
	server := validDoc()
	server.Servers = []ir.Server{{URLTemplate: "https://x", Bindings: cfg}}
	channel := validDoc()
	channel.Channels = map[ir.ChannelID]ir.Channel{"chan/a": {ID: "chan/a", Bindings: cfg}}
	message := validDoc()
	message.Messages = map[ir.MessageID]ir.Message{"msg/a": {ID: "msg/a", Bindings: cfg}}
	operation := validDoc()
	operation.Services = []ir.Service{{ID: "s/x/S", Groups: []ir.OperationGroup{{
		Operations: []ir.Operation{{
			ID:       "o/x/S/op",
			Bindings: ir.OpBindings{Message: &ir.MessageBinding{Bindings: cfg}},
		}},
	}}}}
	protocol := validDoc()
	protocol.Services = []ir.Service{{
		ID:        "s/x/S",
		Protocols: []ir.ProtocolDecl{{Name: "grpc", Options: ir.RawConfig{"clientId": payload}}},
	}}

	return map[string]struct {
		doc  *ir.Document
		path string
	}{
		"Server.Bindings":         {server, "doc.Servers[0].Bindings[kafka][clientId]"},
		"Channel.Bindings":        {channel, "doc.Channels[chan/a].Bindings[kafka][clientId]"},
		"Message.Bindings":        {message, "doc.Messages[msg/a].Bindings[kafka][clientId]"},
		"MessageBinding.Bindings": {operation, "doc.Services[0].Groups[0].Operations[0].Bindings.Message.Bindings[kafka][clientId]"},
		"ProtocolDecl.Options":    {protocol, "doc.Services[0].Protocols[0].Options[clientId]"},
	}
}

// TestVerify_InvalidRawConfigValueIsAViolation drives every RawConfig carrier in
// the IR with every payload shape that is not a JSON value.
func TestVerify_InvalidRawConfigValueIsAViolation(t *testing.T) {
	for payloadName, payload := range badPayloads {
		for carrier, tc := range rawConfigCarriers(payload) {
			t.Run(carrier+"/"+payloadName, func(t *testing.T) {
				got := irverify.Verify(tc.doc)
				require.Len(t, got, 1)
				assert.Equal(t, "ir/invalid-raw-value", got[0].Code)
				assert.Equal(t, tc.path, got[0].Path)
				assert.Contains(t, got[0].Message, "protocol config")
			})
		}
	}
}

// TestVerify_ValidRawConfigIsClean pins the negative half at every carrier: a
// declared protocol binding holding real JSON must pass everywhere.
func TestVerify_ValidRawConfigIsClean(t *testing.T) {
	for carrier, tc := range rawConfigCarriers(ir.RawValue(`{"a":1}`)) {
		t.Run(carrier, func(t *testing.T) {
			assert.Empty(t, irverify.Verify(tc.doc))
		})
	}
}

// TestVerify_InvalidRawValueIsWhatBreaksTheDocument ties the check to the
// invariant it protects rather than to its own definition of valid. Malformed
// and empty payloads fail json.Marshal for the whole document — one entry
// anywhere costs the entire artifact — and a nil payload marshals but decodes
// back as the four bytes "null", so the document no longer round-trips to an
// equal one (invariant #7).
func TestVerify_InvalidRawValueIsWhatBreaksTheDocument(t *testing.T) {
	for name, payload := range badPayloads {
		t.Run(name, func(t *testing.T) {
			doc := docPreserving(ir.Unmodeled{
				"openapi:x": {Reason: ir.ReasonVendorExtension, Value: payload},
			})
			require.Equal(t, []string{"ir/invalid-raw-value"}, codesOf(irverify.Verify(doc)))

			encoded, err := json.Marshal(doc)
			if payload == nil {
				require.NoError(t, err)
				var back ir.Document
				require.NoError(t, json.Unmarshal(encoded, &back))
				assert.Equal(t, ir.RawValue("null"),
					back.Types["t/x/Model"].Common().Unmodeled["openapi:x"].Value,
					"a nil payload does not come back as one")
				return
			}
			require.Error(t, err, "the whole document must fail to marshal")
		})
	}
}

// TestPreservedEntryFields_MatchTheIRShape guards the two fields
// checkRawPayloads reads by name, the coupling the Go compiler cannot check.
// Reason is read as a string and Value as a byte slice, and Bytes() panics on
// the zero reflect.Value a rename would leave behind — so a rename in ir must
// fail here rather than inside Verify on the first document that carries a
// preserved entry.
func TestPreservedEntryFields_MatchTheIRShape(t *testing.T) {
	t.Parallel()
	entry := reflect.TypeOf(ir.UnmodeledEntry{})
	for field, kind := range map[string]reflect.Kind{
		"Reason": reflect.String,
		"Value":  reflect.Slice,
	} {
		f, ok := entry.FieldByName(field)
		require.True(t, ok, "UnmodeledEntry has no field %s, which checkRawPayloads reads by name", field)
		assert.Equal(t, kind, f.Type.Kind(), "UnmodeledEntry.%s", field)
	}
	value, _ := entry.FieldByName("Value")
	assert.Equal(t, reflect.Uint8, value.Type.Elem().Kind(), "UnmodeledEntry.Value must stay a byte slice")
	assert.Equal(t, reflect.Uint8, reflect.TypeOf(ir.RawConfig(nil)).Elem().Elem().Kind(),
		"RawConfig must stay a map of byte slices")
}
