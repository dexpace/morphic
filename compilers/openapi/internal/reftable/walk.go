// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// root is a position a walk for reference sites starts from, and the state
// it is read at: the source's root, a position a schema $ref's hop lands on,
// read as a schema, or one a reference object's lands on, read as its kind.
type root struct {
	at    DocumentPointer
	state state
}

// frame is one position a walk has yet to read. In the source's own walk,
// which reads the identifiers its schemas declare, res indexes the resource
// its parent's content is in (identifiers.resources), and dialect is the one
// its parent is read in.
type frame struct {
	node    *yaml.Node
	at      DocumentPointer
	dialect string
	res     int32
	state   state
}

// finder finds the positions a table answers for: the schema sites, the
// discriminator mapping values, the reference objects, and the landings of
// each, walking each root as it is noted, and the identifiers each document's
// schemas declare. It keeps what it has found, and which of that is new since
// the table last settled.
type finder struct {
	docs          *docs
	ids           *identifiers
	work          *work
	sites         map[DocumentPointer]bool
	mappingSites  map[mappingKey]bool
	objects       map[objectKey]bool
	fresh         []DocumentPointer
	freshMappings []mappingKey
	freshObjects  []objectKey
	freshDynamic  []dynamicSite
	// freshRecursive is each $recursiveRef found since the table last settled
	// (dynamicOf).
	freshRecursive []DocumentPointer
	expansions     []expansion
	landings       map[DocumentPointer]bool
	// objLandings is each position a reference object's chain lands on that
	// the grammar reads as no object of its kind (ObjectLanding).
	objLandings map[objectKey]bool
	// tupled is each schema the parser's model cannot hold, by the schema
	// writing it (tupleOf).
	tupled  map[DocumentPointer]DocumentPointer
	pending []root
	covered map[root]bool
	roots   map[DocumentPointer]state
}

// newFinder returns a finder over docs that has found nothing, noting what the
// documents declare in ids.
func newFinder(docsValue *docs, workValue *work, ids *identifiers) *finder {
	return &finder{docs: docsValue, ids: ids, work: workValue, sites: map[DocumentPointer]bool{}, mappingSites: map[mappingKey]bool{},
		objects: map[objectKey]bool{}, landings: map[DocumentPointer]bool{}, objLandings: map[objectKey]bool{},
		tupled: map[DocumentPointer]DocumentPointer{}, covered: map[root]bool{}, roots: map[DocumentPointer]state{}}
}

// walk reads every position below rootValue by the grammar, noting each schema position
// that writes a $ref (a site), each discriminator mapping value that can name a
// position (mapping), and each reference object (objects). A node an alias places at several positions is read at each,
// as the parser reads it. With mark, each position read at a state is noted, so
// a later root inside one already read is not read again; the source's own walk
// from its root notes none, and reads its identifiers the first time it runs.
func (w *finder) walk(rootValue root, mark bool) {
	if prev, seen := w.roots[rootValue.at]; !seen || rootValue.state < prev {
		w.roots[rootValue.at] = rootValue.state
	}
	declaring := !mark && !w.ids.read[rootValue.at.Doc]
	if !declaring {
		// The dialect roots a walk reads are those its document declares.
		w.identify(rootValue.at.Doc)
	}
	w.ids.read[rootValue.at.Doc] = w.ids.read[rootValue.at.Doc] || declaring
	node := w.docs.node(rootValue.at)
	stack := []frame{{node: node, at: rootValue.at, state: rootValue.state, dialect: startDialect(w, rootValue.at, node)}}
	for len(stack) > 0 && w.work.spend(1) {
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := nodeview.Deref(frame.node)
		if node == nil || !modelled(frame.state) {
			continue
		}
		if mark {
			if w.covered[root{frame.at, frame.state}] {
				continue
			}
			w.covered[root{frame.at, frame.state}] = true
		}
		w.visit(node, frame)
		if declaring {
			frame = w.declared(node, frame)
		} else if len(w.ids.dialects) > 0 {
			frame.dialect = rootDialect(w.ids, frame.at, frame.dialect)
		}
		if frame.state == sSchema {
			w.dynamicOf(node, frame)
		}
		w.tupleOf(node, frame)
		stack = w.pushChildren(stack, node, frame)
	}
}

// identify reads what the schemas of document id declare, the first time it is
// asked, whole: OAS 3.1 §4.3.1 has a document parsed complete before a reference
// to it is deemed unresolvable, so what a reference names of it depends on
// nothing read before. It notes no site, as a position of another document
// answers only for what a reference reaches.
func (w *finder) identify(id DocID) {
	if w.ids.read[id] {
		return
	}
	w.ids.read[id] = true
	res := w.ids.document(id, resources{w.docs}.document(id))
	stack := []frame{{node: w.docs.list[id].root, at: DocumentPointer{Doc: id}, state: w.docs.identityState(id), res: res,
		dialect: defaultDialect(w.docs, id)}}
	for len(stack) > 0 && w.work.spend(1) {
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := nodeview.Deref(frame.node)
		if node == nil || !modelled(frame.state) {
			continue
		}
		stack = w.pushChildren(stack, node, w.declared(node, frame))
	}
}

// identifyAll reads what each document opened so far declares, starting past
// those an earlier call read, so a pass that drains many times reads each once.
func (w *finder) identifyAll() {
	for ; w.ids.scanned < DocID(len(w.docs.list)); w.ids.scanned++ {
		if !w.work.spend(1) {
			return
		}
		w.identify(w.ids.scanned)
	}
}

// declared notes the identifiers the schema at frameValue, holding node,
// declares, and returns frameValue as its children read it: in the resource its
// content is in.
func (w *finder) declared(node *yaml.Node, frameValue frame) frame {
	if frameValue.state == sSchema || frameValue.state == sLoose {
		frameValue.res, frameValue.dialect = w.ids.declare(resources{w.docs}, frameValue.res, frameValue.at, node, frameValue.dialect)
	}
	return frameValue
}

// visit notes what the position frameValue, holding node, gives the table.
func (w *finder) visit(node *yaml.Node, frameValue frame) {
	if frameValue.state == sSecurityRequirement {
		w.requirementKeys(node, frameValue)
		return
	}
	if frameValue.state != sSchema {
		w.objectOf(node, frameValue)
		return
	}
	if _, ok := w.docs.refOf(frameValue.at.Doc, node); ok && !w.sites[frameValue.at] {
		w.sites[frameValue.at] = true
		w.fresh = append(w.fresh, frameValue.at)
	}
	w.mappingsOf(node, frameValue.at)
}

// objectOf notes the position frameValue, holding node, as a reference object
// of the kind its state reads, where it writes a $ref: a string one, read as
// the model reads the mapping, a `<<` merging it in included.
func (w *finder) objectOf(node *yaml.Node, frameValue frame) {
	kind, ok := refKinds[frameValue.state]
	if !ok {
		return
	}
	if _, refs := w.docs.refOf(frameValue.at.Doc, node); !refs {
		return
	}
	if objectKeyValue := (objectKey{at: frameValue.at, kind: kind}); !w.objects[objectKeyValue] {
		w.objects[objectKeyValue] = true
		w.freshObjects = append(w.freshObjects, objectKeyValue)
	}
}

// requirementKeys notes each key of the Security Requirement Object node, at
// frameValue, that names no securitySchemes component of the source, as a
// reference to a Security Scheme Object written where the key is, the key its
// URI, where the document holding it reads a key as a URI (Config.KeysByURI).
func (w *finder) requirementKeys(node *yaml.Node, frameValue frame) {
	if node.Kind != yaml.MappingNode || !w.docs.keysByURI(frameValue.at.Doc) {
		return
	}
	for _, pair := range nodeview.Pairs(node) {
		if w.docs.schemes[pair.Key] {
			continue
		}
		at := frameValue.at.below(pair.Key)
		w.docs.keyRefs[at] = pair.Key
		if objectKeyValue := (objectKey{at: at, kind: RefSecurityScheme}); !w.objects[objectKeyValue] {
			w.objects[objectKeyValue] = true
			w.freshObjects = append(w.freshObjects, objectKeyValue)
		}
	}
}

// tupleOf notes each schema the schema at frameValue, holding node and read in
// frameValue's dialect, writes where the parser's model has no member to hold
// one: each entry of an `items` written as a list the dialect reads as a tuple,
// and the `additionalItems` beside it, its rest. The table builds a schema for
// each, as for a landing, held by the schema writing it.
func (w *finder) tupleOf(node *yaml.Node, frameValue frame) {
	schema := frameValue.state == sSchema || frameValue.state == sLoose
	if !schema || !dialectRule(w.docs, frameValue.at.Doc, frameValue.dialect).Tuples || !listsItems(w.docs, frameValue.at.Doc, node) {
		return
	}
	view := w.docs.list[frameValue.at.Doc].view
	items := frameValue.at.below("items")
	for i := range nodeview.Deref(view.ChildByToken(node, "items")).Content {
		w.tupled[items.below(strconv.Itoa(i))] = frameValue.at
	}
	if nodeview.Deref(view.ChildByToken(node, "additionalItems")) != nil {
		w.tupled[frameValue.at.below("additionalItems")] = frameValue.at
	}
}

// dynamicSite is a $dynamicRef: where it is written, its value, and whether a
// $ref is written beside it, which is then the schema's reference.
type dynamicSite struct {
	at     DocumentPointer
	value  string
	beside bool
}

// expansion is a dynamic reference written at from and the schema at to it
// names, which it reaches as a $ref reaches what it names: the one anchor of a
// $dynamicRef's name, or the root of a $recursiveRef's resource.
type expansion struct {
	from, to DocumentPointer
}

// dynamicOf notes the dynamic reference the schema at frameValue, holding node
// and read in frameValue's dialect, writes: a $dynamicRef, which settle reads
// as the $ref it spells or as dynamic (staticSites), and a $recursiveRef of "#"
// where the dialect defines one (DialectRule.Recursive). Nothing but a
// reference builds a schema the model does not hold, another document's or raw
// YAML's (landed), and an expansion is one, so settle lands on what each
// dynamic one names (landDynamic).
func (w *finder) dynamicOf(node *yaml.Node, frameValue frame) {
	view := w.docs.list[frameValue.at.Doc].view
	if ref, ok := dynamicRefValue(w.docs, frameValue.at.Doc, node); ok {
		_, beside := w.docs.refOf(frameValue.at.Doc, node)
		w.freshDynamic = append(w.freshDynamic, dynamicSite{at: frameValue.at, value: ref, beside: beside})
		return
	}
	ref, ok := scalarOf(view.ChildByToken(node, "$recursiveRef"))
	if ok && strings.TrimSpace(ref) == "#" && dialectRule(w.docs, frameValue.at.Doc, frameValue.dialect).Recursive {
		w.freshRecursive = append(w.freshRecursive, frameValue.at)
	}
}

// landOn notes hop, the hop the dynamic reference written at from takes to what
// it names, as a $ref's would be (landed).
func (w *finder) landOn(from DocumentPointer, hop Hop) {
	w.landed(hop)
	w.expansions = append(w.expansions, expansion{from: from, to: hop.Target})
}

// mappingsOf notes each value of the discriminator the schema at at, holding
// node, writes that names no schema component of the source
// (Config.Components): a URI reference, with a fragment or without, as OpenAPI
// reads a value naming no schema. The parser reads none
// (docs/parser-library.md#mapping-unresolved), so each is bound here.
func (w *finder) mappingsOf(node *yaml.Node, at DocumentPointer) {
	disc := DocumentPointer{Doc: at.Doc, Pointer: at.Pointer.AppendToken("discriminator")}
	for _, mappingValue := range mappingValues(w.docs.list[at.Doc].view, node) {
		if w.docs.components[mappingValue.Value] {
			continue
		}
		if mappingKeyValue := (mappingKey{at: disc, value: mappingValue.Value}); !w.mappingSites[mappingKeyValue] {
			w.mappingSites[mappingKeyValue] = true
			w.freshMappings = append(w.freshMappings, mappingKeyValue)
		}
	}
}

// mappingValues returns the scalars the discriminator of the schema node names
// schemas by, as view reads them, aliases followed: each value of its mapping,
// then its defaultMapping. The finder binds them and NamedPointerBytes weighs
// where they land, each by its text whatever its tag, so both ask here.
func mappingValues(view *nodeview.View, node *yaml.Node) []*yaml.Node {
	discriminatorNode := nodeview.Deref(view.ChildByToken(node, "discriminator"))
	if discriminatorNode == nil || discriminatorNode.Kind != yaml.MappingNode {
		return nil
	}
	var values []*yaml.Node
	for _, pair := range view.MappingPairs(nodeview.Deref(view.ChildByToken(discriminatorNode, "mapping"))) {
		values = append(values, pair.Val)
	}
	values = append(values, nodeview.Deref(view.ChildByToken(discriminatorNode, "defaultMapping")))
	return slices.DeleteFunc(values, func(otherNode *yaml.Node) bool { return otherNode == nil || otherNode.Kind != yaml.ScalarNode })
}

// pushChildren returns stack with the positions directly below frameValue,
// whose node is node, appended in order, so a walk reads its children with no
// slice of their own. Nothing beside a reference object's $ref is read but a
// path item's fields (refsOnly): OpenAPI ignores it, so it holds no reference
// and declares no identifier. Every frame a walk reads past its root is pushed
// here, which is where a test counts the pointers the walks spell
// (Config.pushed).
func (w *finder) pushChildren(stack []frame, node *yaml.Node, frameValue frame) []frame {
	if w.refsOnly(node, frameValue) {
		return stack
	}
	from := len(stack)
	switch node.Kind {
	case yaml.MappingNode:
		for _, pair := range w.docs.list[frameValue.at.Doc].view.MappingPairs(node) {
			stack = append(stack, frame{node: pair.Val, at: frameValue.at.below(pair.Key),
				state: stepIn(w.docs, frameValue.at.Doc, node, frameValue.state, pair.Key, frameValue.dialect), res: frameValue.res, dialect: frameValue.dialect})
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			token := strconv.Itoa(i)
			stack = append(stack, frame{node: child, at: frameValue.at.below(token), state: step(frameValue.state, token), res: frameValue.res,
				dialect: frameValue.dialect})
		}
	}
	if note := w.docs.cfg.pushed; note != nil {
		for _, frame := range stack[from:] {
			note(frame.at, node.Kind == yaml.MappingNode)
		}
	}
	return stack
}

// refsOnly reports whether frameValue, holding node, is a reference object
// other than a path item, whose fields beside its $ref OpenAPI gives a meaning
// (docs/compilers/openapi.md#path-item-ref-fields): one whose $ref is all of it
// the specification reads.
func (w *finder) refsOnly(node *yaml.Node, frameValue frame) bool {
	kind, ok := refKinds[frameValue.state]
	if !ok || kind == RefPathItem {
		return false
	}
	_, refs := w.docs.refOf(frameValue.at.Doc, node)
	return refs
}

// below is the position token names under p.
func (p DocumentPointer) below(token string) DocumentPointer {
	return DocumentPointer{Doc: p.Doc, Pointer: p.Pointer.AppendToken(token)}
}

// landed notes the root a resolved hop gives a walk, and, where the model
// holds no schema at its target, a landing the table builds one for.
func (w *finder) landed(hop Hop) {
	w.pending = append(w.pending, root{at: hop.Target, state: sSchema})
	if hop.Role != Schema {
		w.landings[hop.Target] = true
	}
}

// landedObject notes the root a reference object's hop of kind gives a walk,
// its target at, and, where the grammar reads no object of kind there (raw),
// an object landing the load phase builds one for.
func (w *finder) landedObject(at DocumentPointer, kind RefKind, raw bool) {
	w.pending = append(w.pending, root{at: at, state: kind.state()})
	if raw {
		w.objLandings[objectKey{at: at, kind: kind}] = true
	}
}

// walkPending walks each root noted since the last round, in order, skipping
// a source position its own grammar already reads at that state.
func (w *finder) walkPending() bool {
	roots := w.pending
	w.pending = nil
	slices.SortFunc(roots, func(rootValue, otherRoot root) int {
		if c := w.docs.compare(rootValue.at, otherRoot.at); c != 0 {
			return c
		}
		return int(rootValue.state) - int(otherRoot.state)
	})
	walked := false
	for _, root := range slices.Compact(roots) {
		if w.work.cancelled() {
			break
		}
		if root.at.Doc == Source && w.docs.stateAt(root.at) == root.state {
			continue
		}
		w.walk(root, true)
		walked = true
	}
	return walked
}

// mappingKey is a mapping value and the discriminator writing it.
type mappingKey struct {
	at    DocumentPointer
	value string
}

// mapping is where the mapping value mappingKeyValue names, read as a $ref
// written at the discriminator: a hop, and the chain on from where it lands. It
// reports waits where either waits for a document the pass has yet to read
// (waits).
func (b *binder) mapping(mappingKeyValue mappingKey) (Mapping, bool) {
	// A discriminator is a position its document holds, so the path reaches it.
	path, _ := b.docs.path(mappingKeyValue.at)
	mapping := Mapping{At: mappingKeyValue.at, Hop: b.resolve(mappingKeyValue.at, path, mappingKeyValue.value, false)}
	if b.work.over() {
		mapping.Hop = Hop{Ref: mappingKeyValue.value, Status: OverBudget}
	}
	if mapping.Hop.Status != Resolved {
		mapping.Chain = Chain{Status: mapping.Hop.Status}
		if mapping.Hop.Reason == PendingExternalDocumentRead {
			b.wait.heldMappings = append(b.wait.heldMappings, mappingKeyValue)
			return mapping, true
		}
		return mapping, false
	}
	b.find.landed(mapping.Hop)
	if !b.holdsRef(mapping.Hop.Target) {
		mapping.Chain = Chain{Status: Resolved, End: mapping.Hop.Target, Hops: 1}
		return mapping, false
	}
	chain, waits := b.chain(mapping.Hop.Target)
	if chain.Status == Resolved {
		chain.Hops++
	}
	mapping.Chain = chain
	return mapping, waits
}
