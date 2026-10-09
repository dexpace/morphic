package ir

// IRVersion is the semver of the IR schema itself. Compilers stamp it into
// Document.IRVersion; the document decoder and irverify compare it through
// CompatibleVersion and refuse any other.
//
// It names a schema generation, not a commit: any change to the JSON shape
// bumps it, once, where the change lands on main. ir-design §2.1 holds that
// policy and the history of what each bump changed.
const IRVersion = "0.7.0"

// CompatibleVersion reports whether a document stamped version can be read by
// this build: exact equality with IRVersion, per ir-design §2.1. A differing
// patch, a prerelease suffix and a non-version are equally unreadable, since
// accepting a neighbouring version would claim to know what changed between the
// two. A consumer asks it before interpreting any other field of a decoded
// document.
//
// An empty version is incompatible too, but a caller that can act on the
// difference should test for it separately: absence is a producer that never
// stamped the document, an unrecognized stamp a fault in the pairing.
func CompatibleVersion(version string) bool {
	return version == IRVersion
}

// TypeRegistry is the flat, ID-keyed owner of every TypeDef in a Document
// (ir-design §2, §4); every other node references types by TypeID. JSON
// (un)marshaling of the sealed sum is defined with the rest of the sum-type
// codec.
type TypeRegistry map[TypeID]TypeDef

// Document is the root of a Morphic IR document (ir-design §2). It is
// self-contained: no node references anything outside it.
type Document struct {
	// IRVersion is the version of the IR schema itself (semver).
	IRVersion string `json:"irVersion,omitempty"`
	// IDSpaces declares, for each kind prefix (IDKinds), the namespaces the
	// document's IDs of that kind live in. A producer states it as it stamps
	// IRVersion, from the namespace constants it already names; irverify reports
	// an ID whose namespace is not declared, which is the one way to see a path
	// glued onto its namespace. IDSpacePrim is ir's own and needs no entry. Each
	// list is sorted, without repeats, and names no empty or separator-bearing
	// namespace.
	IDSpaces map[string][]string `json:"idSpaces,omitempty"`
	// Name is the API title.
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
	// Servers holds the endpoint templates.
	Servers []Server `json:"servers,omitempty"`
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
	// Sources describes the input files: format, path, content hash.
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
}
