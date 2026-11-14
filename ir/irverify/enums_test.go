// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// enumCase is one wire enum the document holds to a declared set: where a
// fixture writes a value, the code and path a value outside the set draws, and
// the values inside it, each of which must draw none.
type enumCase struct {
	place   func(doc *ir.Document, value string)
	code    string
	path    string
	valid   []string
	invalid []string
}

// enumCases is one case per wire enum of the documents that irverify did not
// hold. An enum with an empty form that its field's comment lists as a value
// (AdditionalMode, PresenceKind, IdempotencyKind) holds "" among its valid
// values, and one whose comment gives a closed list with no empty form
// (HTTPLocation, MsgDirection, PageStrategy) draws the code for "".
func enumCases() map[string]enumCase {
	return map[string]enumCase{
		"AdditionalMode": {
			place: func(doc *ir.Document, v string) {
				doc.Types["t/x/M"].(*ir.Model).Additional = ir.AdditionalMode(v)
			},
			code: "ir/unknown-additional-mode",
			path: "types[t/x/M].Additional",
			valid: []string{
				string(ir.AdditionalUnspecified), string(ir.AdditionalClosed), string(ir.AdditionalClosedAfterComposition),
			},
			invalid: []string{"bogus"},
		},
		"PresenceKind": {
			place: func(doc *ir.Document, v string) {
				doc.Types["t/x/M"].(*ir.Model).Properties[0].Presence = ir.PresenceKind(v)
			},
			code: "ir/unknown-presence-kind",
			path: "types[t/x/M].Properties[0].Presence",
			valid: []string{
				string(ir.PresenceDefault), string(ir.PresenceImplicit), string(ir.PresenceExplicit), string(ir.PresenceRequired),
			},
			invalid: []string{"bogus"},
		},
		"HTTPLocation": {
			place: func(doc *ir.Document, v string) {
				scenarioOperation(doc).Bindings.HTTP[0].ParamBindings[0].Location = ir.HTTPLocation(v)
			},
			code: "ir/unknown-http-location",
			path: scenarioOp + ".Bindings.HTTP[0].ParamBindings[0].Location",
			valid: []string{
				string(ir.HTTPLocationPath), string(ir.HTTPLocationQuery), string(ir.HTTPLocationQuerystring),
				string(ir.HTTPLocationHeader), string(ir.HTTPLocationCookie), string(ir.HTTPLocationBody),
				string(ir.HTTPLocationBodyProperty), string(ir.HTTPLocationHost),
			},
			invalid: []string{"bogus", ""},
		},
		"IdempotencyKind": {
			place: func(doc *ir.Document, v string) {
				op := scenarioOperation(doc)
				op.Idempotency = ir.Idempotency{Kind: ir.IdempotencyKind(v)}
				if ir.IdempotencyKind(v) == ir.IdempotencyToken {
					op.Idempotency.TokenParam = "param/x/o/q"
				}
			},
			code: "ir/unknown-idempotency-kind",
			path: scenarioOp + ".Idempotency.Kind",
			valid: []string{
				string(ir.IdempotencyUnknown), string(ir.IdempotencySafe), string(ir.IdempotencyIdempotent),
				string(ir.IdempotencyToken),
			},
			invalid: []string{"bogus"},
		},
		"MsgDirection": {
			place: func(doc *ir.Document, v string) {
				doc.Channels = map[ir.ChannelID]ir.Channel{"chan/x/c": {ID: "chan/x/c", Name: named("c")}}
				doc.IDSpaces[ir.IDKindChannel] = []string{"x"}
				scenarioOperation(doc).Bindings.Message = &ir.MessageBinding{
					Channel: "chan/x/c", Direction: ir.MsgDirection(v),
				}
			},
			code:    "ir/unknown-message-direction",
			path:    scenarioOp + ".Bindings.Message.Direction",
			valid:   []string{string(ir.MsgDirectionSend), string(ir.MsgDirectionReceive)},
			invalid: []string{"bogus", ""},
		},
		"PageStrategy": {
			place: func(doc *ir.Document, v string) {
				scenarioOperation(doc).Pagination = &ir.Pagination{Strategy: ir.PageStrategy(v)}
			},
			code: "ir/unknown-page-strategy",
			path: scenarioOp + ".Pagination.Strategy",
			valid: []string{
				string(ir.PageStrategyCursor), string(ir.PageStrategyOffset), string(ir.PageStrategyPage),
				string(ir.PageStrategyLinkHeader), string(ir.PageStrategyNextLink), string(ir.PageStrategyToken),
			},
			invalid: []string{"bogus", ""},
		},
		"Severity": {
			place: func(doc *ir.Document, v string) {
				doc.Diagnostics = []ir.Diagnostic{severityDiagnostic(v)}
			},
			code:    "ir/unknown-severity",
			path:    "doc.Diagnostics[0].Severity",
			valid:   []string{string(ir.SeverityInfo), string(ir.SeverityWarning), string(ir.SeverityError)},
			invalid: []string{"bogus"},
		},
	}
}

// severityDiagnostic is a diagnostic of the given severity that is otherwise
// well formed.
func severityDiagnostic(severity string) ir.Diagnostic {
	return ir.Diagnostic{
		Severity: ir.Severity(severity), Code: "x/y", Message: "m",
		Provenance: ir.Provenance{Source: ir.NoSource},
	}
}

// TestVerify_UndeclaredEnumValueIsAViolation holds every wire enum of the
// documents to the set its documents declare, one row per value: each declared
// value passes where its field carries it, and a value outside the set draws the
// enum's code at the field, so a value added to the documents and not to the
// check, or the reverse, reddens here. Each row names the code and path the
// design states for it.
func TestVerify_UndeclaredEnumValueIsAViolation(t *testing.T) {
	t.Parallel()
	for name, tc := range enumCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, value := range tc.valid {
				doc := scenarioBase()
				tc.place(doc, value)
				assert.Empty(t, scenarioCodes(doc, tc.code), "%q is a declared %s", value, name)
			}
			for _, value := range tc.invalid {
				doc := scenarioBase()
				tc.place(doc, value)
				assert.Equal(t, []string{tc.path}, scenarioCodes(doc, tc.code), "%q is no %s", value, name)
			}
		})
	}
}

// TestVerify_UndeclaredEnumValueCarriesItsMessage pins the wording of each
// code at the value that drew it, which names the value as the document wrote it.
func TestVerify_UndeclaredEnumValueCarriesItsMessage(t *testing.T) {
	t.Parallel()
	messages := map[string]string{
		"AdditionalMode":  `model carries undeclared additional mode "bogus"`,
		"PresenceKind":    `property carries undeclared presence "bogus"`,
		"HTTPLocation":    `parameter binding carries undeclared location "bogus"`,
		"IdempotencyKind": `operation carries undeclared idempotency kind "bogus"`,
		"MsgDirection":    `message binding carries undeclared direction "bogus"`,
		"PageStrategy":    `pagination names undeclared strategy "bogus"`,
		"Severity":        `diagnostic carries undeclared severity "bogus"`,
	}
	cases := enumCases()
	assert.Len(t, messages, len(cases), "every enum case has a message row")
	for name, want := range messages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc := cases[name]
			doc := scenarioBase()
			tc.place(doc, "bogus")
			var got []string
			for _, v := range verifyDeclared(doc) {
				if v.Code == tc.code {
					got = append(got, v.Message)
				}
			}
			assert.Equal(t, []string{want}, got)
		})
	}
}

// TestVerify_ModelEnumsAreReadOnEveryPropertyAndType pins that the model
// loop reads each property rather than the first, and each model rather than
// the first: a plant on a second property and one on a second model each draw
// the code at their own index.
func TestVerify_ModelEnumsAreReadOnEveryPropertyAndType(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	m := doc.Types["t/x/M"].(*ir.Model)
	m.Properties = append(m.Properties, ir.Property{
		ID: "p/x/M/b", Name: named("b"), Type: ir.TypeRef{Target: "t/prim/string"}, Presence: "bogus",
	})
	doc.Types["t/x/N"] = &ir.Model{ID: "t/x/N", Name: named("n"), Additional: "bogus"}

	assert.Equal(t, []string{"types[t/x/M].Properties[1].Presence"}, scenarioCodes(doc, "ir/unknown-presence-kind"))
	assert.Equal(t, []string{"types[t/x/N].Additional"}, scenarioCodes(doc, "ir/unknown-additional-mode"))
}

// TestVerifyFindings_UndeclaredSeverityIsAViolation holds the same set on the
// findings a run reports before any document exists: ir/unknown-severity at
// findings[<i>].Severity, and none for a declared one.
func TestVerifyFindings_UndeclaredSeverityIsAViolation(t *testing.T) {
	t.Parallel()
	got := irverify.VerifyFindings([]ir.Diagnostic{severityDiagnostic("bogus")}, nil)
	assert.Equal(t, []string{"findings[0].Severity"}, violationPaths(got, "ir/unknown-severity"))

	for _, severity := range []ir.Severity{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError} {
		valid := irverify.VerifyFindings([]ir.Diagnostic{severityDiagnostic(string(severity))}, nil)
		assert.Empty(t, violationPaths(valid, "ir/unknown-severity"), "%q is declared", severity)
	}
}

// TestVerify_IdempotencyAndWebhookChecksKeepTheirFindings pins what the two
// pruned checks keep reporting: a token paired with a kind that takes none, and
// an inbound value outside its set, each on its own carrier.
func TestVerify_IdempotencyAndWebhookChecksKeepTheirFindings(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	op := scenarioOperation(doc)
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencySafe, TokenParam: "param/x/o/q"}
	op.Bindings.HTTP[0].Inbound = "bogus"

	assert.Equal(t, []string{scenarioOp + ".Idempotency"}, scenarioCodes(doc, "ir/idempotency-token"))
	assert.Equal(t, []string{scenarioOp + ".Bindings.HTTP[0].Inbound"}, scenarioCodes(doc, "ir/unknown-http-inbound"))

	valid := scenarioBase()
	assert.Empty(t, scenarioCodes(valid, "ir/idempotency-token"))
	assert.Empty(t, scenarioCodes(valid, "ir/unknown-http-inbound"))
}
