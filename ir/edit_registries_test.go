// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// registriesDoc returns a document of two servers, two channels, two messages
// and two auth schemes, each node's summary its ID, the first server with a
// tag, the first channel with a parameter and the first scheme with a flow.
// Each call builds new nodes.
func registriesDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Servers: map[ir.ServerID]ir.Server{
			"v/a": {ID: "v/a", Description: ir.Docs{Summary: "v/a"}, Tags: []string{"kept"}},
			"v/b": {ID: "v/b", Description: ir.Docs{Summary: "v/b"}},
		},
		Channels: map[ir.ChannelID]ir.Channel{
			"c/a": {ID: "c/a", Docs: ir.Docs{Summary: "c/a"}, Params: []ir.Parameter{{ID: "c/a/p"}}},
			"c/b": {ID: "c/b", Docs: ir.Docs{Summary: "c/b"}},
		},
		Messages: map[ir.MessageID]ir.Message{
			"m/a": {ID: "m/a", Docs: ir.Docs{Summary: "m/a"}},
			"m/b": {ID: "m/b", Docs: ir.Docs{Summary: "m/b"}},
		},
		Auth: map[ir.AuthID]ir.AuthScheme{
			"a/a": {ID: "a/a", Docs: ir.Docs{Summary: "a/a"}, Flows: []ir.OAuthFlow{{TokenURL: "https://a"}}},
			"a/b": {ID: "a/b", Docs: ir.Docs{Summary: "a/b"}},
		},
	}
}

// summaries spells the summary of every server, channel, message and auth
// scheme doc holds, keyed by ID.
func summaries(doc *ir.Document) map[string]string {
	out := map[string]string{}
	for id, s := range doc.Servers {
		out[string(id)] = s.Description.Summary
	}
	for id, c := range doc.Channels {
		out[string(id)] = c.Docs.Summary
	}
	for id, m := range doc.Messages {
		out[string(id)] = m.Docs.Summary
	}
	for id, a := range doc.Auth {
		out[string(id)] = a.Docs.Summary
	}
	return out
}

// TestEditor_ARegistryReadIsNotChangedByWhatFollows pins that what a read of a
// type, server, channel, message or auth scheme returned stays what the edit
// held then, after an update of it and after its removal.
func TestEditor_ARegistryReadIsNotChangedByWhatFollows(t *testing.T) {
	t.Parallel()
	doc := registriesDoc()
	doc.Types = ir.TypeRegistry{"t/a": &ir.Any{ID: "t/a", Docs: ir.Docs{Summary: "t/a"}}}
	e := ir.NewEditor(doc)
	td, _ := e.Type("t/a")
	server, _ := e.Server("v/a")
	channel, _ := e.Channel("c/a")
	message, _ := e.Message("m/a")
	scheme, _ := e.AuthScheme("a/a")

	e.UpdateType("t/a", func(td ir.TypeDef) { changed(&td.Common().Docs) })
	e.UpdateServer("v/a", func(s *ir.Server) {
		changed(&s.Description)
		s.Tags[0] = "changed"
	})
	e.UpdateChannel("c/a", func(c *ir.Channel) {
		changed(&c.Docs)
		c.Params[0].Docs.Summary = "changed"
	})
	e.UpdateMessage("m/a", func(m *ir.Message) { changed(&m.Docs) })
	e.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { a.Flows[0].TokenURL = "changed" })
	e.RemoveType("t/a")
	e.RemoveServer("v/a")
	e.RemoveChannel("c/a")
	_, diags := e.Finish()

	require.Empty(t, diags)
	assert.Equal(t, "t/a", td.Common().Docs.Summary)
	assert.Equal(t, "v/a", server.Description.Summary)
	assert.Equal(t, []string{"kept"}, server.Tags)
	assert.Equal(t, "c/a", channel.Docs.Summary)
	assert.Empty(t, channel.Params[0].Docs.Summary)
	assert.Equal(t, "m/a", message.Docs.Summary)
	assert.Equal(t, "https://a", scheme.Flows[0].TokenURL)
}

// TestEditor_WritesTheRegistriesAndNotTheDocument pins each write to a server,
// channel, message or auth scheme: what the edit's document holds after it,
// that a registry the edit does not write stays the edited document's, and
// that the edited document still holds what it held, byte for byte.
func TestEditor_WritesTheRegistriesAndNotTheDocument(t *testing.T) {
	t.Parallel()
	all := summaries(registriesDoc())
	without := func(ids ...string) map[string]string {
		out := maps.Clone(all)
		for _, id := range ids {
			delete(out, id)
		}
		return out
	}
	with := func(id, summary string) map[string]string {
		out := maps.Clone(all)
		out[id] = summary
		return out
	}
	tests := []struct {
		name    string
		write   func(*ir.Editor)
		want    map[string]string
		written string
	}{
		{"update a server", func(e *ir.Editor) { e.UpdateServer("v/a", func(s *ir.Server) { changed(&s.Description) }) },
			with("v/a", "changed"), "Servers"},
		{"remove a server", func(e *ir.Editor) { e.RemoveServer("v/b") }, without("v/b"), "Servers"},
		{"update a channel", func(e *ir.Editor) { e.UpdateChannel("c/a", func(c *ir.Channel) { changed(&c.Docs) }) },
			with("c/a", "changed"), "Channels"},
		{"remove a channel", func(e *ir.Editor) { e.RemoveChannel("c/b") }, without("c/b"), "Channels"},
		{"update a message", func(e *ir.Editor) { e.UpdateMessage("m/b", func(m *ir.Message) { changed(&m.Docs) }) },
			with("m/b", "changed"), "Messages"},
		{"remove a message", func(e *ir.Editor) { e.RemoveMessage("m/a") }, without("m/a"), "Messages"},
		{"update an auth scheme", func(e *ir.Editor) {
			e.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { changed(&a.Docs) })
		}, with("a/a", "changed"), "Auth"},
		{"remove an auth scheme", func(e *ir.Editor) { e.RemoveAuthScheme("a/a") }, without("a/a"), "Auth"},
		{"update a removed message's sibling", func(e *ir.Editor) {
			e.RemoveMessage("m/a")
			e.UpdateMessage("m/b", func(m *ir.Message) { m.Docs.Summary += " twice" })
		}, func() map[string]string { m := without("m/a"); m["m/b"] = "m/b twice"; return m }(), "Messages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := registriesDoc()
			before := irtest.Encoded(t, doc)

			e := ir.NewEditor(doc)
			tt.write(e)
			out, diags := e.Finish()

			assert.Empty(t, diags)
			assert.Equal(t, tt.want, summaries(out))
			assert.Equal(t, before, irtest.Encoded(t, doc), "the edited document changed")
			for _, field := range []string{"Servers", "Channels", "Messages", "Auth"} {
				in := reflect.ValueOf(doc).Elem().FieldByName(field).Pointer()
				got := reflect.ValueOf(out).Elem().FieldByName(field).Pointer()
				assert.Equal(t, field != tt.written, in == got, "%s: shared %v with the edited document", field, in == got)
			}
		})
	}
}

// TestEditor_EmptiedRegistriesAreNil pins that a removal emptying a registry
// leaves it nil, as the document decodes one that was empty, the type
// registry's as every other's.
func TestEditor_EmptiedRegistriesAreNil(t *testing.T) {
	t.Parallel()
	doc := registriesDoc()
	doc.Types = ir.TypeRegistry{"t/a": &ir.Any{ID: "t/a"}}
	e := ir.NewEditor(doc)
	for _, id := range slices.Sorted(maps.Keys(doc.Servers)) {
		e.RemoveServer(id)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Channels)) {
		e.RemoveChannel(id)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Messages)) {
		e.RemoveMessage(id)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Auth)) {
		e.RemoveAuthScheme(id)
	}
	e.RemoveType("t/a")
	out, diags := e.Finish()

	require.Empty(t, diags)
	assert.Nil(t, out.Servers)
	assert.Nil(t, out.Channels)
	assert.Nil(t, out.Messages)
	assert.Nil(t, out.Auth)
	assert.Nil(t, out.Types)
	assert.Len(t, doc.Channels, 2, "the edited document changed")
}

// TestEditor_ReadsTheRegistriesAsTheEditHoldsThem pins that Server, Channel,
// Message and AuthScheme answer with what the edit holds: a node changed as
// written, none once removed, and the edited document's otherwise.
func TestEditor_ReadsTheRegistriesAsTheEditHoldsThem(t *testing.T) {
	t.Parallel()
	doc := registriesDoc()
	e := ir.NewEditor(doc)
	e.UpdateServer("v/a", func(s *ir.Server) { changed(&s.Description) })
	e.RemoveServer("v/b")
	e.UpdateChannel("c/a", func(c *ir.Channel) { changed(&c.Docs) })
	e.RemoveMessage("m/a")
	e.UpdateAuthScheme("a/b", func(a *ir.AuthScheme) { changed(&a.Docs) })

	s, sHeld := e.Server("v/a")
	_, gone := e.Server("v/b")
	c, cHeld := e.Channel("c/a")
	_, mHeld := e.Message("m/a")
	m, otherHeld := e.Message("m/b")
	a, aHeld := e.AuthScheme("a/b")
	_, absent := e.AuthScheme("a/x")

	assert.True(t, sHeld)
	assert.Equal(t, "changed", s.Description.Summary)
	assert.False(t, gone)
	assert.True(t, cHeld)
	assert.Equal(t, "changed", c.Docs.Summary)
	assert.False(t, mHeld)
	assert.True(t, otherHeld)
	assert.Equal(t, doc.Messages["m/b"].Docs, m.Docs)
	assert.True(t, aHeld)
	assert.Equal(t, "changed", a.Docs.Summary)
	assert.False(t, absent)
}

// TestEditor_RefusesRegistryWritesOnlyABugMakes pins each write to a
// registry refused for what it asks alone: the one message Finish reports, and
// that the refused write leaves the edit's document as a control edit making
// only the writes before it leaves its own.
func TestEditor_RefusesRegistryWritesOnlyABugMakes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		before  func(*ir.Editor)
		refused func(*ir.Editor)
		want    string
	}{
		{"update an absent server", nil, func(e *ir.Editor) { e.UpdateServer("v/x", func(*ir.Server) {}) },
			`update rejected: no server "v/x" to update; an edit adds no server`},
		{"remove a removed server", func(e *ir.Editor) { e.RemoveServer("v/b") },
			func(e *ir.Editor) { e.RemoveServer("v/b") },
			`remove rejected: no server "v/b" to remove; an edit adds no server`},
		{"update moving a server", nil, func(e *ir.Editor) {
			e.UpdateServer("v/a", func(s *ir.Server) { s.ID = "v/b" })
		}, `update rejected: the change moved "v/a" to "v/b"; a node keeps its ID`},
		{"update an absent channel", nil, func(e *ir.Editor) { e.UpdateChannel("c/x", func(*ir.Channel) {}) },
			`update rejected: no channel "c/x" to update; an edit adds no channel`},
		{"remove an absent message", nil, func(e *ir.Editor) { e.RemoveMessage("m/x") },
			`remove rejected: no message "m/x" to remove; an edit adds no message`},
		{"remove a removed auth scheme", func(e *ir.Editor) { e.RemoveAuthScheme("a/b") },
			func(e *ir.Editor) { e.RemoveAuthScheme("a/b") },
			`remove rejected: no auth scheme "a/b" to remove; an edit adds no auth scheme`},
		{"update a removed channel", func(e *ir.Editor) { e.RemoveChannel("c/a") },
			func(e *ir.Editor) { e.UpdateChannel("c/a", func(*ir.Channel) {}) }, `update rejected: no channel "c/a"`},
		{"update a message with no change", nil, func(e *ir.Editor) { e.UpdateMessage("m/a", nil) },
			`update rejected: no change for "m/a"`},
		{"update moving an auth scheme", nil, func(e *ir.Editor) {
			e.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { a.ID = "a/b" })
		}, `update rejected: the change moved "a/a" to "a/b"; a node keeps its ID`},
		{"update adding a channel parameter", nil, func(e *ir.Editor) {
			e.UpdateChannel("c/b", func(c *ir.Channel) { c.Params = []ir.Parameter{{ID: "c/a/p"}} })
		}, `update rejected: the change of "c/b" declares the ir.ParamID "c/a/p", which the node does not`},
		{"update repeating a channel parameter", nil, func(e *ir.Editor) {
			e.UpdateChannel("c/a", func(c *ir.Channel) { c.Params = append(c.Params, c.Params[0]) })
		}, `update rejected: the change of "c/a" declares the ir.ParamID "c/a/p" 2 times, where the node does 1 times`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, control := registriesDoc(), registriesDoc()
			e, c := ir.NewEditor(doc), ir.NewEditor(control)
			if tt.before != nil {
				tt.before(e)
				tt.before(c)
			}
			tt.refused(e)

			out, diags := e.Finish()
			want, ctrl := c.Finish()

			require.Empty(t, ctrl, "a write before the refused one was refused as well, so the case tests the wrong write")
			got := refusals(t, diags)
			require.Len(t, got, 1, "one refused write, one refusal: %v", got)
			assert.Contains(t, got[0], "internal: edit: "+tt.want)
			assert.Equal(t, irtest.Encoded(t, want), irtest.Encoded(t, out), "the refused write changed the edit")
			assert.Equal(t, irtest.Encoded(t, registriesDoc()), irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_RefusesARegistryNodeAtAnotherIDsKey pins the update of a server,
// channel, message or auth scheme whose node names another ID, which irverify
// reports: it is refused as what it is, while a removal takes the entry as any
// other.
func TestEditor_RefusesARegistryNodeAtAnotherIDsKey(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{
		Servers:  map[ir.ServerID]ir.Server{"v/a": {ID: "v/b"}},
		Channels: map[ir.ChannelID]ir.Channel{"c/a": {ID: "c/b"}},
		Messages: map[ir.MessageID]ir.Message{"m/a": {ID: "m/b"}},
		Auth:     map[ir.AuthID]ir.AuthScheme{"a/a": {ID: "a/b"}, "a/c": {ID: "a/d"}},
	}

	e := ir.NewEditor(doc)
	e.UpdateServer("v/a", func(*ir.Server) {})
	e.UpdateChannel("c/a", func(*ir.Channel) {})
	e.UpdateMessage("m/a", func(*ir.Message) {})
	e.UpdateAuthScheme("a/a", func(*ir.AuthScheme) {})
	e.RemoveAuthScheme("a/c")
	out, diags := e.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: "v/a" holds the node of "v/b", which belongs at its own ID`,
		`internal: edit: update rejected: "c/a" holds the node of "c/b", which belongs at its own ID`,
		`internal: edit: update rejected: "m/a" holds the node of "m/b", which belongs at its own ID`,
		`internal: edit: update rejected: "a/a" holds the node of "a/b", which belongs at its own ID`,
	}, refusals(t, diags))
	assert.Equal(t, map[ir.AuthID]ir.AuthScheme{"a/a": {ID: "a/b"}}, out.Auth)
}
