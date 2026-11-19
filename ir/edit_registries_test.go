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
	for id, server := range doc.Servers {
		out[string(id)] = server.Description.Summary
	}
	for id, channel := range doc.Channels {
		out[string(id)] = channel.Docs.Summary
	}
	for id, message := range doc.Messages {
		out[string(id)] = message.Docs.Summary
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
	doc.Types = ir.TypeRegistry{"type/a": &ir.Any{ID: "type/a", Docs: ir.Docs{Summary: "type/a"}}}
	editor := ir.NewEditor(doc)
	td, _ := editor.Type("type/a")
	server, _ := editor.Server("v/a")
	channel, _ := editor.Channel("c/a")
	message, _ := editor.Message("m/a")
	scheme, _ := editor.AuthScheme("a/a")

	editor.UpdateType("type/a", func(td ir.TypeDef) { changed(&td.Common().Docs) })
	editor.UpdateServer("v/a", func(server *ir.Server) {
		changed(&server.Description)
		server.Tags[0] = "changed"
	})
	editor.UpdateChannel("c/a", func(channel *ir.Channel) {
		changed(&channel.Docs)
		channel.Params[0].Docs.Summary = "changed"
	})
	editor.UpdateMessage("m/a", func(message *ir.Message) { changed(&message.Docs) })
	editor.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { a.Flows[0].TokenURL = "changed" })
	editor.RemoveType("type/a")
	editor.RemoveServer("v/a")
	editor.RemoveChannel("c/a")
	_, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Equal(t, "type/a", td.Common().Docs.Summary)
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
		{"update a server", func(editor *ir.Editor) {
			editor.UpdateServer("v/a", func(server *ir.Server) { changed(&server.Description) })
		},
			with("v/a", "changed"), "Servers"},
		{"remove a server", func(editor *ir.Editor) { editor.RemoveServer("v/b") }, without("v/b"), "Servers"},
		{"update a channel", func(editor *ir.Editor) {
			editor.UpdateChannel("c/a", func(channel *ir.Channel) { changed(&channel.Docs) })
		},
			with("c/a", "changed"), "Channels"},
		{"remove a channel", func(editor *ir.Editor) { editor.RemoveChannel("c/b") }, without("c/b"), "Channels"},
		{"update a message", func(editor *ir.Editor) {
			editor.UpdateMessage("m/b", func(message *ir.Message) { changed(&message.Docs) })
		},
			with("m/b", "changed"), "Messages"},
		{"remove a message", func(editor *ir.Editor) { editor.RemoveMessage("m/a") }, without("m/a"), "Messages"},
		{"update an auth scheme", func(editor *ir.Editor) {
			editor.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { changed(&a.Docs) })
		}, with("a/a", "changed"), "Auth"},
		{"remove an auth scheme", func(editor *ir.Editor) { editor.RemoveAuthScheme("a/a") }, without("a/a"), "Auth"},
		{"update a removed message's sibling", func(editor *ir.Editor) {
			editor.RemoveMessage("m/a")
			editor.UpdateMessage("m/b", func(message *ir.Message) { message.Docs.Summary += " twice" })
		}, func() map[string]string { m := without("m/a"); m["m/b"] = "m/b twice"; return m }(), "Messages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := registriesDoc()
			before := irtest.Encoded(t, doc)

			editor := ir.NewEditor(doc)
			tt.write(editor)
			out, diags := editor.Finish()

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
	doc.Types = ir.TypeRegistry{"type/a": &ir.Any{ID: "type/a"}}
	editor := ir.NewEditor(doc)
	for _, id := range slices.Sorted(maps.Keys(doc.Servers)) {
		editor.RemoveServer(id)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Channels)) {
		editor.RemoveChannel(id)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Messages)) {
		editor.RemoveMessage(id)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Auth)) {
		editor.RemoveAuthScheme(id)
	}
	editor.RemoveType("type/a")
	out, diags := editor.Finish()

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
	editor := ir.NewEditor(doc)
	editor.UpdateServer("v/a", func(server *ir.Server) { changed(&server.Description) })
	editor.RemoveServer("v/b")
	editor.UpdateChannel("c/a", func(channel *ir.Channel) { changed(&channel.Docs) })
	editor.RemoveMessage("m/a")
	editor.UpdateAuthScheme("a/b", func(a *ir.AuthScheme) { changed(&a.Docs) })

	serverValue, sHeld := editor.Server("v/a")
	_, gone := editor.Server("v/b")
	channelValue, cHeld := editor.Channel("c/a")
	_, mHeld := editor.Message("m/a")
	message, otherHeld := editor.Message("m/b")
	a, aHeld := editor.AuthScheme("a/b")
	_, absent := editor.AuthScheme("a/x")

	assert.True(t, sHeld)
	assert.Equal(t, "changed", serverValue.Description.Summary)
	assert.False(t, gone)
	assert.True(t, cHeld)
	assert.Equal(t, "changed", channelValue.Docs.Summary)
	assert.False(t, mHeld)
	assert.True(t, otherHeld)
	assert.Equal(t, doc.Messages["m/b"].Docs, message.Docs)
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
		{"update an absent server", nil, func(editor *ir.Editor) { editor.UpdateServer("v/x", func(*ir.Server) {}) },
			`update rejected: no server "v/x" to update; an edit adds no server`},
		{"remove a removed server", func(editor *ir.Editor) { editor.RemoveServer("v/b") },
			func(editor *ir.Editor) { editor.RemoveServer("v/b") },
			`remove rejected: no server "v/b" to remove; an edit adds no server`},
		{"update moving a server", nil, func(editor *ir.Editor) {
			editor.UpdateServer("v/a", func(server *ir.Server) { server.ID = "v/b" })
		}, `update rejected: the change moved "v/a" to "v/b"; a node keeps its ID`},
		{"update an absent channel", nil, func(editor *ir.Editor) { editor.UpdateChannel("c/x", func(*ir.Channel) {}) },
			`update rejected: no channel "c/x" to update; an edit adds no channel`},
		{"remove an absent message", nil, func(editor *ir.Editor) { editor.RemoveMessage("m/x") },
			`remove rejected: no message "m/x" to remove; an edit adds no message`},
		{"remove a removed auth scheme", func(editor *ir.Editor) { editor.RemoveAuthScheme("a/b") },
			func(editor *ir.Editor) { editor.RemoveAuthScheme("a/b") },
			`remove rejected: no auth scheme "a/b" to remove; an edit adds no auth scheme`},
		{"update a removed channel", func(editor *ir.Editor) { editor.RemoveChannel("c/a") },
			func(editor *ir.Editor) { editor.UpdateChannel("c/a", func(*ir.Channel) {}) }, `update rejected: no channel "c/a"`},
		{"update a message with no change", nil, func(editor *ir.Editor) { editor.UpdateMessage("m/a", nil) },
			`update rejected: no change for "m/a"`},
		{"update moving an auth scheme", nil, func(editor *ir.Editor) {
			editor.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { a.ID = "a/b" })
		}, `update rejected: the change moved "a/a" to "a/b"; a node keeps its ID`},
		{"update adding a channel parameter", nil, func(editor *ir.Editor) {
			editor.UpdateChannel("c/b", func(channel *ir.Channel) { channel.Params = []ir.Parameter{{ID: "c/a/p"}} })
		}, `update rejected: the change of "c/b" declares the ir.ParameterID "c/a/p", which the node does not`},
		{"update repeating a channel parameter", nil, func(editor *ir.Editor) {
			editor.UpdateChannel("c/a", func(channel *ir.Channel) { channel.Params = append(channel.Params, channel.Params[0]) })
		}, `update rejected: the change of "c/a" declares the ir.ParameterID "c/a/p" 2 times, where the node does 1 times`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, control := registriesDoc(), registriesDoc()
			editor, editorValue := ir.NewEditor(doc), ir.NewEditor(control)
			if tt.before != nil {
				tt.before(editor)
				tt.before(editorValue)
			}
			tt.refused(editor)

			out, diags := editor.Finish()
			want, ctrl := editorValue.Finish()

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

	editor := ir.NewEditor(doc)
	editor.UpdateServer("v/a", func(*ir.Server) {})
	editor.UpdateChannel("c/a", func(*ir.Channel) {})
	editor.UpdateMessage("m/a", func(*ir.Message) {})
	editor.UpdateAuthScheme("a/a", func(*ir.AuthScheme) {})
	editor.RemoveAuthScheme("a/c")
	out, diags := editor.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: "v/a" holds the node of "v/b", which belongs at its own ID`,
		`internal: edit: update rejected: "c/a" holds the node of "c/b", which belongs at its own ID`,
		`internal: edit: update rejected: "m/a" holds the node of "m/b", which belongs at its own ID`,
		`internal: edit: update rejected: "a/a" holds the node of "a/b", which belongs at its own ID`,
	}, refusals(t, diags))
	assert.Equal(t, map[ir.AuthID]ir.AuthScheme{"a/a": {ID: "a/b"}}, out.Auth)
}
