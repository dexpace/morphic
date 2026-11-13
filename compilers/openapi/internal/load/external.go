// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// external reads the documents a reference names beside the source, when
// Options.Reader lets a reference leave the source (bind.Config.Open): each
// through that reader, once in the compile however it is spelled, and prepared
// as Load prepares the source: the same budgets and pre-parse refusals, then
// readyForModel. Anchors are released only behind the refusals, which reject a
// recursive one (docs/parser-library.md#alias-cycle, GitHub #536).
type external struct {
	opts Options
	// read holds every document prepared and the answer each document's one
	// read got.
	read *externalReads
	// version is the source's OpenAPI version, whose rows read a document
	// that declares none (readsID).
	version string
}

// newExternal returns the reader of the documents the references of a source
// declaring version name, recording in read what it prepares and how each
// document's read was answered.
func newExternal(version string, opts Options, read *externalReads) *external {
	return &external{opts: opts, read: read, version: version}
}

// readAs answers the read of the document id names, asked for by the spelling
// key at location: judged first by key's own spelling (unnamed), then as the
// one read in the compile of that document answered it (see
// externalReads.once), whichever of its spellings asked first, so a file a
// $ref spells two ways is read and charged once. The tree it prepared is held
// under key too.
func (e external) readAs(ctx context.Context, id, key, location string) answer {
	if e.opts.Reader == nil {
		return answer{err: refused(key, "no reader was supplied to read it")}
	}
	if err := e.unnamed(location); err != nil {
		return answer{err: refused(key, err.Error())}
	}
	a := e.read.once(id, func() answer { return e.fetch(ctx, id, key, location) })
	if a.err != nil {
		return a
	}
	e.read.adopt(key, a.prepared)
	return a
}

// fileNamed returns the file the path name names: its dot segments removed, as
// a URI reference's are before anything is read. The resolver cleans a relative
// $ref this way and hands an absolute one over as written, so left to the
// reader, one through a directory that is not there failed, and one through a
// link read another file (GitHub #780). A relative path whose first segment
// holds a colon keeps a leading "./", as RFC 3986 §4.2 writes one, so a reader
// never reads it as a URL. An empty name names no file, where cleaned it would
// name the current directory.
func fileNamed(name string) string {
	if name == "" {
		return name
	}
	clean := filepath.Clean(name)
	if refscope.IsURL(clean) && !filepath.IsAbs(clean) {
		return "./" + clean
	}
	return clean
}

// documentID names the document a URL key, as a request spells it, reads: the
// file a file URI names, as fileNamed names one, or the URL itself.
func documentID(key string) string {
	if file, ok := refscope.FileOf(key); ok {
		return fileNamed(file)
	}
	return key
}

// fetch reads the document id names at location through Options.Reader,
// confined to Options.Within and held to the byte budget, and prepares it
// under key, the spelling that asked for it. A refusal names the document by
// id, which no spelling asking first changes.
func (e external) fetch(ctx context.Context, id, key, location string) answer {
	data, err := e.opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{
		Location: location, Within: e.opts.Within, Limit: ReadLimit(e.opts.MaxSourceBytes),
	})
	if err != nil {
		return answer{err: readFailure(e.opts, id, err)}
	}
	data, err = e.prepare(ctx, id, key, data)
	if err != nil {
		return answer{err: err}
	}
	return answer{body: data, prepared: key}
}

// unnamed returns why a read confined to Options.Within refuses the document
// read at location unread, or nil: a file URI names it. A file read at a path
// is no file URI, though its name may start like one ("./file:a.yaml").
func (e external) unnamed(location string) error {
	if e.opts.Within != "" && isFileURI(location) {
		return errors.New("it is named by a file URI" + followedWhenAllowed)
	}
	return nil
}

// followedWhenAllowed ends the reason a confined read refuses a document by
// how a $ref spells it.
const followedWhenAllowed = ", which a read follows only when external references are allowed"

// refusedByName returns why a read refuses a $ref by how its document part is
// written, uri, or nil, judging each $ref alone so that no order of reading
// changes the answer. Confined to opts.Within, it refuses an absolute file path
// naming any document but the source, named self, since a relative $ref reaches
// the same file however the working directory is spelled (GitHub #780). In
// either mode it refuses a URL whose scheme is none of http, https and file,
// which a reader would take for a path against the working directory. With no
// reader it is nil.
func refusedByName(opts Options, self string) func(uri string) error {
	if opts.Reader == nil {
		return nil
	}
	confined := opts.Within != ""
	return func(uri string) error {
		if !refscope.IsURL(uri) {
			if confined && filepath.IsAbs(uri) && !namesSource(self, uri) {
				return refused(uri, "the $ref names it by an absolute path"+followedWhenAllowed)
			}
			return nil
		}
		// IsURL found a scheme, which is all a URL's text holds before its first ':'.
		scheme, _, _ := strings.Cut(uri, ":")
		switch scheme = strings.ToLower(scheme); scheme {
		case "http", "https", "file":
			return nil
		default:
			return refused(uri, fmt.Sprintf("it is named by a URL with the scheme %q, "+
				"and only http, https and file URLs are read", scheme))
		}
	}
}

// namesSource reports whether uri, a $ref's document part, names the source
// at self, read as every reference is (resolve.Locate).
func namesSource(self, uri string) bool {
	named, ok := refscope.Locate(uri, self)
	return ok && refscope.SameDocument(self, named)
}

// readFailure returns the error a reference into the document under key fails
// with when the reader, asked under opts, refused it: a document past the byte
// budget, outside Options.Within or on the network a confined read never
// reaches is refused with the reason, and any other failure is the reader's
// own account of it, passed through.
func readFailure(opts Options, key string, err error) error {
	switch {
	case errors.Is(err, compilers.ErrDocumentTooLarge):
		return refused(key, fmt.Sprintf("it is past the %d-byte budget", opts.MaxSourceBytes.Cap()))
	case errors.Is(err, compilers.ErrOutsideWithin) && overNetwork(key):
		return refused(key, "it is on the network, which a read reaches only when external references are allowed")
	case errors.Is(err, compilers.ErrOutsideWithin):
		return refused(key, fmt.Sprintf("it lies outside the source's directory %s, "+
			"which a read leaves only when external references are allowed", directoryNamed(opts.Within)))
	default:
		return err
	}
}

// directoryNamed is dir as a refusal names it: quoted, or, for the working
// directory, which a source named by a bare file name lies in, said so.
func directoryNamed(dir string) string {
	if dir == "." {
		return "(the working directory)"
	}
	return strconv.Quote(dir)
}

// prepare records the tree the bytes of the document id names parse to under
// key, once the source's refusals pass it, readied as the source's is, and
// returns the bytes. It runs once per document read (see externalReads.once).
//
// The byte budget is checked again, though the reader held the read to it:
// admitting bytes past it would let a reader lift a budget the compile set.
// Unparsable or empty bytes are refused as such, the parser's reason kept
// (bind.UnparsableError, bind.ErrEmptyDocument).
func (e external) prepare(ctx context.Context, id, key string, data []byte) ([]byte, error) {
	if !e.opts.MaxSourceBytes.Admits(len(data)) {
		return nil, refused(id, fmt.Sprintf("it is past the %d-byte budget", e.opts.MaxSourceBytes.Cap()))
	}

	root, err := parseTree(ctx, data)
	if err != nil {
		return nil, stopped(ctx, &bind.UnparsableError{Err: err})
	}
	if bind.IsEmpty(root) {
		return nil, bind.ErrEmptyDocument
	}
	if err := e.admit(ctx, id, root); err != nil {
		return nil, err
	}
	readyForModel(ctx, root)
	e.read.recordTree(key, root, data)
	// A screen or walk above that a cancel cut short has admitted nothing.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// parseTree parses data as the parser parses a document, its first document,
// and returns the parser's error for bytes that do not parse, or
// ctx's once it is done: the parse reads the bytes through ctx
// (contextReader). Bytes holding no document parse to an empty tree, as
// yaml.Unmarshal parses them.
func parseTree(ctx context.Context, data []byte) (*yaml.Node, error) {
	var root yaml.Node
	err := yaml.NewDecoder(contextReader{ctx: ctx, r: bytes.NewReader(data)}).Decode(&root)
	if errors.Is(err, io.EOF) {
		return &root, nil
	}
	return &root, stopped(ctx, err)
}

// admit holds an external document's tree to what Load holds the source's to:
// the pre-parse refusals, then the node budget.
//
// Any finding refuses it, the scan's report that it could not finish included,
// where the source carries that one forward as a warning. A refused document
// is not read into the compile, so it has no Document.Sources entry a finding
// could name: the reference that named the document is what fails, with the
// findings as its reason.
func (e external) admit(ctx context.Context, key string, root *yaml.Node) error {
	place := nowhere()
	findings := refusals(ctx, place, place.Node(nil), root, e.opts)
	if len(findings) > 0 {
		reasons := make([]string, len(findings))
		for i, d := range findings {
			reasons[i] = d.Message
		}
		return refused(key, strings.Join(reasons, "; "))
	}
	if nodes := nodeCount(ctx, root); !e.opts.MaxSourceNodes.Admits(nodes) {
		return refused(key, fmt.Sprintf("it parses to %d nodes, past the %d-node budget",
			nodes, e.opts.MaxSourceNodes.Cap()))
	}
	if reason, refuse := reachesLocal(ctx, root, key, e.readsID(root)); refuse {
		return refused(key, "it is fetched over the network and "+reason+
			", which a remote document must not do")
	}
	return nil
}

// readsID returns whether the binder reads a schema of root, a document read
// beside the source, written in dialect as naming its identifier `id`: by the
// rows dialect has at the version root declares, or the source's, as the
// binder asks them (bindAll).
func (e external) readsID(root *yaml.Node) func(dialect string) bool {
	d := &dialects{spec: e.read.self.judged.checker.Spec(), version: e.version}
	declared := declaredVersion(root)
	return func(dialect string) bool {
		_, _, rows := d.of(declared, dialect)
		return dialectRuleOf(rows.Schema, dialect).Keyword == "id"
	}
}

// reachesLocal returns why root, a document fetched over the network, must not
// be read, when a $ref or an identifier in it takes a read to the local file
// system, and whether one does (GitHub #929). base keys the document.
//
// A $ref or $id naming a local location against base does (localTarget). An
// `id` does only where a dialect root's `$schema` or jsonSchemaDialect names
// reads it as an identifier (readsID), as draft 4 does, so an example's is
// none. A value is read through an alias. The reason names the one at the
// least pointer (scan.Least), which no declaration order moves.
func reachesLocal(ctx context.Context, root *yaml.Node, base string, readsID func(dialect string) bool,
) (reason string, refuse bool) {
	if !overNetwork(base) {
		return "", false
	}
	r := localReading{base: base, named: map[string]bool{declaredDialect(root): true},
		refs: map[*yaml.Node]string{}, ids: map[*yaml.Node]string{}}
	nodeview.Walk{Nodes: maxTreeNodes, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		r.read(n)
		return nodeview.Descend
	}}.Run(ctx, root)
	if len(r.refs) > 0 {
		return leastOf(ctx, root, r.refs), true
	}
	for dialect := range r.named {
		if readsID(dialect) && len(r.ids) > 0 {
			return leastOf(ctx, root, r.ids), true
		}
	}
	return "", false
}

// localReading is what a walk of a document read from base has found that can
// take a read of it to the local file system: why each $ref or $id value that
// does, and each `id` value that would were it an identifier, does, by the
// value's node, and each dialect a `$schema` or its jsonSchemaDialect names.
type localReading struct {
	base      string
	refs, ids map[*yaml.Node]string
	named     map[string]bool
}

// read notes what the mapping n writes itself, a value's alias followed.
func (r *localReading) read(n *yaml.Node) {
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		key, own := nodeview.KeyOf(n.Content[i])
		value := nodeview.Deref(n.Content[i+1])
		if !own || value == nil || value.Kind != yaml.ScalarNode {
			continue
		}
		switch key {
		case "$ref", "$id":
			if why := localReference(key, value.Value, r.base); why != "" {
				r.refs[value] = why
			}
		case "id":
			if why := localID(value.Value, r.base); why != "" {
				r.ids[value] = why
			}
		case "$schema":
			r.named[value.Value] = true
		}
	}
}

// leastOf returns why the value at the least pointer of root among found
// reaches the local file system: the nodes are handed over in the order the
// document writes them, so of a key written twice the first is named.
func leastOf(ctx context.Context, root *yaml.Node, found map[*yaml.Node]string) string {
	nodes := slices.SortedFunc(maps.Keys(found), func(a, b *yaml.Node) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
	})
	return found[scan.Least(ctx, root, nodes)]
}

// localReference returns why a $ref or $id, key, written value in a document
// read from base takes the resolver to the local file system: a $ref naming a
// local file, or a $id naming a local location. It is "" for one that does not.
func localReference(key, value, base string) string {
	t, local := localTarget(value, base)
	switch {
	case !local:
		return ""
	case key == "$ref":
		return fmt.Sprintf("its $ref %q reaches the local file %s", value, t)
	default:
		return fmt.Sprintf("its $id %q rebases references onto the local %s", value, t)
	}
}

// localID returns why an `id` written value in a document read from base would
// rebase references onto the local file system, were it read as JSON Schema
// draft 4 reads it: where it names a location on disk as the binder, which
// alone reads one, resolves it (bind.IdentifierURI). It is "" for one that
// stays on the network, as a relative one does, or names nothing.
func localID(value, base string) string {
	if t, local, named := bind.IdentifierURI(value, base); named && local {
		return fmt.Sprintf("its id %q rebases references onto the local %s", value, t)
	}
	return ""
}

// overNetwork reports whether key names a document the reader fetched over the
// network: an http or https URL. A file: URI is local, and so is a path.
func overNetwork(key string) bool {
	u, err := url.Parse(key)
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

// localTarget returns the location the $ref or $id value ref, read against
// base, names (resolve.Locate), and whether a reader reads it from disk: a
// location with no scheme of two letters or more, a path or a drive letter's,
// or a file: URI. A URL of any other scheme is no file, and a value no reading
// can place names nothing, each left for the read to refuse or report.
func localTarget(ref, base string) (string, bool) {
	uri, _, _ := strings.Cut(ref, "#")
	target, ok := refscope.Locate(uri, base)
	if !ok {
		return "", false
	}
	u, err := url.Parse(target)
	return target, err != nil || len(u.Scheme) < 2 || strings.EqualFold(u.Scheme, "file")
}

// isFileURI reports whether s is a file: URI.
func isFileURI(s string) bool {
	u, err := url.Parse(s)
	return err == nil && strings.EqualFold(u.Scheme, "file")
}

// nowhere places an external document's findings, which leave as the text of
// an error and never as diagnostics, so where each is does not matter.
func nowhere() provenance.Tree {
	return provenance.Tree{Doc: provenance.Doc{Source: ir.NoSource}}
}

// refused returns the error the read of the document under key fails with,
// refused for reason, which the binder reports as the reason each reference
// naming it could not be resolved (bind.Unreadable).
func refused(key, reason string) error {
	return fmt.Errorf("external document %s refused: %s", key, reason)
}
