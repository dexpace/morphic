// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

const module = "github.com/dexpace/morphic"

// subtreeSuffix marks an allowlist entry that admits a whole subtree rather than
// one package.
const subtreeSuffix = "/..."

// rules maps a directory (relative to repo root) to its allowed non-stdlib
// imports; test files are exempt. An unkeyed subdirectory is audited under its
// nearest keyed ancestor's allowlist.
//
// An entry is one exact import path, or a subtree when it ends in "/...". The
// distinction makes "compilers never import each other" expressible:
// compilers/openapi names the contract and framework packages individually, so
// no sibling compiler rides in. Prefer the exact form; a subtree suits an
// external module whose layout is not ours to enumerate, or a compiler's own
// internal tree.
var rules = map[string][]string{
	// What runs code the caller does not own, turns a panic in it into an
	// error, and quotes what that code says. It is below every package that
	// runs such code, a compiler, a pass or a compiler's own dependency, so it
	// reaches the standard library alone.
	"barrier":     {},
	"ir":          {},
	"ir/irfacts":  {module + "/ir"},
	"ir/irtest":   {module + "/ir", "github.com/google/go-cmp" + subtreeSuffix},
	"ir/irverify": {module + "/ir"},
	"compilers":   {module + "/barrier", module + "/ir"},
	// What every compiler shares: the type registry, diagnostics and the naming
	// and ID grammars. It reaches ir alone, so no compiler's vocabulary rides in.
	"compilers/compile": {module + "/ir"},
	"compilers/openapi": {module + "/ir", module + "/compilers", module + "/compilers/compile",
		module + "/compilers/openapi/internal" + subtreeSuffix},
	// The diagnostic vocabulary sits at the bottom of the compiler's own import
	// graph: every package that reports anything reaches it, so it must reach
	// nothing of the compiler's. It reaches ir, and barrier, through which the
	// compiler runs a dependency and quotes what it says. Its own entry says
	// that, rather than letting it inherit the compiler's much wider allowlist
	// by being an unkeyed subdirectory.
	"compilers/openapi/internal/diag": {module + "/barrier", module + "/ir"},
	// The one counting of a compile's work and size against its limits. It
	// reaches the standard library alone, so any package that holds a count to
	// a limit, the lowest of them included, can count through it rather than
	// keep a counter of its own.
	"compilers/openapi/internal/budget": {},
	// Pointer construction and the OpenAPI derivation of an ID from a pointer. It
	// reaches the framework for the grammar wrapped around a path and nothing
	// else: what is OpenAPI's here is the path, and a package that could reach
	// the compiler would be able to derive one from something other than a
	// source coordinate.
	"compilers/openapi/internal/ids": {module + "/ir", module + "/compilers/compile"},
	// The one classification of a YAML scalar, and value lowering built on it.
	// It reaches ir for the value types, yaml for the node it reads and nodeview
	// for a mapping's members as the parser reads them, and nothing else: a
	// value is decided by its own wire spelling, so a package that could reach
	// the compiler would be able to let a surrounding schema type change what a
	// literal means.
	"compilers/openapi/internal/value": {module + "/ir",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// The yaml.v3 nodes tests build trees from: the constructors for the node
	// kinds a parse produces, the `<<` merge key among them. It reaches yaml and
	// nothing else, so any package's internal tests can import it, nodeview's
	// included: an internal test file cannot import a package that imports its
	// own.
	"compilers/openapi/internal/ynode": {"go.yaml.in/yaml/v3"},
	// The OpenAPI Specification's rules as data, and the engine that checks a
	// source tree against them at the minor it declares. It reads the tree
	// through nodeview, reports through diag, counts its work through budget and
	// reaches nothing that loads or lowers: the table is the compiler's own
	// authority on the specification, so it depends on no parser library.
	"compilers/openapi/internal/oas": {module + "/ir", module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// The one reading of a $ref fragment, and the spelling the resolver is handed
	// for it (GitHub #911). It reaches the library's reference type for the
	// resolver's own reading and nothing of the compiler, so the scans, the loader
	// and the lowering can all reach it.
	"compilers/openapi/internal/fragment": {"github.com/speakeasy-api/openapi/references"},
	// The one reading of the raw source: mappings read the way the parser reads
	// them, through aliases and `<<` merge keys, and the one bounded walk of a
	// tree. It reaches the library's yml package for the merge-key predicate the
	// unmarshaller itself uses, and budget for what its memo may hold, and is
	// below every package that reads raw YAML, which reads through it rather
	// than by a reading of its own.
	"compilers/openapi/internal/nodeview": {module + "/compilers/openapi/internal/budget",
		"github.com/speakeasy-api/openapi/yml", "go.yaml.in/yaml/v3"},
	// How the library's walk reads a pointer through the parsed model, a token at
	// a time, and where it leaves the model for raw YAML. It reaches the library's
	// navigation and the root node a model was built from, and nothing of the
	// compiler, so the reference scope and the loader read a pointer as the
	// resolver does, without the key scan its read of raw YAML costs.
	"compilers/openapi/internal/navigation": {"github.com/speakeasy-api/openapi/jsonpointer",
		"github.com/speakeasy-api/openapi/marshaller", "go.yaml.in/yaml/v3"},
	// Every schema $ref of the source resolved into a table over the decoded
	// trees, never through the library's resolver (GitHub #558). It reads a tree
	// through nodeview and a fragment through fragment, counts its work through
	// budget, and reaches nothing of the library's model or of the compiler:
	// what the library resolved of a reference object reaches it as data,
	// through its Config.
	"compilers/openapi/internal/bind": {module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/fragment",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// The typed side of a bind table: each schema of the parsed model known by
	// the position it was read at, located through the library's walk, and one
	// the library's decoder builds from raw YAML where the model holds none,
	// behind the compiler's barrier in diag.
	"compilers/openapi/internal/bind/typed": {module + "/compilers/openapi/internal/bind",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/nodeview",
		"github.com/speakeasy-api/openapi/jsonschema/oas3", "github.com/speakeasy-api/openapi/marshaller",
		"github.com/speakeasy-api/openapi/walk", "go.yaml.in/yaml/v3"},
	// The name of every node the lowering builds, as a function of the documents
	// and the node's coordinate (GitHub #981). It reads the grammar through a
	// bind table, the raw tree through nodeview and a $ref's fragment through
	// fragment, and composes names in compile's grammar. It reaches nothing that
	// lowers, so no lowering can make a name depend on the order it runs in.
	"compilers/openapi/internal/naming": {module + "/compilers/compile",
		module + "/compilers/openapi/internal/bind",
		module + "/compilers/openapi/internal/fragment",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// The $dynamicAnchor declarations of a source tree, which a $dynamicRef
	// expands against. It reads the tree through nodeview, as the parser reads
	// it, spells pointers through ids, counts the nodes it reads through budget,
	// and reaches nothing that lowers.
	"compilers/openapi/internal/anchors": {module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// One walk over the decoded source tree, answering what the pre-lowering
	// refusals would otherwise each walk it to ask. It reaches nodeview for the
	// document root and budget for the steps its measure takes, and nothing
	// else: an index of what the source says is not allowed to depend on what
	// any consumer of it wants to say about that.
	"compilers/openapi/internal/sourceindex": {module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// The pre-lowering refusals. They read the source through nodeview and the
	// index built over it, weigh it against its budgets through budget, report
	// through diag, and reach no part of the lowering — nothing here has a
	// document to lower yet.
	"compilers/openapi/internal/scan": {module + "/ir",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/sourceindex", "go.yaml.in/yaml/v3"},
	// What a schema or a carrier says about itself rather than about its shape,
	// plus how a construct is kept verbatim under Unmodeled, the validation-only
	// keywords among them. It reads the parsed model and the raw nodes behind
	// it, those through nodeview and their scalars through value, and holds no
	// opinion about lowering — so it sits below the schema lowering that asks it
	// questions and above nothing that asks it any. It reaches the library's yml
	// package for the tags its decoder accepts as a string, so a field read off
	// a raw node decodes as the parser decodes the fields it models, and budget
	// for the nodes a kept construct may render.
	"compilers/openapi/internal/annotation": {module + "/ir",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/value",
		"github.com/speakeasy-api/openapi/extensions",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/yml", "go.yaml.in/yaml/v3"},
	// Source-document patching: an OpenAPI Overlay applied to the decoded node
	// tree, and the attribution of what it changed. It reads bytes and nodes and
	// reports through diag, reaching ids for the pointer that names a position,
	// nodeview for the document root and budget for the node budget it builds
	// within — and nothing that lowers, because it runs before there is anything
	// to lower.
	"compilers/openapi/internal/overlay": {module + "/ir",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/nodeview",
		"github.com/speakeasy-api/openapi/overlay", "go.yaml.in/yaml/v3"},
	// The entry side: parse, check, resolve. It indexes the tree its one decode
	// produced through sourceindex, runs the pre-lowering refusals over that index
	// through scan, applies the caller's overlay through overlay, and holds the
	// tree, and what its references reach, to the specification's rules through
	// oas, each within its budget through budget. It reaches resolve to read
	// which mapping values name a position in the source as the lowering will,
	// and the library's pointer walk, directly and through navigation, to tell a
	// hop that walked a document's bytes and to walk a document as it does. It
	// reaches nothing that lowers — at this point there is no document to lower.
	"compilers/openapi/internal/load": {module + "/ir", module + "/compilers",
		module + "/compilers/openapi/internal/bind",
		module + "/compilers/openapi/internal/bind/typed",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/fragment",
		module + "/compilers/openapi/internal/navigation",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/oas",
		module + "/compilers/openapi/internal/overlay",
		module + "/compilers/openapi/internal/resolve",
		module + "/compilers/openapi/internal/scan",
		module + "/compilers/openapi/internal/sourceindex",
		"github.com/speakeasy-api/openapi/jsonpointer",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/marshaller",
		"github.com/speakeasy-api/openapi/openapi",
		"github.com/speakeasy-api/openapi/references",
		"github.com/speakeasy-api/openapi/yml", "go.yaml.in/yaml/v3"},
	// Recognition of a source from its bytes, before the compile. It reaches load
	// for the parse the compile goes on to lower, budget for the byte budget that
	// parse is held to, and nodeview to read the root mapping as the parser does,
	// and nothing that lowers: it runs before anything has decided the bytes are
	// this compiler's.
	"compilers/openapi/internal/detect": {module + "/ir", module + "/compilers",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/load",
		module + "/compilers/openapi/internal/nodeview", "go.yaml.in/yaml/v3"},
	// What a $ref names: the pointer it addresses, the schema declared there and
	// the type already interned there. It reaches bind for where a schema $ref
	// lands, annotation to ask whether a referenced position declares a body at
	// all, navigation to walk a pointer through the model as the resolver does,
	// compile for the registry it looks IDs up in, the library's openapi package
	// to name each kind of reference once (ReferenceEnd), and yaml to stop a walk
	// at raw YAML, which holds none. It reaches nothing that lowers — following a
	// reference far enough to lower its target recurses back into the schema
	// walk, so that stays with the walk.
	"compilers/openapi/internal/resolve": {module + "/ir",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/bind",
		module + "/compilers/openapi/internal/bind/typed",
		module + "/compilers/openapi/internal/fragment",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/navigation",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/openapi",
		"github.com/speakeasy-api/openapi/references",
		"go.yaml.in/yaml/v3"},
	// allOf property reconciliation. It reaches annotation for the one field a
	// redeclaration unions rather than intersects, and takes everything else it
	// needs from lowering — the registry lookup and the recorder — as function
	// fields, which is what keeps the conflict lattice drivable without a walk.
	"compilers/openapi/internal/merge": {module + "/ir",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/spelling"},
	// The table that maps a (type, format) pair to a primitive kind, and the
	// relation between two kinds that table and ir's value domains give. It is its
	// own package because merge reads the relation and may not import schema,
	// whose walk reads the table, and the table is no part of merge's fold. It
	// reaches ir alone.
	"compilers/openapi/internal/spelling": {module + "/ir"},
	// What is being lowered: the parsed document, the identity of the source, and
	// the indexes derived from them at entry. It is the substrate both walks share
	// and so must reach neither, which is why it sits here rather than with either
	// one. It reaches load for the version grammar — the dialect question is asked
	// of the document, and the grammar that answers it is the loader's — and for
	// the schema bindings the load phase built, which its scope reads. It reaches
	// naming for what each position is called, anchors for the $dynamicAnchor
	// index it builds on first ask, and budget for the limits it holds.
	"compilers/openapi/internal/lowering": {module + "/ir",
		module + "/compilers/openapi/internal/anchors",
		module + "/compilers/openapi/internal/bind",
		module + "/compilers/openapi/internal/bind/typed",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/load",
		module + "/compilers/openapi/internal/naming",
		module + "/compilers/openapi/internal/overlay",
		module + "/compilers/openapi/internal/resolve",
		"github.com/speakeasy-api/openapi/openapi"},
	// The security schemes a document declares and the requirements that name
	// them. It is its own package because both sides of the compiler reach it:
	// the compiler's run lowers the schemes, and the service and operation walks
	// lower the requirements against them. It reads a field the model names no
	// member for off the raw node, through nodeview.
	"compilers/openapi/internal/auth": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/resolve",
		"github.com/speakeasy-api/openapi/openapi"},
	// The servers a document declares, whichever object declares them, and the
	// registry they are lowered into. It is its own package for the reason auth
	// is: the service walk lowers the document's own list and the operation walk
	// a path item's and an operation's, into one registry the compiler's run
	// assigns.
	"compilers/openapi/internal/servers": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		"github.com/speakeasy-api/openapi/openapi"},
	// The schema walk, and everything that recurses with it: composition,
	// reference following, and the preservation that hangs off both. It is one
	// package because they are one cycle (micro-compiler-design §5), and it
	// reaches every part of the compiler below it — but nothing above: an import
	// of the compiler package from here is the cycle the extraction removed.
	"compilers/openapi/internal/schema": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/bind",
		module + "/compilers/openapi/internal/bind/typed",
		module + "/compilers/openapi/internal/budget",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/merge",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/resolve",
		module + "/compilers/openapi/internal/spelling",
		module + "/compilers/openapi/internal/value",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/values", "go.yaml.in/yaml/v3"},
	// The operation walk: path items, webhooks and callbacks, the parameters
	// merged onto them, and the content of every body, response and header. It
	// reaches the schema walk for the positions it hoists, auth for the
	// requirements it reads and nodeview for the raw nodes behind the model, and
	// nothing above itself — the compiler assembles the document from what this
	// returns rather than the other way round.
	"compilers/openapi/internal/operation": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/auth",
		module + "/compilers/openapi/internal/bind/typed",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/resolve",
		module + "/compilers/openapi/internal/schema",
		module + "/compilers/openapi/internal/servers",
		"github.com/speakeasy-api/openapi" + subtreeSuffix, "go.yaml.in/yaml/v3"},
	// What a document says about itself: its info, and what the objects around
	// it write that lowers to no node of its own. It runs after both walks and
	// reaches neither: the compiler hands it the context and assigns what it
	// returns onto the Document.
	"compilers/openapi/internal/document": {module + "/ir",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/nodeview",
		"github.com/speakeasy-api/openapi/extensions",
		"github.com/speakeasy-api/openapi/openapi"},
	// The canaries that pin docs/parser-library.md. Its one production file is
	// the package doc: the tests call the library directly, and test files are
	// outside these rules.
	"compilers/openapi/internal/canary": {},
	// The scaffolding the compiler's test packages share. It is a production
	// package only in the sense that it holds non-test files; what governs it is
	// that every test package under compilers/openapi must be able to import it,
	// internal ones included. That is why its allowlist stops at ir, the contract
	// package and diag: an internal test file may not import a package that
	// imports its own, so anything further would shut out the tests of whatever
	// it reached. Widening this entry is how that becomes true silently.
	"compilers/openapi/internal/openapitest": {module + "/ir", module + "/compilers",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/fragment",
		"github.com/speakeasy-api/openapi/jsonpointer",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/marshaller",
		"github.com/speakeasy-api/openapi/openapi",
		"github.com/speakeasy-api/openapi/references",
		"github.com/speakeasy-api/openapi/sequencedmap",
		"github.com/stretchr/testify/assert",
		"github.com/stretchr/testify/require", "go.yaml.in/yaml/v3"},
	"pass": {module + "/ir", module + "/ir/irfacts"},
	// The emitter contract, Layer 2, ruled before any package of it exists
	// (unbuilt), so the first emitter is audited from its first import. It
	// reaches ir and nothing else yet: never a compiler, the engine or another
	// emitter (docs/emitters/packages.md). Each package beneath it carries its
	// own entry (ownEntryRequired), and none goes in exempt (mayBeExempt).
	"emitters": {module + "/ir"},
	// The orchestration. It reaches the compiler package to compose the default
	// registry and nothing of any source format: what a spec looks like and what
	// its options are called are answered through the contract, so no parser is
	// named here. It reads a run's documents through its own reader package.
	"engine": {module + "/barrier", module + "/ir", module + "/ir/irverify", module + "/compilers",
		module + "/compilers/openapi", module + "/engine/reader", module + "/pass"},
	// What a run's references and settings name, read from disk, the network or
	// memory. It reaches the contract whose reader it implements and nothing
	// else, so a caller compiling directly reads as the engine does without
	// composing a pipeline.
	"engine/reader": {module + "/compilers"},
	// The one renderer of a run's findings for a person. It reaches ir alone: it
	// renders what a run returned and nothing of how the run was composed.
	"engine/report": {module + "/ir"},
	// The engine's writer: the IR document to a stream or a file, published
	// through the atomic replace beneath it.
	"engine/output": {module + "/ir", module + "/engine/internal/atomicfile"},
	// The atomic replace every file the engine writes goes through. It reaches
	// renameio, which creates, syncs and renames the file, and nothing of the
	// module, so each writer the engine grows can stand on it.
	"engine/internal/atomicfile": {"github.com/google/renameio/v2"},
	// The CLI. Beyond the pipeline it reaches its command framework: cobra
	// dispatches the commands and renders help, on pflag's parse. What it prints
	// it asks the engine's renderer and writer for.
	"cmd/morphic": {module + "/ir", module + "/engine", module + "/engine/output", module + "/engine/report",
		"github.com/spf13/cobra", "github.com/spf13/pflag"},
	"cmd/morphic-harness": {module + "/internal/harness"},
	// The census command: tooling outside the pipeline, like the harness command
	// above. It compiles specs through the engine, lists them as the harness does,
	// and counts what their documents set.
	"cmd/morphic-census": {module + "/engine", module + "/internal/census", module + "/internal/harness"},
	// The coverage gate's judge: tooling outside the pipeline, like the harness
	// command above. x/tools/cover parses and merges the profile it judges.
	"cmd/morphic-covergate": {"golang.org/x/tools/cover"},
	// The go vet tool that runs internal/lintrules: tooling outside the pipeline,
	// on unitchecker, the driver go vet speaks to.
	"cmd/morphic-vet":   {module + "/internal/lintrules", "golang.org/x/tools/go/analysis/unitchecker"},
	"internal/testspec": {},
	// Test infrastructure outside the pipeline, like internal/harness above —
	// but stdlib-only, which is what lets a package that starts a goroutine in
	// its tests call it without pulling anything else in.
	"internal/leakcheck": {},
	// Test infrastructure too, and stdlib-only for the same reason: the context
	// decorators a test of cancellation puts a cancel at a chosen check with,
	// which tests across the module import.
	"internal/ctxtest": {},
	// Test infrastructure too: tests that hold a hand-written list to what a
	// package declares read it type-checked, through go/packages. Two of its
	// testdata packages are imported by others, which is how a test shows a
	// package reaching a type directly and through another import.
	"internal/decls": {
		"golang.org/x/tools/go/packages",
		module + "/internal/decls/testdata/good/fixture",
		module + "/internal/decls/testdata/good/uses",
	},
	// Test infrastructure as well: it describes the JSON shape of the types one
	// root reaches, which it reads type-checked through decls, for the test in
	// package ir that pins that shape to IRVersion, for the census, and for the
	// tests that read the IR's types.
	"internal/jsonshape": {module + "/internal/decls"},
	// Tooling as well: it counts which facts of the IR a set of documents sets.
	// The facts are the JSON shape jsonshape reads from ir's declarations, and
	// ir's own walk reaches what each document holds.
	"internal/census": {module + "/ir", module + "/internal/decls", module + "/internal/jsonshape"},
	// Test infrastructure as well: the one reader of the repository's keyed
	// documents and of the citations made of them, which the tests holding each
	// document to the code share. Standard library only.
	"internal/keydoc": {},
	// The tables ir's tests and its fuzz targets share: stdlib-only, because
	// a fuzz run pays on every execution for all the code its binary links.
	"internal/ircases": {},
	// The repository's own lint rules: go/analysis analyzers, which read the
	// packages they hold through types and nothing of the module's own.
	"internal/lintrules": {"golang.org/x/tools/go/analysis", "golang.org/x/tools/go/types/typeutil"},
	// What the analyzers refuse, planted in a tree laid out like the module. It
	// reaches what the rules are about: ir's types, the framework, the parser
	// library's reference readings, and its own packages, look-alikes of ir and
	// compile among them.
	"internal/lintrules/testdata/mod": {module + "/ir", module + "/compilers/compile",
		module + "/internal/lintrules/testdata/mod" + subtreeSuffix,
		"github.com/speakeasy-api/openapi/expression", "github.com/speakeasy-api/openapi/references"},
	// Beside that tree, so no rule holds it.
	"internal/lintrules/testdata/outside": {module + "/ir"},
	// What the rules .golangci.yml configures refuse, planted for
	// scripts/lint-planted.sh, and what cmd/morphic-vet refuses, planted for
	// scripts/vet-rules.sh.
	"internal/lintrules/testdata/planted": {module + "/ir"},
	"internal/lintrules/testdata/reach":   {module + "/ir"},
}

// exempt names the production packages deliberately outside the layering rules,
// each with the reason it is out. Only tooling under internal/ may be
// (mayBeExempt): a pipeline package left out would be audited by nothing.
//
// Being unkeyed in rules is not by itself a decision — an omission looks
// identical to one — so TestImportGraph_EveryPackageIsRuledOrExempt requires
// every production package to appear in one map or the other. Adding a package
// to neither fails, which is what stops both lists rotting.
var exempt = map[string]string{
	"internal/harness": "test/tooling infrastructure that drives specs through the oracles, " +
		"so it legitimately imports the Layer-3 engine, whose runs it judges",
}

// unbuilt names the rules entries whose directory does not exist yet, each
// with the reason it is ruled already. A rule for a missing directory audits
// nothing, and so does a misspelt one, so TestImportGraph_LayeringHolds skips
// only these, and TestUnbuilt_NamesOnlyMissingRuledDirectories fails on one
// whose package has landed until it is dropped here.
var unbuilt = map[string]string{
	"emitters": "the emitter contract, ruled before its first package so that package is audited from " +
		"its first import (GitHub #973)",
}

// TestImportGraph_LayeringHolds parses every non-test Go file under each ruled
// directory (imports only) and fails on any import whose path is neither stdlib
// nor covered by that directory's allowlist, and on any allowlist entry no such
// import uses: an entry left behind when a package stops importing something
// keeps admitting it, so the allowlist no longer says what the package reaches.
//
// It stays a test rather than linter configuration (#796): depguard matches raw
// string prefixes and applies every list a file matches at once, so
// nearest-ancestor inheritance cannot be written, and go-arch-lint allows
// components where these entries name packages.
func TestImportGraph_LayeringHolds(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	for dir, allowed := range rules {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			base := filepath.Join(root, dir)
			_, statErr := os.Stat(base)
			skip, err := unbuiltSkip(dir, statErr)
			require.NoError(t, err)
			if skip {
				t.Skipf("directory %s does not exist yet: %s", dir, unbuilt[dir])
			}
			violations, unused, err := walkImports(dir, base, allowed)
			require.NoError(t, err)
			for _, v := range violations {
				t.Error(v)
			}
			for _, entry := range unused {
				t.Errorf("%s allows %q, which no production file it audits imports; drop the entry", dir, entry)
			}
		})
	}
}

// unbuiltSkip decides a ruled directory whose stat returned statErr: skipped
// when it is missing and unbuilt names it, audited when it exists, and
// refused otherwise, since a misspelt entry would audit nothing.
func unbuiltSkip(dir string, statErr error) (bool, error) {
	if statErr == nil {
		return false, nil
	}
	if _, planned := unbuilt[dir]; planned && errors.Is(statErr, fs.ErrNotExist) {
		return true, nil
	}
	return false, fmt.Errorf("rules names %s, which cannot be audited; fix the entry, "+
		"or name a package not yet built in unbuilt: %w", dir, statErr)
}

// TestUnbuiltSkip_SkipsOnlyAMissingDirectoryUnbuiltNames pins the decision:
// a missing directory unbuilt names is skipped, an existing one audited, and
// any other missing or unreadable directory refused rather than skipped.
func TestUnbuiltSkip_SkipsOnlyAMissingDirectoryUnbuiltNames(t *testing.T) {
	t.Parallel()
	denied := &fs.PathError{Op: "stat", Path: "emitters", Err: fs.ErrPermission}
	for _, tc := range []struct {
		name, dir string
		statErr   error
		skip      bool
		wantIs    error
	}{
		{"a planned directory, missing", "emitters", fs.ErrNotExist, true, nil},
		{"a planned directory, present", "emitters", nil, false, nil},
		{"a ruled directory, present", "engine", nil, false, nil},
		{"a misspelt directory", "engnie", fs.ErrNotExist, false, fs.ErrNotExist},
		{"a planned directory that cannot be read", "emitters", denied, false, fs.ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			skip, err := unbuiltSkip(tc.dir, tc.statErr)

			assert.Equal(t, tc.skip, skip)
			if tc.wantIs == nil {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantIs)
			assert.Contains(t, err.Error(), "rules names "+tc.dir)
		})
	}
}

// TestImportGraph_ContractEntryAdmitsNoSiblingCompiler states the rule the
// allowlist exists for, at the matcher rather than through the tree: a compiler
// reaches the contract package and the shared framework package by naming each,
// and no sibling compiler comes with them.
//
// It is asked of the live rules entry, not a fixture, because the hole was in
// how that entry was read: every path here was allowed while the match was a
// segment prefix, so a compiler could import another and the suite stayed green
// (GitHub #57).
func TestImportGraph_ContractEntryAdmitsNoSiblingCompiler(t *testing.T) {
	t.Parallel()
	allowed := rules["compilers/openapi"]
	require.NotEmpty(t, allowed, "rules must still carry the compilers/openapi entry")
	allows := func(ip string) bool { return admit(ip, allowed, make([]bool, len(allowed))) }
	assert.True(t, allows(module+"/compilers"), "the contract package")
	assert.True(t, allows(module+"/compilers/compile"), "the shared framework package")
	assert.True(t, allows(module+"/compilers/openapi/internal/schema"),
		"a subtree entry still admits the packages under it")
	assert.False(t, allows(module+"/compilers/graphql"),
		"a sibling compiler must not ride in on the contract entry")
	assert.False(t, allows(module+"/engine"), "nor a layer above")
}

// TestWalkImports_NestedLookAlikeIsAudited plants the tree the skip decision
// used to miss: a directory whose basename matches a ruled sibling's but whose
// path carries no rule of its own. Skipping it left it audited by nothing at
// all, while TestImportGraph_EveryPackageIsRuledOrExempt still reported it
// covered — its ancestor is ruled (GitHub #57).
//
// Both directories are planted so the fix cannot be "stop skipping": the real
// ir/irtest child must still be skipped for its own rule, or it would be judged
// by ir's empty allowlist and fail for imports it is entitled to.
func TestWalkImports_NestedLookAlikeIsAudited(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeImporter(t, filepath.Join(root, "sub", "irtest", "nested.go"), "github.com/google/go-cmp/cmp")
	writeImporter(t, filepath.Join(root, "irtest", "child.go"), "github.com/google/go-cmp/cmp")

	violations, _, err := walkImports("ir", root, rules["ir"])
	require.NoError(t, err)
	require.Len(t, violations, 1, "exactly the nested look-alike is audited: %v", violations)
	assert.Contains(t, violations[0], filepath.Join("sub", "irtest", "nested.go"))
}

// TestWalkImports_AnInheritingPackageIsNamed plants the package a new compiler
// starts as: a directory under compilers/ with no entry of its own, importing
// the contract package its inherited allowlist forbids. The message must name
// the missing entry, which is the fix, rather than the import alone.
func TestWalkImports_AnInheritingPackageIsNamed(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	writeImporter(t, filepath.Join(base, "newformat", "newformat.go"), module+"/compilers/compile")

	violations, _, err := walkImports("compilers", base, rules["compilers"])
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0], "compilers/newformat has no rules entry of its own")
}

// TestWalkImports_AnUnusedAllowanceIsReported plants a tree that imports one
// allowed entry directly and another both exactly and through its subtree, and
// holds the walk to naming the remaining entry alone. A subtree entry is used
// by any import beneath it, as entryAdmits admits one, and an import two
// entries admit uses both; an entry only an unaudited test file imports is
// still unused.
func TestWalkImports_AnUnusedAllowanceIsReported(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	writeImporter(t, filepath.Join(base, "a.go"), "example.com/used")
	writeImporter(t, filepath.Join(base, "b.go"), "example.com/tree/leaf")
	writeImporter(t, filepath.Join(base, "c_test.go"), "example.com/unused")

	violations, unused, err := walkImports("planted", base, []string{"example.com/used",
		"example.com/tree" + subtreeSuffix, "example.com/tree/leaf", "example.com/unused"})
	require.NoError(t, err)
	assert.Empty(t, violations)
	assert.Equal(t, []string{"example.com/unused"}, unused)
}

// TestOwnEntryRequired_HoldsEveryPackageUnderCompilersAndEmitters pins the
// subtrees the stricter rule covers: each package under compilers/ or
// emitters/, at any depth, and nothing outside them, where nearest-ancestor
// inheritance is deliberate.
func TestOwnEntryRequired_HoldsEveryPackageUnderCompilersAndEmitters(t *testing.T) {
	t.Parallel()
	for dir, want := range map[string]bool{
		"compilers":                       true,
		"compilers/newformat":             true,
		"compilers/openapi/internal/zz":   true,
		"compilers/openapi/internal/a/b":  true,
		"emitters":                        true,
		"emitters/golang":                 true,
		"emitters/golang/goast":           true,
		"compilersx":                      false,
		"emittersx":                       false,
		"engine":                          false,
		"internal/harness":                false,
		"internal/lintrules/testdata/mod": false,
	} {
		assert.Equal(t, want, ownEntryRequired(dir), dir)
	}
}

// TestWalkImports_AnEmitterReachingAboveIrIsRefused plants the package the
// first emitter starts as, under the live emitters entry: a target with no
// entry of its own importing a compiler, the compiler contract and the engine,
// each refused and the missing entry named, and ir, which every emitter
// reaches, admitted (invariant 1).
func TestWalkImports_AnEmitterReachingAboveIrIsRefused(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	refused := []string{module + "/compilers/openapi", module + "/compilers", module + "/engine"}
	for i, imp := range append([]string{module + "/ir"}, refused...) {
		writeImporter(t, filepath.Join(base, "golang", fmt.Sprintf("f%d.go", i)), imp)
	}

	violations, unused, err := walkImports("emitters", base, rules["emitters"])
	require.NoError(t, err)
	assert.Empty(t, unused, "the planted ir import uses the entry")
	require.Len(t, violations, len(refused), "%v", violations)
	for i, imp := range refused {
		assert.Contains(t, violations[i], `"`+imp+`"`)
		assert.Contains(t, violations[i], "emitters/golang has no rules entry of its own")
	}
}

// TestMayBeExempt_AdmitsOnlyInternal pins the predicate: a package under
// internal/, the tooling outside the pipeline, and nothing else.
func TestMayBeExempt_AdmitsOnlyInternal(t *testing.T) {
	t.Parallel()
	for dir, want := range map[string]bool{
		"internal/harness":  true,
		"internal/a/b":      true,
		"internal":          false,
		"internalx/harness": false,
		"emitters/golang":   false,
		"compilers/openapi": false,
		"engine":            false,
		"cmd/morphic":       false,
		"pass":              false,
	} {
		assert.Equal(t, want, mayBeExempt(dir), dir)
	}
}

// mayBeExempt reports whether dir may be named in exempt: a package under
// internal/, test and tooling infrastructure the pipeline never imports.
func mayBeExempt(dir string) bool {
	return strings.HasPrefix(dir, "internal/")
}

// TestUnbuilt_NamesOnlyMissingRuledDirectories holds unbuilt to what it is
// for: each entry names a rules entry whose directory does not exist yet. One
// whose package has landed is audited, so its entry would only hide a later
// removal.
func TestUnbuilt_NamesOnlyMissingRuledDirectories(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	for dir := range unbuilt {
		assert.Contains(t, rules, dir, "unbuilt names %s, which has no rules entry", dir)
		_, err := os.Stat(filepath.Join(root, dir))
		assert.ErrorIs(t, err, fs.ErrNotExist, "%s exists now; drop it from unbuilt", dir)
	}
}

// writeImporter writes a minimal parseable Go file at file importing imp, so a
// planted tree exercises walkImports without a buildable package.
func writeImporter(t *testing.T, file, imp string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := "package planted\n\nimport _ \"" + imp + "\"\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
}

// TestImportGraph_EveryPackageIsRuledOrExempt requires every directory holding
// production Go files to be reached by rules — through itself or an ancestor,
// and under compilers/ and emitters/ through itself (ownEntryRequired) — or
// named in exempt with its reason, which only tooling may be (mayBeExempt): a
// pipeline package left out would be audited by nothing.
//
// A package in neither is audited by nothing, and looks no different from one
// deliberately left out: the layering gate stays green while that package
// imports whatever it likes. Requiring the choice to be recorded is what makes
// the next package added an explicit decision rather than a silent omission.
func TestImportGraph_EveryPackageIsRuledOrExempt(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	pkgs := productionPackages(t, root)
	require.NotEmpty(t, pkgs, "found no production packages to audit")

	for _, dir := range pkgs {
		if _, ok := exempt[dir]; ok {
			assert.False(t, coveredByRule(dir), "%s is both ruled and exempt; pick one", dir)
			continue
		}
		assert.True(t, coveredByRule(dir),
			"%s holds production Go files but no import rule reaches it; "+
				"add it to rules, or name it in exempt with the reason it is out", dir)
		_, own := rules[dir]
		assert.False(t, ownEntryRequired(dir) && !own,
			"%s has no rules entry of its own: a package under compilers/ or emitters/ needs one, since an "+
				"inherited allowlist says nothing about where it sits (for a compiler, CONTRIBUTING.md, Add a "+
				"compiler, step 2)", dir)
	}
	for dir := range exempt {
		assert.Contains(t, pkgs, dir, "exempt names %s, which holds no production Go files", dir)
		assert.True(t, mayBeExempt(dir), "%s is a pipeline package; rule it instead of exempting it", dir)
	}
}

// ownEntryRequired reports whether dir must carry a rules entry of its own
// rather than inherit an ancestor's. Under compilers/ and emitters/ an
// inherited allowlist is never the intended one: a new compiler or target
// would inherit its contract package's, which forbids the shared packages each
// imports, and a package inside one its parent's, which admits every sibling
// the ordering among them exists to keep out.
func ownEntryRequired(dir string) bool {
	for _, layer := range []string{"compilers", "emitters"} {
		if dir == layer || strings.HasPrefix(dir, layer+"/") {
			return true
		}
	}
	return false
}

// coveredByRule reports whether dir or one of its ancestors carries an import
// rule, which is what decides whether walkImports ever reaches it.
func coveredByRule(dir string) bool {
	for d := dir; ; d = path.Dir(d) {
		if _, ok := rules[d]; ok {
			return true
		}
		if d == "." || d == "/" {
			return false
		}
	}
}

// productionPackages returns every repo-relative directory holding at least one
// non-test Go file, deduplicated and in lexical order. Dot-directories are
// skipped whole, so neither .git nor tooling state reaches the audit.
func productionPackages(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isProductionGoFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(p))
		require.NoError(t, relErr)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	slices.Sort(out)
	return slices.Compact(out)
}

// isProductionGoFile reports whether name is a Go file the layering rules apply
// to. walkImports exempts test files, so the package sweep must exempt them too
// — otherwise a test-only directory would be required to carry a rule that
// governs none of its files.
func isProductionGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// walkImports traverses base and returns one message per production Go file
// import that allowed does not cover, and the entries of allowed that no such
// import uses. It excludes nested directories that carry their own rule so each
// subtree is checked exactly once under its most specific rule.
//
// It returns both rather than reporting them, so the planted-tree tests above
// can assert what a walk finds instead of only that a real walk is clean. One
// walk answers both, so they cannot disagree about which files they audit.
func walkImports(dir, base string, allowed []string) (violations, unused []string, err error) {
	used := make([]bool, len(allowed))
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipIfRuled(dir, base, p)
		}
		if !isProductionGoFile(d.Name()) {
			return nil
		}
		found, ferr := fileViolations(p, inheritedBy(dir, base, filepath.Dir(p)), allowed, used)
		if ferr != nil {
			return ferr
		}
		violations = append(violations, found...)
		return nil
	})
	for i, entry := range allowed {
		if !used[i] {
			unused = append(unused, entry)
		}
	}
	return violations, unused, err
}

// skipIfRuled reports filepath.SkipDir for a directory carrying its own rules
// entry, which walkImports visits under that entry instead.
//
// The entry is looked up by the directory's full path under dir, not by its
// basename: with a basename, *any* directory at any depth sharing a ruled
// sibling's name was skipped — and skipped by a walk that never reached it under
// its own rule either, so nothing audited it (GitHub #57).
func skipIfRuled(dir, base, p string) error {
	if p == base {
		return nil
	}
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return err
	}
	if _, own := rules[path.Join(dir, filepath.ToSlash(rel))]; own {
		return filepath.SkipDir
	}
	return nil
}

// inheritedBy names the rule a file in pkgDir is audited under: dir itself when
// pkgDir is base, the directory dir's entry keys, and otherwise dir with a note
// that the package has no entry of its own. A violation in an inheriting
// package is most often that missing entry rather than a wrong import, and a
// message naming only the ancestor's allowlist sends its reader to the import.
func inheritedBy(dir, base, pkgDir string) string {
	rel, err := filepath.Rel(base, pkgDir)
	if err != nil || rel == "." {
		return dir
	}
	return dir + " (" + path.Join(dir, filepath.ToSlash(rel)) + " has no rules entry of its own and inherits it)"
}

// fileViolations parses one file's import declarations and returns a message for
// each disallowed non-stdlib import, marking in used each entry of allowed that
// admits any of its imports. dir names the rule it is audited under.
func fileViolations(p, dir string, allowed []string, used []bool) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, `"`)
		if first, _, _ := strings.Cut(ip, "/"); !strings.Contains(first, ".") {
			continue // stdlib: first path element has no dot
		}
		// An empty allowlist (the "ir" rule) forbids every non-stdlib import.
		if !admit(ip, allowed, used) {
			violations = append(violations,
				fmt.Sprintf("%s imports %q: not allowed for %s (allowed: %v)", p, ip, dir, allowed))
		}
	}
	return violations, nil
}

// admit reports whether allowed covers the import path ip, marking in used
// every entry that does, not only the first: whether an entry counts as used
// must not depend on where the allowlist lists it.
func admit(ip string, allowed []string, used []bool) bool {
	admitted := false
	for i, entry := range allowed {
		if entryAdmits(entry, ip) {
			used[i], admitted = true, true
		}
	}
	return admitted
}

// entryAdmits reports whether one allowlist entry covers the import path ip:
// the entry matches ip exactly, or — written with the subtree suffix — matches
// ip and everything under it. Matching a subtree on segment boundaries is what
// keeps "ir" from admitting "irtest".
func entryAdmits(entry, ip string) bool {
	root, subtree := strings.CutSuffix(entry, subtreeSuffix)
	return ip == root || (subtree && strings.HasPrefix(ip, root+"/"))
}
