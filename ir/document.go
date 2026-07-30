package ir

// IRVersion is the semver of the IR schema itself. Compilers stamp it into
// Document.IRVersion; consumers compare against it to detect schema drift.
//
// It names a schema generation, not a commit. A line of work that changes the
// JSON shape several times bumps it once, where it lands on main, rather than
// once per change: a version that moves within an unmerged branch tells a
// consumer nothing and rewrites every golden each time it moves. Pre-1.0 is no
// exemption from moving it at all — a shape change that reaches main without a
// bump leaves a consumer pinned to the old version accepting a document it
// cannot read, which is the one thing this constant exists to prevent.
//
// 0.2.0 covers three shape changes made together: Extensions became Preserved
// with RawConfig split out, Content.ItemEncoding became a single encoding
// rather than a sentinel-keyed map, and the diagnostic code
// pass/dangling-auth-ref became ir/dangling-auth-ref (see pass's package doc).
//
// 0.3.0 renames that field to Unmodeled on every carrier, so the JSON key
// "preserved" is now "unmodeled". A consumer pinned to 0.2.0 finds no key it
// recognizes and drops every unmodeled construct in silence.
const IRVersion = "0.3.0"

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
	// Name is the API title.
	Name string `json:"name,omitempty"`
	// Version is the source-declared API version string.
	Version string `json:"version,omitempty"`
	// Docs is the document-level documentation.
	Docs Docs `json:"docs"`
	// Contact is the API contact (OpenAPI/AsyncAPI info.contact).
	Contact *Contact `json:"contact,omitempty"`
	// License is the API license (OpenAPI/AsyncAPI info.license).
	License *License `json:"license,omitempty"`
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
