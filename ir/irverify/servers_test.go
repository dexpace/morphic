// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// serverDoc is validDoc with two servers registered, one declared at its
// pointer and one minted with none, a service naming the first and an
// operation naming the second, as a compiled document carries them.
func serverDoc() *ir.Document {
	doc := validDoc()
	doc.Servers = map[ir.ServerID]ir.Server{
		"server/x/servers/0": {ID: "server/x/servers/0", Name: named("prod"), URLTemplate: "https://a.example",
			Provenance: ir.Provenance{Pointer: "/servers/0"}},
		"server/default": {ID: "server/default", Name: named("root"), URLTemplate: "/"},
	}
	doc.Services = []ir.Service{{
		ID: "s/x/S", Name: named("svc"), Servers: []ir.ServerID{"server/x/servers/0"},
		Groups: []ir.OperationGroup{{ID: "g/x/G", Name: named("g"), Operations: []ir.Operation{{
			ID: "op/x/get", Name: named("get"), Servers: []ir.ServerID{"server/default"},
		}}}},
	}}
	return doc
}

// TestVerify_ServersAreHeldToTheIDRules plants each defect a server's identity
// can carry and requires the rule every other class of ID answers to, so a
// server is checked as a type or a scheme is rather than by rules of its own.
// The clean document is the control: a minted server records no pointer and
// is not held to one.
func TestVerify_ServersAreHeldToTheIDRules(t *testing.T) {
	t.Parallel()
	require.Empty(t, verifyDeclared(serverDoc()), "the fixture is clean")

	for _, tc := range []struct {
		name, code string
		plant      func(d *ir.Document)
	}{
		{"registry key disagrees with the ID", "ir/server-id-mismatch", func(d *ir.Document) {
			s := d.Servers["server/default"]
			s.ID = "server/default/other"
			d.Servers["server/default"] = s
		}},
		{"ID path disagrees with the pointer", "ir/id-provenance-disagreement", func(d *ir.Document) {
			s := d.Servers["server/x/servers/0"]
			s.Provenance.Pointer = "/servers/1"
			d.Servers["server/x/servers/0"] = s
		}},
		{"malformed ID", "ir/id-malformed", func(d *ir.Document) {
			d.Servers["server/"] = ir.Server{ID: "server/", Name: named("bad")}
		}},
		{"a scope names no registered server", "ir/dangling-server-ref", func(d *ir.Document) {
			d.Services[0].Servers = append(d.Services[0].Servers, "server/x/ghost")
		}},
		{"a scope names an empty ID", "ir/empty-server-ref", func(d *ir.Document) {
			d.Services[0].Groups[0].Operations[0].Servers = []ir.ServerID{""}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := serverDoc()
			tc.plant(doc)
			assert.Contains(t, violationCodes(verifyDeclared(doc)), tc.code)
		})
	}
}

// TestVerify_ServerSpaceMustBeDeclared pins the namespace half: a server ID in
// a space the document does not declare is reported as any other ID's is.
func TestVerify_ServerSpaceMustBeDeclared(t *testing.T) {
	t.Parallel()
	doc := serverDoc()
	doc.Sources = oneSource()
	doc.IDSpaces = map[string][]string{
		ir.IDKindGroup: {"x"}, ir.IDKindOp: {"x"}, ir.IDKindService: {"x"}, ir.IDKindType: {"x"},
		ir.IDKindServer: {"default"},
	}
	found := irverify.Verify(doc)
	assert.Equal(t, []string{"ir/id-space-undeclared"}, violationCodes(found), "only the undeclared space: %+v", found)
}
