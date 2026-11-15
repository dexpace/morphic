// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
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
	// aside holds what readying a document set aside for its model's build,
	// put back with the source's.
	aside *aside
}

// newExternal returns the reader of the documents a source's references name,
// recording in read what it prepares and how each document's read was
// answered, and in aside what readying each set aside for its model.
func newExternal(opts Options, read *externalReads, aside *aside) *external {
	return &external{opts: opts, read: read, aside: aside}
}

// readAs answers the read of the document id names, asked for by the spelling
// key at location, by a reference written in the document named from: judged
// first by key's own spelling (unnamed), then as the one read in the compile
// of that document answered it (see externalReads.once), whichever of its
// spellings asked first, so a file a $ref spells two ways is read and charged
// once, and last by what from may reach (reachOf, external.admits). The tree
// it prepared is held under key too.
func (e external) readAs(ctx context.Context, id, key, location, from string) answer {
	if e.opts.Reader == nil {
		return answer{err: refused(key, "no reader was supplied to read it")}
	}
	if err := e.unnamed(key, location); err != nil {
		return answer{err: refused(key, err.Error())}
	}
	reach := e.reachOf(from)
	a := e.read.once(id, reach, func() answer { return e.fetch(ctx, id, key, location, reach) })
	if err := e.admits(ctx, id, location, a, reach); err != nil {
		return answer{err: err}
	}
	if a.err != nil {
		return a
	}
	e.read.adopt(key, a.prepared)
	return a
}

// reachOf is the most private address a reference written in the document
// named from may dial: no bound where from was not read over http(s), a file
// or a source that is one; else the space from's read stated, and public
// where it stated none, or where from is a source located at a URL, whose
// dial the compile was not told of.
func (e external) reachOf(from string) compilers.AddressSpace {
	if !bind.OverNetwork(from) {
		return compilers.SpaceUnstated
	}
	if space := e.read.spaceOf(e.documentID(from)); space != compilers.SpaceUnstated {
		return space
	}
	return compilers.SpacePublic
}

// admits returns the refusal of the document id, answered a, to a reference
// bounded by reach, or nil where the reference is answered a. A failure is
// the asker's own where no read gave it or its read was bounded no wider. Else
// the document is judged as a target, by the most private space its read can
// have dialled (answer.bound); where that read stated none, a read wider than
// reach is judged again at reach (verdict), and one no wider is refused, as
// the reader did not say.
func (e external) admits(ctx context.Context, id, location string, a answer, reach compilers.AddressSpace) error {
	switch {
	case reach == compilers.SpaceUnstated, !bind.OverNetwork(id):
		return nil
	case a.err != nil && (!a.read || !wider(a.reach, reach)):
		return nil
	case a.bound != compilers.SpaceUnstated:
		return beyond(id, a.bound, reach)
	case !wider(a.reach, reach):
		return unsaid(id)
	}
	return e.read.verdict(id, reach, func() (compilers.AddressSpace, error) {
		return e.verdict(ctx, id, location, reach)
	})
}

// verdict reads the document id at location as an asker bounded by reach
// would, to judge it for that asker alone, and returns the bound the read
// stated, or why the asker is refused: its read's own failure among them.
// What it reads is dropped, as the document's content, space and place in the
// document budget stay its first read's, so the read is not charged; one is
// made per reach a document is judged at (externalReads.verdict).
func (e external) verdict(ctx context.Context, id, location string, reach compilers.AddressSpace) (compilers.AddressSpace, error) {
	got, err := e.opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{
		Location: location, Within: e.opts.Within, Limit: ReadLimit(e.opts.MaxSourceBytes), Reach: reach,
	})
	if err != nil {
		return compilers.SpaceUnstated, readFailure(e.opts, id, err, reach)
	}
	bound := boundOf(got)
	if bound == compilers.SpaceUnstated {
		return bound, unsaid(id)
	}
	return bound, beyond(id, bound, reach)
}

// boundOf is the most private space read can have dialled: its Bound, or its
// Space where that is more private, since an address that answered bounds
// the read however a reader states its bound.
func boundOf(read compilers.DocumentRead) compilers.AddressSpace {
	return max(read.Space, read.Bound)
}

// beyond returns the refusal of the document id, which a read can have
// dialled as far as bound, to a reference bounded by reach, or nil.
func beyond(id string, bound, reach compilers.AddressSpace) error {
	if bound > reach {
		return barred(id, beyondReach(bound, reach))
	}
	return nil
}

// unsaid returns the refusal of the document id, whose reader did not say
// where its read dialled, to a reference bounded by a reach.
func unsaid(id string) error {
	return barred(id, "the reader did not say what address it was read from, "+
		"which a reference in a document read over http or https needs")
}

// beyondReach is why a document at an address in space is refused to a
// reference in a document read from reach.
func beyondReach(space, reach compilers.AddressSpace) string {
	return fmt.Sprintf("it is at a %s address, which a reference in a document read from a %s one does not reach",
		spaceNamed(space), spaceNamed(reach))
}

// spaceNamed names an address space as a refusal does.
func spaceNamed(space compilers.AddressSpace) string {
	switch space {
	case compilers.SpacePublic:
		return "public"
	case compilers.SpacePrivate:
		return "private"
	default:
		return "loopback"
	}
}

// barred is the refusal of the document under key to one asker alone,
// for reason (bind.BarredError).
func barred(key, reason string) error {
	return &bind.BarredError{Why: refused(key, reason).Error()}
}

// fileNamed returns the file the path name names: its dot segments removed, as
// a URI reference's are before anything is read, so one through a directory
// that is not there names the file its cleaned path does, and one through a
// link no other file (GitHub #780). A relative path whose first segment
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

// documentID names the document key, a path or a URL as a request spells it,
// reads: the file it names where it lies (refscope.Places.FileOf), however a
// reference spells it, or the URL itself.
func (e external) documentID(key string) string {
	if file, ok := e.read.self.places.FileOf(key); ok {
		return fileNamed(file)
	}
	return key
}

// fetch reads the document id names at location through Options.Reader,
// confined to Options.Within, held to the byte budget and dialling no
// address more private than reach, and prepares it under key, the spelling
// that asked for it. A refusal names the document by id, which no spelling
// asking first changes. A redirected document's references resolve against
// location while its space is the address that answered, which bounds what
// they reach, so none reaches further (GitHub #1943).
func (e external) fetch(ctx context.Context, id, key, location string, reach compilers.AddressSpace) answer {
	got, err := e.opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{
		Location: location, Within: e.opts.Within, Limit: ReadLimit(e.opts.MaxSourceBytes), Reach: reach,
	})
	if err != nil {
		return answer{err: readFailure(e.opts, id, err, reach), reach: reach, read: true}
	}
	data, err := e.prepare(ctx, id, key, got.Data)
	if err != nil {
		return answer{err: err, space: got.Space, bound: boundOf(got), reach: reach, read: true}
	}
	return answer{body: data, prepared: key, space: got.Space, bound: boundOf(got), reach: reach, read: true}
}

// unnamed returns the refusal of the document under key, read at location,
// unread, or nil: a location a source read over the network may not reach
// (bind.Reachable), as every document it reads is read over it too. A file
// URI a confined read refuses is refused by the reference's spelling
// (refusedByName), since a file is read at its path however it is named.
func (e external) unnamed(key, location string) error {
	if !bind.Reachable(e.read.self.path, location) {
		return refused(key, bind.OffNetwork)
	}
	if _, mapped, err := e.read.self.mapped(location); mapped && err != nil {
		return err
	}
	return nil
}

// followedWhenAllowed ends the reason a confined read refuses a document by
// how a $ref spells it.
const followedWhenAllowed = ", which a read follows only when external references are allowed"

// refusedByName returns why a read refuses a $ref by how its document part is
// written, uri, or nil, judging each $ref alone so that no order of reading
// changes the answer. Confined to opts.Within, it refuses an absolute path and
// a file URI naming any document but the source, named self, since a relative
// $ref reaches the same file however the working directory is spelled (GitHub
// #780, #1886). In either mode it refuses a URL whose scheme is none of http,
// https and file, which a reader would take for a path against the working
// directory. With no reader it is nil.
func refusedByName(opts Options, self string, places *refscope.Places) func(uri string) error {
	if opts.Reader == nil {
		return nil
	}
	confined := opts.Within != ""
	return func(uri string) error {
		if !refscope.IsURL(uri) {
			if confined && filepath.IsAbs(uri) && !namesSource(places, self, uri) {
				return refused(uri, "the $ref names it by an absolute path"+followedWhenAllowed)
			}
			return nil
		}
		// IsURL found a scheme, which is all a URL's text holds before its first ':'.
		scheme, _, _ := strings.Cut(uri, ":")
		switch scheme = strings.ToLower(scheme); scheme {
		case "file":
			if confined && !namesSource(places, self, uri) {
				return refused(uri, "it is named by a file URI"+followedWhenAllowed)
			}
			return nil
		case "http", "https":
			return nil
		default:
			return refused(uri, fmt.Sprintf("it is named by a URL with the scheme %q, "+
				"and only http, https and file URLs are read", scheme))
		}
	}
}

// namesSource reports whether uri, a $ref's document part, names the source
// at self, read as every reference is (resolve.Locate), where the files lie in
// places.
func namesSource(places *refscope.Places, self, uri string) bool {
	named, ok := refscope.Locate(uri, self)
	return ok && places.SameDocument(self, named)
}

// readFailure returns the error a reference into the document under key fails
// with when the reader, asked under opts and reach, refused it: a document past
// the byte budget, outside Options.Within or on the network a confined read
// never reaches is refused with the reason, one past reach, or one whose reader
// cannot judge reach, is refused to that asker alone, and any other failure is
// the reader's own account of it, passed through.
func readFailure(opts Options, key string, err error, reach compilers.AddressSpace) error {
	var past *compilers.AddressSpaceError
	switch {
	case errors.As(err, &past):
		return barred(key, beyondReach(past.Space, reach))
	case errors.Is(err, compilers.ErrAddressSpace):
		// A reader naming no space refused one past reach: the next.
		return barred(key, beyondReach(reach+1, reach))
	case errors.Is(err, compilers.ErrUnboundedRead):
		return barred(key, "the reader cannot bound the addresses its read dials, "+
			"which a reference in a document read over http or https needs")
	case errors.Is(err, compilers.ErrDocumentTooLarge):
		return refused(key, fmt.Sprintf("it is past the %d-byte budget", opts.MaxSourceBytes.Cap()))
	case errors.Is(err, compilers.ErrOutsideWithin) && bind.OverNetwork(key):
		return refused(key, "it is on the network, which a read reaches only when external references are allowed; "+
			"a locate option maps its URL to a location")
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

// prepare records the document the bytes of the document id names read to
// under key, once the source's refusals pass it, readied as the source's is,
// and returns the bytes. It runs once per document read (see
// externalReads.once).
//
// The byte budget is checked again, though the reader held the read to it:
// admitting bytes past it would let a reader lift a budget the compile set. The
// node budget holds the read as it holds the source's. Unparsable or empty
// bytes are refused as such, the parser's reason kept (bind.UnparsableError,
// bind.ErrEmptyDocument).
func (e external) prepare(ctx context.Context, id, key string, data []byte) ([]byte, error) {
	if !e.opts.MaxSourceBytes.Admits(len(data)) {
		return nil, refused(id, fmt.Sprintf("it is past the %d-byte budget", e.opts.MaxSourceBytes.Cap()))
	}

	doc, err := doctree.Read(ctx, data, e.opts.MaxSourceNodes)
	var past *doctree.PastNodesError
	switch {
	case errors.As(err, &past):
		return nil, refused(id, "it "+pastNodesReason(past))
	case err != nil:
		return nil, stopped(ctx, &bind.UnparsableError{Err: err})
	case bind.IsEmpty(doc.Root):
		return nil, bind.ErrEmptyDocument
	}
	if err := e.admit(ctx, id, doc); err != nil {
		return nil, err
	}
	readyForModel(ctx, doc.Root, e.aside)
	e.read.recordTree(key, doc, data)
	// A screen or walk above that a cancel cut short has admitted nothing.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// admit holds an external document, as read, to what Load holds the source to:
// the pre-parse refusals, then the node budget, which the read already held it
// to (prepare).
//
// Any finding refuses it, the scan's report that it could not finish included,
// where the source carries that one forward as a warning. A refused document
// is not read into the compile, so it has no Document.Sources entry a finding
// could name: the reference that named the document is what fails, with the
// findings as its reason.
func (e external) admit(ctx context.Context, key string, doc doctree.Document) error {
	place := nowhere()
	findings := refusals(ctx, place, place.Node(nil), doc.Root, e.opts, false)
	if len(findings) > 0 {
		reasons := make([]string, len(findings))
		for i, d := range findings {
			reasons[i] = d.Message
		}
		return refused(key, strings.Join(reasons, "; "))
	}
	if !e.opts.MaxSourceNodes.Admits(doc.Nodes) {
		return refused(key, "it "+pastNodesReason(&doctree.PastNodesError{Nodes: doc.Nodes, Limit: e.opts.MaxSourceNodes, Whole: true}))
	}
	return nil
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
