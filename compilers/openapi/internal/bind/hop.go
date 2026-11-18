// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// resolve is where ref, written at site, reached along path, lands: in the
// resource its document part names against the resource site is in
// (resources.holding), at the position its fragment names there. schema says
// whether site is read as a schema whatever the grammar reads it as, as a
// $ref's is and a discriminator's mapping value's is not: its own $id then
// counts, and a "#/$defs/..." pointer nothing answers says whether its own
// definitions hold the name.
func (b *binder) resolve(site Position, path []*yaml.Node, ref string, schema bool) Hop {
	h := Hop{Ref: ref, Status: Unresolved}
	uri, frag, _ := strings.Cut(ref, "#")
	if strings.Contains(frag, "#") {
		h.Reason = BadPointer
		return h
	}
	in, known := b.resources.holding(site, path, schema, b.ids.readsDialects(site.Doc))
	if !known {
		h.Reason = MergeBound
		return h
	}
	to, reason, detail := b.named(in, uri)
	if reason != NoReason {
		h.Reason, h.Detail = reason, detail
		return h
	}
	r, reason := b.land(site, path, to, ref, schema, &h)
	if reason != NoReason {
		h.Reason = reason
		return h
	}
	// A schema no model holds is raw YAML, which the table builds as a
	// landing (finder.landed), and which answers only where shaped as one.
	if !answers(r.role(), r.node) {
		h.Reason = NotSchema
		return h
	}
	h.Status, h.Target, h.Role = Resolved, r.at, r.role()
	return h
}

// named is the resource the document part uri of a reference read in the
// resource in names: in itself for none, else the one the URI it names against
// in's identifies: a resource an $id names, in whichever document, the source,
// or the document there, opened. A relative URI read against an $id that no
// schema declares names the document at the URI the $id makes of it (readAt);
// an absolute one names what it names wherever it is written. A location the
// document in is read from may not reach (Reachable) is refused unread.
func (b *binder) named(in resource, uri string) (resource, Reason, string) {
	if uri = strings.TrimSpace(uri); uri == "" {
		return in, NoReason, ""
	}
	key, ok := b.resources.resolveURI(uri, in.uri)
	if !ok {
		return resource{}, Unreadable, "it names no document a reference can"
	}
	if b.resources.names(Source, key) {
		switch {
		case reachable(b.docs, in, b.docs.cfg.Self):
			return b.resources.document(Source), NoReason, ""
		case key != b.docs.list[Source].self:
			return resource{}, Unreadable, OffNetwork
		}
		// Named by its $self alone, the source answers no document that could
		// not read it: the URI is a location to this one (admitted).
	}
	// A miss the merge bound may explain is read as a location all the same:
	// the table says it is truncated.
	root, found, why, waitOn := b.ids.lookupURI(key, func(decl DocID) bool {
		return admitted(b.docs, decl, in.root.Doc, key) && b.reads.admits(b.docs, decl, in.root.Doc)
	}, !b.wait.unwaited)
	switch {
	case found:
		b.ids.keep(key)
		b.docs.reach(root.Doc)
		return resource{root: root, uri: key, id: true}, NoReason, ""
	case why == DuplicateID:
		b.ids.keep(key)
		return resource{}, why, key
	case len(waitOn) > 0:
		// A document read and not yet reached declares it: the hop waits for
		// that document as a held one waits for its read (goOn).
		b.wait.declaring = append(b.wait.declaring, waitOn...)
		return resource{}, Held, ""
	}
	located, reason, detail := locatedAt(b, in, uri, key)
	if reason != Held {
		b.ids.keep(key)
	}
	return located, reason, detail
}

// locatedAt is the resource of the document uri, written in the resource in,
// names as a location, key being the URI it resolves to: read against an
// $id's or a $self's base where only those name it (readAt), else where it
// is written (opened).
func locatedAt(b *binder, in resource, uri, key string) (resource, Reason, string) {
	if (in.id || in.based) && !fragment.Absolute(uri) {
		return b.readAt(in, uri, key)
	}
	return b.opened(in, uri, key)
}

// opened is the resource of the document uri, written in the resource in,
// names, placed by Config.Locate, or why not: key, the URI it resolves to, is
// no location in's document may reach, or Config.Refuse refuses that spelling,
// each judged before Config.Locate asks anything of a reader, it names no
// document, or the read fails (read). An identifier is never refused, since no
// document is read for one.
func (b *binder) opened(in resource, uri, key string) (resource, Reason, string) {
	if !reachable(b.docs, in, key) {
		return resource{}, Unreadable, OffNetwork
	}
	if why := b.docs.refusal(uri); why != "" {
		return resource{}, Unreadable, why
	}
	name, ok := opening(b.docs, uri, in.uri)
	if !ok {
		return resource{}, Unreadable, "it names no document a reference can"
	}
	// Where the URL is read from, a location the caller mapped it to among
	// them, is judged too: a mapping never lands a fetched document on a file.
	if !reachable(b.docs, in, name) {
		return resource{}, Unreadable, OffNetwork
	}
	return b.read(name, in)
}

// readAt is the resource of the document a relative URI uri names, written in
// the resource in, whose base an $id or a $self gives: key, the URI that base
// makes of it by RFC 3986, normalized (NormalURI). Config.Refuse judges uri as
// written where only relative paths moved the base, and key once an absolute
// one detached it, so a base cannot make a refused read pass as relative. A
// key that is a URL is located as one and opened where a caller's mapping puts
// it (opening), and where it lands is judged too.
func (b *binder) readAt(in resource, uri, key string) (resource, Reason, string) {
	spelled := uri
	if in.detached {
		spelled = key
	}
	if !reachable(b.docs, in, key) {
		return resource{}, Unreadable, OffNetwork
	}
	if why := b.docs.refusal(spelled); why != "" {
		return resource{}, Unreadable, why
	}
	name := key
	if u, err := url.Parse(key); err == nil && len(u.Scheme) > 1 {
		if located, ok := opening(b.docs, key, key); ok {
			name = located
		}
	}
	if !reachable(b.docs, in, name) {
		return resource{}, Unreadable, OffNetwork
	}
	return b.read(name, in)
}

// OffNetwork is why a reference written in a document read over the network
// is not read, where it names a document by no http or https URL (Reachable).
const OffNetwork = "a reference in a document read over http or https reads only http and https URLs"

// reachable reports whether a reference written in the resource in, of one of
// d's documents, may read the document named name: judged by the location
// in's document was read from, never by a URI an $id gives in, so that no $id
// moves a reference in a document read over the network onto a file.
func reachable(d *docs, in resource, name string) bool {
	return Reachable(d.name(in.root.Doc), name)
}

// Reachable reports whether a reference written in the document read from
// from may read the document at target. One read over http or https reads
// only another so read, as its author owns no file of the reader's: a path, a
// file URI, a drive's path or a URL of any other scheme is read nowhere. Any
// other document may read any. It is the one judgement every route reading a
// reference makes, before the document is read.
func Reachable(from, target string) bool {
	return !OverNetwork(from) || OverNetwork(target)
}

// OverNetwork reports whether location names a document read over the
// network: an http or https URL, its scheme in any case. A file URI, a path
// and a URL of any other scheme are not.
func OverNetwork(location string) bool {
	u, err := url.Parse(location)
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

// read is the resource of the document named name, read through Config.Open
// as a reference written in the resource in reads it, which the hop then
// reaches, or why the read failed. The first read of a document waits until
// the pass's walks are done (Held), since a document read meanwhile may
// declare its name, and an identifier is not a location to read (goOn).
func (b *binder) read(name string, in resource) (resource, Reason, string) {
	from := b.docs.name(in.root.Doc)
	if b.reads.unread(b.docs, name, from) {
		b.reads.hold(name, in.root.Doc)
		return resource{}, Held, ""
	}
	id, err := b.reads.open(b.docs, name, from)
	var unparsable *UnparsableError
	switch {
	case errors.Is(err, errNotRead):
		return resource{}, ExternalOff, ""
	case errors.As(err, &unparsable):
		return resource{}, Unparsable, unparsable.Err.Error()
	case errors.Is(err, ErrEmptyDocument):
		return resource{}, Empty, ""
	case err != nil:
		return resource{}, Unreadable, err.Error()
	}
	b.docs.reach(id)
	return b.resources.document(id), NoReason, ""
}

// land reads where the fragment of ref, written at site and reached along
// path, names in the resource to: a JSON pointer from its root, a
// "#/$defs/..." one in a document's own resource by the $defs rule
// (definition), or the schema a plain name names (anchored). It notes in h
// the URI of a resource it names nothing in.
func (b *binder) land(site Position, path []*yaml.Node, to resource, ref string, schema bool, h *Hop,
) (reading, Reason) {
	pointer := fragment.Pointer(ref)
	switch {
	case pointer != "" && !strings.HasPrefix(string(pointer), "/"):
		return b.anchored(to, string(pointer))
	case !pointer.IsValid() || !utf8.ValidString(string(pointer)):
		return reading{}, BadPointer
	}
	if uri, _, _ := strings.Cut(ref, "#"); strings.TrimSpace(uri) == "" && !to.id && isDefs(pointer) {
		r, reason := b.readPointer(site.Doc, pointer)
		return b.defs.definition(site, path, pointer, schema, r, reason)
	}
	r, reason := b.readPointer(to.root.Doc, to.root.Pointer+pointer)
	if reason == NoTarget && to.id {
		reason, h.Detail = NoResourceTarget, to.uri
	}
	return r, reason
}

// anchored reads the schema the resource to declares the plain name name in, as
// its $anchor, its $dynamicAnchor or an identifier naming that fragment alone,
// whichever document holds it: any fragment no JSON pointer spells is a plain
// name, as every JSON Schema dialect reads one (2020-12 §5). A name no grammar
// admits, which only draft 4's `id` can declare, is BadPointer where no schema
// declares it and none past a merge bound may: no other dialect could, so it
// is likelier a pointer written wrong.
func (b *binder) anchored(to resource, name string) (reading, Reason) {
	b.find.identify(to.root.Doc)
	at, why := b.ids.lookupAnchor(to.root, name)
	switch {
	case why == NoReason:
		return b.readPointer(to.root.Doc, at.Pointer)
	case why != NoAnchor || oas.ValidFormat(oas.AnchorName, name) || oas.ValidFormat(oas.PlainName, name):
		return reading{}, why
	default:
		return reading{}, BadPointer
	}
}

// hop is the hop of the $ref written at p, read once and remembered. Only a
// position writing a $ref is asked, a site or a hop's target that writes one,
// or a $dynamicRef that is the $ref it spells (holdsRef).
func (b *binder) hop(p Position) Hop {
	if h, ok := b.hops[p]; ok {
		return h
	}
	// Only a position its document holds writes a $ref, so the path reaches it.
	path, _ := b.docs.path(p)
	ref, written := b.docs.refOf(p.Doc, path[len(path)-1])
	if !written {
		ref, _ = dynamicRefValue(b.docs, p.Doc, path[len(path)-1])
		b.dyn.spelled[p] = true
	}
	h := b.resolve(p, path, ref, true)
	if b.work.over() {
		h = Hop{Ref: ref, Status: OverBudget}
	}
	b.hops[p] = h
	if h.Status == Resolved {
		b.find.landed(h)
	}
	if h.Reason == Held {
		b.wait.held = append(b.wait.held, p)
	}
	return h
}

// DynamicFragment returns the anchor name a $dynamicRef written as ref
// addresses, or the reason it addresses none: its fragment, after any URI
// part, unless that is empty or holds a '/', as a JSON pointer does. A
// $dynamicRef the table reads as dynamic names a $dynamicAnchor by a plain
// name, so only one written past what the table read meets the refusal.
//
// The name is the fragment percent-decoded once (RFC 3986): `#my%2Danchor` and
// `#my-anchor` name one anchor. PathUnescape keeps `+` literal, as a fragment
// needs, and a fragment it cannot decode is refused, not matched raw.
func DynamicFragment(ref string) (name, why string, ok bool) {
	_, spelled, found := strings.Cut(ref, "#")
	if !found || spelled == "" || strings.Contains(spelled, "/") {
		return "", fmt.Sprintf("%q names no anchor by a plain-name fragment", ref), false
	}
	name, err := url.PathUnescape(spelled)
	if err != nil {
		return "", fmt.Sprintf("%q is not valid percent-encoded text: %s", ref, err.Error()), false
	}
	return name, "", true
}

// anchorNamed reports whether s is a name the anchor keyword can declare in a
// schema read as rule says: by the plain-name grammar for `$anchor` where the
// dialect gives it that grammar (DialectRule.PlainNames), as 2019-09 does; by
// 2020-12's for any other, as for `$dynamicAnchor`, which 2020-12 added, and
// for an `$anchor` in a draft defining none, read as 2020-12 reads it. An
// identifier's name is fragmentName's.
func anchorNamed(rule DialectRule, keyword, s string) bool {
	if keyword == "$anchor" && rule.PlainNames {
		return oas.ValidFormat(oas.PlainName, s)
	}
	return oas.ValidFormat(oas.AnchorName, s)
}
