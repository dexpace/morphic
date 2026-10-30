// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// UpdateDocument hands change a deep copy of the document's own fields, the
// version, docs and tag definitions among them, and holds the copy in their
// place. The services, types, servers, channels, messages and auth schemes are
// left out of the copy, and stay as the edit holds them: the edit changes each
// by its own ID.
//
// It refuses a nil change, and a change writing to or ending the edit or
// setting one of the fields left out. A refused or panicking update leaves the
// document as it was.
func (e *Editor) UpdateDocument(change func(*Document)) {
	at := subject{document: true}
	if !e.open("update", at) {
		return
	}
	own := *e.out
	own.Services, own.Types, own.Servers, own.Channels, own.Messages, own.Auth = nil, nil, nil, nil, nil, nil
	next, taken := updated(e, documentKind(), "", at, &own, change)
	if !taken {
		return
	}
	next.Services, next.Types, next.Servers, next.Channels, next.Messages, next.Auth =
		e.out.Services, e.out.Types, e.out.Servers, e.out.Channels, e.out.Messages, e.out.Auth
	*e.out = *next
}

// documentKind is what an update knows of a document, which has no ID of its
// own: those of the nodes it holds are each held to its key.
func documentKind() nodeKind[*Document] {
	return nodeKind[*Document]{
		method: "UpdateDocument",
		id:     func(*Document) string { return "" },
		keyed:  keyedDocumentField,
	}
}

// keyedDocumentField names a field of doc holding nodes the edit keys by ID,
// when one is set.
func keyedDocumentField(doc *Document) string {
	switch {
	case doc.Services != nil:
		return "Services"
	case doc.Types != nil:
		return "Types"
	case doc.Servers != nil:
		return "Servers"
	case doc.Channels != nil:
		return "Channels"
	case doc.Messages != nil:
		return "Messages"
	case doc.Auth != nil:
		return "Auth"
	default:
		return ""
	}
}
