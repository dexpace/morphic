// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// IRVersion is the semver of the IR schema itself. Compilers stamp it into
// Document.IRVersion; the document decoder and irverify compare it through
// CompatibleVersion and refuse any other.
//
// It names a schema generation, not a commit: any change to the JSON shape, or
// to how a name is derived, bumps it, once, where the change lands on main.
// docs/ir/ir-version.md holds that policy, docs/ir/version-history.md what each
// bump changed, and TestIRVersion_PinsTheEncodedShape and
// TestIRVersion_PinsTheNames hold it to the shape and the names.
const IRVersion = "0.24.0"

// CompatibleVersion reports whether a document stamped version can be read by
// this build: exact equality with IRVersion, per docs/ir/ir-version.md. A
// differing patch, a prerelease suffix and a non-version are equally
// unreadable, since accepting a neighbouring version would claim to know what
// changed between the two. A consumer asks it before interpreting any other
// field of a decoded document.
//
// An empty version is incompatible too, but a caller that can act on the
// difference should test for it separately: absence is a producer that never
// stamped the document, an unrecognized stamp a fault in the pairing.
func CompatibleVersion(version string) bool {
	return version == IRVersion
}

// TypeRegistry is the flat, ID-keyed owner of every TypeDef in a Document
// (docs/ir/document.md, docs/ir/type-graph.md); every other node references
// types by TypeID. JSON (un)marshaling of the sealed sum is defined with the
// rest of the sum-type codec.
type TypeRegistry map[TypeID]TypeDef

// Document is the root of a Morphic IR document (docs/ir/document.md). It is
// self-contained: no node references anything outside it.
type Document struct {
	// IRVersion is the version of the IR schema itself (semver).
	IRVersion string `json:"irVersion,omitempty"`
	// IDSpaces declares, for each kind prefix (IDKinds), the namespaces the
	// document's IDs of that kind live in. A producer states it as it stamps
	// IRVersion, from the namespace constants it already names; irverify reports
	// an ID whose namespace is not declared, which sees a path glued onto its
	// namespace in every class. A type needs no entry for IDSpacePrim, which is
	// ir's own. A kind with no namespace is left out. Each list is sorted by the
	// byte order of its UTF-8 spelling, without repeats, and names no empty or
	// separator-bearing namespace.
	IDSpaces map[string][]string `json:"idSpaces,omitempty"`
	// Name is the API title, as the source declares it, or empty for a source
	// that declares none.
	Name string `json:"name,omitempty"`
	// Version is the source-declared API version string.
	Version string `json:"version,omitempty"`
	// Docs is the document-level documentation.
	Docs Docs `json:"docs"`
	// Contact is the API contact (OpenAPI/AsyncAPI info.contact).
	Contact *Contact `json:"contact,omitzero"`
	// License is the API license (OpenAPI/AsyncAPI info.license).
	License *License `json:"license,omitzero"`
	// TermsOfService is the terms-of-service URL or text.
	TermsOfService string `json:"termsOfService,omitempty"`
	// Services holds one or more services; multi-service documents are normal
	// (TypeSpec, stitching).
	Services []Service `json:"services,omitempty"`
	// Types is the type registry — the only owner of TypeDefs.
	Types TypeRegistry `json:"types,omitempty"`
	// Channels is the event/messaging layer (AsyncAPI, webhooks, subscriptions,
	// OTP processes).
	Channels map[ChannelID]Channel `json:"channels,omitempty"`
	// Messages is the message registry; messages are reused across channels and
	// referenced by identity from operations and replies (AsyncAPI 3).
	Messages map[MessageID]Message `json:"messages,omitempty"`
	// Auth is the auth scheme registry.
	Auth map[AuthID]AuthScheme `json:"auth,omitempty"`
	// Servers is the server registry, keyed by ID: the servers the source
	// declares, whichever object declares them. The services, channels and
	// operations a server serves name it by ID, in their own order
	// (docs/ir/servers.md), so one declared for a scope every member of which
	// overrides it is held here and named by none.
	Servers map[ServerID]Server `json:"servers,omitempty"`
	// TagDefs is the tag metadata registry; tag membership stays []string on the
	// tagged nodes.
	TagDefs []TagDef `json:"tagDefs,omitempty"`
	// Versions holds the ordered version labels when availability metadata is used.
	Versions []string `json:"versions,omitempty"`
	// Unmodeled holds source constructs the IR does not model, kept verbatim.
	Unmodeled Unmodeled `json:"unmodeled,omitempty"`
	// Diagnostics is accumulated by the compiler and passes; not part of API
	// meaning.
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	// Sources describes the input files, one entry each; a compiled document
	// read at least one.
	Sources []SourceInfo `json:"sources,omitempty"`
}

// Contact is the API contact information (OpenAPI/AsyncAPI info.contact).
type Contact struct {
	// Name is the contact name.
	Name string `json:"name,omitempty"`
	// URL is the contact URL.
	URL string `json:"url,omitempty"`
	// Email is the contact email address.
	Email string `json:"email,omitempty"`
}

// License is the API license information (OpenAPI/AsyncAPI info.license).
type License struct {
	// Name is the license name.
	Name string `json:"name,omitempty"`
	// Identifier is the SPDX license identifier.
	Identifier string `json:"identifier,omitempty"`
	// URL is the license URL.
	URL string `json:"url,omitempty"`
}

// TagDef is one entry of the document's tag metadata registry. Tag membership
// stays []string on the tagged nodes.
type TagDef struct {
	// Name is the tag name referenced by tagged nodes.
	Name string `json:"name,omitempty"`
	// Docs is the tag's documentation.
	Docs Docs `json:"docs"`
	// Parent names the tag this one is nested under (OpenAPI 3.2 tag parent):
	// the declared hierarchy a consumer may render as a section tree. Empty =
	// top-level. It is the declared spelling, not a resolved reference — a
	// parent that names no declared tag is reported by the parser and left as
	// written here, since resolving it is grouping policy (OperationGroup).
	Parent string `json:"parent,omitempty"`
	// Kind is the tag's declared role (OpenAPI 3.2 tag kind), recorded verbatim
	// — "nav", "badge", or any other string a document writes. It is kept as a
	// fact rather than interpreted: whether a kind is navigational is grouping
	// policy, not a property of the document, so no meaning is read into a
	// value here (docs/ir/services.md).
	Kind string `json:"kind,omitempty"`
}
