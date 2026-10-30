// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import "maps"

// registry is an edit's view of one of a document's maps of nodes keyed by ID:
// the map the edit's document holds, whether the edit owns it yet, and the noun
// a refusal names its nodes by.
type registry[K ~string, V any] struct {
	held  *map[K]V
	owned *bool
	noun  string
}

// types is the edit's view of Document.Types.
func (e *Editor) types() registry[TypeID, TypeDef] {
	return registry[TypeID, TypeDef]{held: (*map[TypeID]TypeDef)(&e.out.Types), owned: &e.owns.types, noun: "type"}
}

// servers is the edit's view of Document.Servers.
func (e *Editor) servers() registry[ServerID, Server] {
	return registry[ServerID, Server]{held: &e.out.Servers, owned: &e.owns.servers, noun: "server"}
}

// serverKind is what an update knows of a server.
func serverKind() nodeKind[*Server] {
	return nodeKind[*Server]{method: "UpdateServer", id: func(s *Server) string { return string(s.ID) }}
}

// channels is the edit's view of Document.Channels.
func (e *Editor) channels() registry[ChannelID, Channel] {
	return registry[ChannelID, Channel]{held: &e.out.Channels, owned: &e.owns.channels, noun: "channel"}
}

// channelKind is what an update knows of a channel.
func channelKind() nodeKind[*Channel] {
	return nodeKind[*Channel]{method: "UpdateChannel", id: func(c *Channel) string { return string(c.ID) }}
}

// messages is the edit's view of Document.Messages.
func (e *Editor) messages() registry[MessageID, Message] {
	return registry[MessageID, Message]{held: &e.out.Messages, owned: &e.owns.messages, noun: "message"}
}

// messageKind is what an update knows of a message.
func messageKind() nodeKind[*Message] {
	return nodeKind[*Message]{method: "UpdateMessage", id: func(m *Message) string { return string(m.ID) }}
}

// authSchemes is the edit's view of Document.Auth.
func (e *Editor) authSchemes() registry[AuthID, AuthScheme] {
	return registry[AuthID, AuthScheme]{held: &e.out.Auth, owned: &e.owns.auth, noun: "auth scheme"}
}

// authSchemeKind is what an update knows of an auth scheme.
func authSchemeKind() nodeKind[*AuthScheme] {
	return nodeKind[*AuthScheme]{method: "UpdateAuthScheme", id: func(a *AuthScheme) string { return string(a.ID) }}
}

// Server returns the server the edit holds at id. It is shared, so it is read
// and never changed; UpdateServer changes a copy.
func (e *Editor) Server(id ServerID) (Server, bool) {
	s, held := e.out.Servers[id]
	return s, held
}

// UpdateServer hands change a deep copy of the server at id, to alter in any
// part but its ID, and holds the copy in its place. It refuses what UpdateType
// refuses of an id, a node off its own key and a change.
func (e *Editor) UpdateServer(id ServerID, change func(*Server)) {
	updateEntry(e, e.servers(), serverKind(), id, change)
}

// RemoveServer removes the server at id, and refuses an id the edit holds none
// at. Whatever still names id is left to validate.
func (e *Editor) RemoveServer(id ServerID) {
	removeEntry(e, e.servers(), id)
}

// Channel returns the channel the edit holds at id. It is shared, so it is
// read and never changed; UpdateChannel changes a copy.
func (e *Editor) Channel(id ChannelID) (Channel, bool) {
	c, held := e.out.Channels[id]
	return c, held
}

// UpdateChannel hands change a deep copy of the channel at id, to alter in any
// part but its ID, and holds the copy in its place. It refuses what UpdateType
// refuses of an id, a node off its own key and a change.
func (e *Editor) UpdateChannel(id ChannelID, change func(*Channel)) {
	updateEntry(e, e.channels(), channelKind(), id, change)
}

// RemoveChannel removes the channel at id, and refuses an id the edit holds
// none at. Whatever still references id is left to validate.
func (e *Editor) RemoveChannel(id ChannelID) {
	removeEntry(e, e.channels(), id)
}

// Message returns the message the edit holds at id. It is shared, so it is
// read and never changed; UpdateMessage changes a copy.
func (e *Editor) Message(id MessageID) (Message, bool) {
	m, held := e.out.Messages[id]
	return m, held
}

// UpdateMessage hands change a deep copy of the message at id, to alter in any
// part but its ID, and holds the copy in its place. It refuses what UpdateType
// refuses of an id, a node off its own key and a change.
func (e *Editor) UpdateMessage(id MessageID, change func(*Message)) {
	updateEntry(e, e.messages(), messageKind(), id, change)
}

// RemoveMessage removes the message at id, and refuses an id the edit holds
// none at. Whatever still references id is left to validate.
func (e *Editor) RemoveMessage(id MessageID) {
	removeEntry(e, e.messages(), id)
}

// AuthScheme returns the auth scheme the edit holds at id. It is shared, so it
// is read and never changed; UpdateAuthScheme changes a copy.
func (e *Editor) AuthScheme(id AuthID) (AuthScheme, bool) {
	a, held := e.out.Auth[id]
	return a, held
}

// UpdateAuthScheme hands change a deep copy of the auth scheme at id, to alter
// in any part but its ID, and holds the copy in its place. It refuses what
// UpdateType refuses of an id, a node off its own key and a change.
func (e *Editor) UpdateAuthScheme(id AuthID, change func(*AuthScheme)) {
	updateEntry(e, e.authSchemes(), authSchemeKind(), id, change)
}

// RemoveAuthScheme removes the auth scheme at id, and refuses an id the edit
// holds none at. Whatever still references id is left to validate.
func (e *Editor) RemoveAuthScheme(id AuthID) {
	removeEntry(e, e.authSchemes(), id)
}

// updateEntry is UpdateServer, UpdateChannel, UpdateMessage and
// UpdateAuthScheme over r, and refuses a node off its own ID's key besides what
// updated refuses.
func updateEntry[K ~string, V any](e *Editor, r registry[K, V], kind nodeKind[*V], id K, change func(*V)) {
	current, ok := entryAt(e, r, "update", id)
	if !ok {
		return
	}
	if at := kind.id(&current); at != string(id) {
		e.refuse("update rejected: %q holds the node of %q, which belongs at its own ID", id, at)
		return
	}
	if next, taken := updated(e, kind, string(id), subjectAt(id), &current, change); taken {
		r.write()[id] = *next
	}
}

// removeEntry removes the node the edit holds at id in r.
func removeEntry[K ~string, V any](e *Editor, r registry[K, V], id K) {
	if _, ok := entryAt(e, r, "remove", id); !ok {
		return
	}
	held := r.write()
	delete(held, id)
	if len(held) == 0 {
		*r.held = nil
	}
}

// entryAt returns the node the edit holds at id in r for a write verb names.
// It refuses one the edit does not take (open), and an id it holds no node
// at: an edit adds no ID.
func entryAt[K ~string, V any](e *Editor, r registry[K, V], verb string, id K) (V, bool) {
	if !e.open(verb, subjectAt(id)) {
		var none V
		return none, false
	}
	v, held := (*r.held)[id]
	if !held {
		e.refuse("%s rejected: no %s %q to %s; an edit adds no %s", verb, r.noun, id, verb, r.noun)
	}
	return v, held
}

// write returns the map r views, copying it at the first write so the edited
// document's is never written.
func (r registry[K, V]) write() map[K]V {
	if !*r.owned {
		*r.held = maps.Clone(*r.held)
		*r.owned = true
	}
	return *r.held
}
