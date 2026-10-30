package pass

import (
	"maps"

	"github.com/dexpace/morphic/ir"
)

// channels is the channel registry under another name.
type channels map[ir.ChannelID]ir.Channel

// keepAny takes a value of any type.
func keepAny[T any](T) {}

// Registries builds and writes each registry beside the type registry, which
// a compiler may, and a pass may not.
func Registries(doc *ir.Document, ch ir.Channel, id ir.ChannelID) {
	_ = map[ir.ChannelID]ir.Channel{}     // want "registrywrites: builds a channel registry (a map from ir.ChannelID to ir.Channel) outside ir and the compilers; edit a document's registry through an ir.Editor"
	_ = make(map[ir.MessageID]ir.Message) // want "registrywrites: builds a message registry (a map from ir.MessageID to ir.Message) outside ir and the compilers; edit a document's registry through an ir.Editor"
	_ = channels{}                        // want "registrywrites: builds a channel registry (a map from ir.ChannelID to ir.Channel) outside ir and the compilers; edit a document's registry through an ir.Editor"
	_ = maps.Clone(doc.Auth)              // want "registrywrites: builds an auth registry (a map from ir.AuthID to ir.AuthScheme) outside ir and the compilers; edit a document's registry through an ir.Editor"
	doc.Channels[id] = ch                 // want "registrywrites: writes a channel registry outside ir and the compilers; edit a document's registry through an ir.Editor"
	clear(doc.Auth)                       // want "registrywrites: writes an auth registry outside ir and the compilers; edit a document's registry through an ir.Editor"
	maps.Copy(doc.Channels, doc.Channels) // want "registrywrites: writes a channel registry outside ir and the compilers; edit a document's registry through an ir.Editor"
	keepAny(doc.Messages)                 // want "registrywrites: instantiates a generic with a message registry or ir.Message, whose registry writes this rule cannot read; keep registry writes in ir and the compilers"
	keepAny(ch)                           // want "registrywrites: instantiates a generic with a channel registry or ir.Channel, whose registry writes this rule cannot read; keep registry writes in ir and the compilers"

	_ = doc.Channels[id]
	_ = len(doc.Messages)
	for range maps.Keys(doc.Auth) {
	}
	byName := map[string]ir.Channel{}
	byName["c"] = ch
	keepAny(id)
}
