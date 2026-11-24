// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/yml"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// unreachableKeyDiag reports a key the census named whose value the mapping,
// read as the parser reads it (nodeview.Child), does not present, so nothing of
// it reached the IR.
//
// The document does write the key: the parser reads every `<<` merge however
// deep, while the compiler reads none past nodeview.MergeDepthLimit, so a key
// merged in from past that bound is reported here.
//
// A warning, not the error unpreservableDiag gives: merging keys is legal input
// that still lowers, and an error would refuse it under the default --fail-on.
func unreachableKeyDiag(entry string, at ir.Provenance) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyUnreachable, at,
		"%s is written at a key merged in through `<<` from deeper than the %d levels the compiler "+
			"reads; it is represented in the IR in no form at all", entry, nodeview.MergeDepthLimit)
}

// occupiedEntryDiag reports a key whose Unmodeled entry is already held by a
// construct written somewhere else, so this one reached the IR in no form.
//
// This happens on carriers holding more than one object's entries: an
// ir.Parameter's map carries the parameter's own keys and everything its schema
// had no home for, both unscoped, so a key written by both spells one entry.
// Which survives is decided by lowering order, not by the document. This census
// cannot settle that alone, because moving either side moves keys other
// mechanisms already publish (GitHub #396).
func occupiedEntryDiag(entry string, at ir.Provenance, held jsontext.Pointer) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyEntryAlreadyClaimed, at,
		"%s is already held by the construct at %q, so this key is represented in the IR in "+
			"no form at all", entry, held)
}

// MaxUnknownKeys bounds how many keys one object contributes to the IR. It sits
// far above what a document writes by accident, so an object reaching it is
// generated or hostile. Keys past it are announced under diag.UnknownKeyBudget.
//
// It bounds the keys the census answers for, not the keys written: one another
// reader already kept is filtered out first, since a slot spent on an entry
// present either way would drop one that is not. An unreachable key still
// spends its slot: finding that out costs the lookup keeping it does.
// Diagnostics are one per key plus the budget's own.
const MaxUnknownKeys = 64

// decidedKeywords are the JSON Schema keywords the schema model names no field
// for (docs/parser-library.md#unmodeled-keywords) and this compiler has already
// decided about, so the census must not claim them as unread. The schema walk's
// 2020-12 vocabulary test fails if one starts being carried:
//
//   - $comment: 2020-12 §8.3 forbids presenting it to end users, so it is
//     dropped on purpose.
//   - $dynamicAnchor: read by internal/reftable as a reference target, so a
//     $dynamicRef can expand.
//   - $dynamicRef: carried by the dynamic-reference lowering, which expands it
//     or keeps it under its own reason. A census entry beside an expanded one
//     would say a resolved reference was ignored.
var decidedKeywords = [...]string{"$comment", "$dynamicAnchor", "$dynamicRef"}

// DecidedKeyword reports whether keyword is one the census leaves alone because
// this compiler has already decided about it (decidedKeywords).
func DecidedKeyword(keyword string) bool { return slices.Contains(decidedKeywords[:], keyword) }

// modellessKeywords are the keywords of the dialect the schema model names no
// field for that a reader keeps, so the census meets them with the rest.
var modellessKeywords = []string{"$vocabulary", "dependentRequired"}

// UnknownKeywordsIn records on unmodeled the keywords schema writes that no
// field of the JSON Schema model names, and announces each: legal JSON Schema
// 2020-12 an implementation must ignore, so a decision rather than a fault. It
// also returns the source provenances of any keywords that could not be
// preserved as JSON. It runs after every other reader and keeps what none kept:
// `$vocabulary` and `dependentRequired` have readers of their own, and decided
// or caller-taken keywords are skipped.
func UnknownKeywordsIn(unmodeled *ir.Unmodeled, schema *statement.Schema, pointer jsontext.Pointer, doc provenance.Doc, taken ...string) ([]ir.Provenance, []ir.Diagnostic) {
	keys, root := unknownSchemaKeys(schema)
	keys = slices.DeleteFunc(keys, func(key string) bool { return slices.Contains(taken, key) })
	return census(unmodeled, keys, root, doc, pointer, "", keyClass{
		code:     diag.UnknownSchemaKeyword,
		severity: ir.SeverityInfo,
		message: "keyword %q has no field in the schema model this compiler lowers and no IR " +
			"position of its own; kept verbatim under Unmodeled",
	})
}

// WritesUnknownKeyword reports whether schema writes a keyword
// UnknownKeywordsIn would consider keeping, so a position can tell whether it
// needs a node of its own for one to land on.
func WritesUnknownKeyword(schema *statement.Schema) bool {
	keys, _ := unknownSchemaKeys(schema)
	return len(keys) > 0
}

// UnknownKeyword reports whether key, a key schema writes, is one
// UnknownKeywordsIn would consider keeping, for a reader telling one keyword's
// slot.
func UnknownKeyword(schema *statement.Schema, key string) bool {
	keys, _ := unknownSchemaKeys(schema)
	return slices.Contains(keys, key)
}

// WritesEmptyDependencyMap reports whether schema writes key,
// `dependentSchemas`, `dependentRequired` or a draft's `dependencies`, as a
// mapping with no entry: JSON Schema equates it with the keyword left out, so
// nothing of it is kept (docs/compilers/openapi.md#validation-only). The
// drafts' spelling is one only where the dialect schema is read in defines it,
// and elsewhere an unknown keyword.
func WritesEmptyDependencyMap(schema *statement.Schema, key string) bool {
	switch key {
	case "dependentSchemas", "dependentRequired":
	case "dependencies":
		if statementKey, _ := schema.Key(key); !statementKey.Defined() {
			return false
		}
	default:
		return false
	}
	node := keyNode(schema, key)
	return node != nil && node.Kind == yaml.MappingNode && len(node.Content) == 0
}

// WritesKeywordLeftOut reports whether schema writes key as a null that reads
// as the keyword left out (statement.Schema.Key), or key as an empty dependency
// map (WritesEmptyDependencyMap), for a reader that lists the keys schema
// writes rather than asking for one. A null is a value in an extension, and in
// a key the census keeps.
func WritesKeywordLeftOut(schema *statement.Schema, key string) bool {
	if WritesEmptyDependencyMap(schema, key) {
		return true
	}
	statementKey, read := schema.Key(key)
	if !statementKey.Null || read || strings.HasPrefix(key, "x-") {
		return false
	}
	keys, _ := unknownSchemaKeys(schema)
	return !slices.Contains(keys, key)
}

// unknownSchemaKeys returns the keys schema writes that UnknownKeywordsIn
// answers for, and the mapping holding them: those the model names no field
// for, but the ones decided about (DecidedKeyword), a null for a keyword the
// dialect defines, which reads as left out (statement.Schema.Key), and an empty
// dependency map (WritesEmptyDependencyMap). A key of no vocabulary has no type
// a null could break, so it is kept with the null it wrote. The keys are the
// model's census (statement.Schema.Unknown), which names a key merged past
// nodeview.MergeDepthLimit too, so the census reports it unreachable.
func unknownSchemaKeys(schema *statement.Schema) ([]string, *yaml.Node) {
	if schema == nil {
		return nil, nil
	}
	return slices.DeleteFunc(slices.Clone(schema.Unknown), func(key string) bool {
		statementKey, _ := schema.Key(key)
		return DecidedKeyword(key) || slices.Contains(modellessKeywords, key) && statementKey.Null ||
			WritesEmptyDependencyMap(schema, key)
	}), schema.Node
}

// UndefinedKeywordsIn records on unmodeled each of keys, keywords schema writes
// that the schema model names but the dialect schema is read in does not define
// and nothing reads, and announces each as UnknownKeywordsIn announces a
// keyword the model does not name: JSON Schema 2020-12 ignores the two alike.
// The caller says which keywords those are, since only it knows the dialect and
// the policies.
func UndefinedKeywordsIn(unmodeled *ir.Unmodeled, schema *statement.Schema, keys []string, pointer jsontext.Pointer, doc provenance.Doc) []ir.Diagnostic {
	if len(keys) == 0 {
		return nil
	}
	_, diags := census(unmodeled, keys, schema.Node, doc, pointer, "", keyClass{
		code:     diag.UnknownSchemaKeyword,
		severity: ir.SeverityInfo,
		message: "keyword %q is not defined by the dialect this schema is read in, and no policy reads it " +
			"here; kept verbatim under Unmodeled",
	})
	return diags
}

// UnknownKeysIn records on unmodeled the keys an OpenAPI object writes that the
// specification neither defines nor admits as an extension, for an object
// lowering to a node with an Unmodeled map of its own. owner is the object's
// source pointer.
//
// It keeps each key and reports none: the rule table reports every such key of
// every object it judges, lowered or not (docs/compilers/openapi.md#unknown-keys),
// so a report here would be the key's second. What it reports is about the
// entry: one taken, unreachable or past the budget.
func UnknownKeysIn(unmodeled *ir.Unmodeled, model any, doc provenance.Doc, owner jsontext.Pointer, taken ...string) []ir.Diagnostic {
	return UnknownKeysUnder(unmodeled, model, doc, owner, "", taken...)
}

// UnknownKeysUnder is UnknownKeysIn with every entry keyed beneath scope, for
// the objects with no Unmodeled map of their own, whose keys ride on the nearest
// node that has one — an info object's on the document, a tag's on the document.
//
// scope says which object wrote them: the source path from the carrier down to
// the object. Several objects reach one map, where "openapi:status" from two of
// them would be a single key and the entry that survived would depend on which
// lowering ran last. taken lists keys a reader placed in a field (TakeString).
func UnknownKeysUnder(unmodeled *ir.Unmodeled, model any, doc provenance.Doc, owner jsontext.Pointer, scope string, taken ...string) []ir.Diagnostic {
	keys, root := undeclaredKeys(model)
	return UnknownKeysNamed(unmodeled, keys, root, doc, owner, scope, taken...)
}

// UnknownKeysNamed is UnknownKeysUnder for an object whose model keeps no
// census of its own, so the caller names the keys and hands over the mapping
// node they were written on.
//
// One object needs it: a Path Item Object, whose core model embeds the map of
// its operations. The unmarshaller folds an unrecognized key into that map
// rather than recording it as undeclared, so the object's census is empty
// however much the document wrote (docs/parser-library.md#embedded-map-keys).
// Its leftovers are graded as any undeclared key is, because UnknownKeysUnder
// delegates here.
func UnknownKeysNamed(unmodeled *ir.Unmodeled, keys []string, root *yaml.Node,
	doc provenance.Doc, owner jsontext.Pointer, scope string, taken ...string,
) []ir.Diagnostic {
	_, diags := census(unmodeled, keys, root, doc, owner, scope, keyClass{skip: taken})
	return diags
}

// TakeString reads a string field the parser's model names no member for,
// written directly under key on root, the mapping the census reads (GitHub
// #615). It reports whether it read the key, for the caller to pass to the
// census as taken.
//
// A scalar the parser decodes as a string is returned, unless valid refuses a
// non-empty one; empty or null is read as unset. Any other value is kept
// verbatim on unmodeled at the census's own entry, with a warning. root is read
// as the parser reads it, through its `<<` merges (nodeview.Child).
func TakeString(unmodeled *ir.Unmodeled, root *yaml.Node, key string, valid func(string) bool,
	doc provenance.Doc, owner jsontext.Pointer, scope string,
) (string, bool, []ir.Diagnostic) {
	node := nodeview.Child(root, key)
	if node == nil {
		return "", false, nil
	}
	value, ok := stringValue(node)
	if ok && (value == "" || valid == nil || valid(value)) {
		return value, true, nil
	}
	at := doc.At(owner + ids.Ptr(key))
	kept, diags := PreserveNodeInto(unmodeled, EntryKey(scope, key), node, ir.ReasonDegradedLowering, at)
	if !kept {
		return "", true, diags
	}
	why := "is not a string"
	if ok {
		why = fmt.Sprintf("value %q is not one the specification defines", value)
	}
	return "", true, append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, at,
		"%s %s, so it reaches no IR field; kept verbatim under Unmodeled", key, why))
}

// stringValue decodes node as the parser decodes a string field it models: a
// scalar of a tag the parser accepts there, an alias followed first, with null
// read as the empty string. ok is false for any other node, which the parser
// would report as a type mismatch.
func stringValue(node *yaml.Node) (string, bool) {
	dereferenced := yml.ResolveAlias(node)
	if dereferenced == nil || dereferenced.Kind != yaml.ScalarNode {
		return "", false
	}
	if dereferenced.Tag == "!!null" {
		return "", true
	}
	return dereferenced.Value, slices.Contains(yml.TypeToYamlTags(reflect.TypeFor[string]()), dereferenced.Tag)
}

// keyClass is how a key the model does not name is graded: which diagnostic
// announces it, and at what severity.
//
// The reason is not part of it. Both classes carry ReasonOutOfScope, a property
// of the construct rather than of the document: no IR node is coming for a key
// the format does not define, nor for one a schema dialect defines and this
// compiler does not model, so only an emitter policy layer consumes either.
// Which of the two a key is says something about the source, which the
// diagnostic channel reports.
type keyClass struct {
	code     string // empty for a class the rule table reports, which keeps and announces nothing
	severity ir.Severity
	skip     []string // keys another reader answers for
	message  string   // one %q, filled with the key
}

// census records on unmodeled every key in keys, read off the mapping node root
// they were written on, each under its own key beneath scope.
//
// It sorts a copy: neither source of keys yields document order, the model's
// census following the goroutines that decoded it
// (docs/parser-library.md#goroutine-per-node), and sorting in place would
// reorder the slice under its owner (invariant 7).
//
// A key unmodeled already holds for this very construct is left alone and
// unannounced, since the reader that recorded it said it better (see
// unrecorded). That happens before the bound applies (see MaxUnknownKeys).
func census(unmodeled *ir.Unmodeled, keys []string, root *yaml.Node,
	doc provenance.Doc, owner jsontext.Pointer, scope string, cl keyClass,
) ([]ir.Provenance, []ir.Diagnostic) {
	if len(keys) == 0 {
		return nil, nil // the common case: most objects write no key their model misses
	}
	fresh := unrecorded(unmodeled, slices.Sorted(slices.Values(keys)), owner, scope, cl.skip)
	if len(fresh) == 0 {
		return nil, nil
	}
	var unpreserved []ir.Provenance
	var diags []ir.Diagnostic
	if len(fresh) > MaxUnknownKeys {
		diags = append(diags, budgetDiag(len(fresh), doc.At(owner)))
		fresh = fresh[:MaxUnknownKeys]
	}
	for _, key := range fresh {
		keyDiags, failed := keep(unmodeled, root, key, doc, owner, scope, cl)
		if failed {
			unpreserved = append(unpreserved, doc.At(owner+ids.Ptr(key)))
		}
		diags = append(diags, keyDiags...)
	}
	return unpreserved, diags
}

// keep writes one key's value under its entry and announces it, or says why it
// could not, reporting whether raw conversion failed.
func keep(unmodeled *ir.Unmodeled, root *yaml.Node, key string, doc provenance.Doc, owner jsontext.Pointer, scope string, cl keyClass) ([]ir.Diagnostic, bool) {
	entry, at := EntryKey(scope, key), doc.At(owner+ids.Ptr(key))
	if taken, occupied := (*unmodeled)[entry]; occupied {
		return []ir.Diagnostic{occupiedEntryDiag(entry, at, taken.Provenance.Pointer)}, false
	}
	node := nodeview.Child(root, key)
	if node == nil {
		return []ir.Diagnostic{unreachableKeyDiag(entry, at)}, false
	}
	kept, diags := PreserveNodeInto(unmodeled, entry, node, ir.ReasonOutOfScope, at)
	if !kept {
		return diags, len(diags) > 0
	}
	if cl.code == "" {
		return diags, false
	}
	return append(diags, diag.Newf(cl.severity, cl.code, at, cl.message, key)), false
}

// unrecorded returns the keys this census has to answer for: those not in skip,
// less those a reader already recorded for the very construct.
//
// Sameness is the entry's provenance, not its presence. A reader with more to
// say about a keyword writes it at the pointer the census would use
// (`$vocabulary` and `dependentRequired` on a schema's own map), so the census
// has nothing to add. An entry pointing elsewhere is a different construct
// spelling the same key, a collision that keep reports.
func unrecorded(unmodeled *ir.Unmodeled, keys []string, owner jsontext.Pointer, scope string, skip []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if slices.Contains(skip, key) {
			continue
		}
		if unmodeledEntry, recorded := (*unmodeled)[EntryKey(scope, key)]; recorded &&
			unmodeledEntry.Provenance.Pointer == owner+ids.Ptr(key) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// EntryKey is the Unmodeled key the census keeps key under, written on an object
// at scope beneath its carrier. A reader that keeps a key itself spells it with
// this, so the census finds the entry and leaves the key alone.
func EntryKey(scope, key string) string {
	return entryPrefix + scoped(scope, key)
}

// scoped spells one entry's key on the carrier holding it.
//
// The key is escaped as one segment while scope is a path of literals already
// spelled that way, so a key holding a "/" cannot read as a scope of its own: a
// root key spelled "info/contact/slack" would otherwise be the very entry the
// contact object's own "slack" keys under, and the second site to reach the
// carrier would drop its key without a word. ids.Scope records the rule.
func scoped(scope, key string) string {
	if scope == "" {
		return ids.Scope(key)
	}
	return scope + "/" + ids.Scope(key)
}

// budgetDiag reports the keys past MaxUnknownKeys, which reach the IR in no form.
func budgetDiag(total int, owner ir.Provenance) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyBudget, owner,
		"object writes %d keys its model names no field for and no other reader kept, past the "+
			"%d this compiler keeps; the rest are represented in the IR in no form at all",
		total, MaxUnknownKeys)
}

// parsedObject is the part of a parsed model the census reads: its core, which
// holds the census the unmarshaller took, and the mapping node the keys were
// written on.
//
// Declared here rather than taken from the library, so this package depends on
// the shape it uses rather than on the marshaller package, and so a test can
// drive the branches below with a model of its own.
type parsedObject interface {
	GetCoreAny() any
	GetRootNode() *yaml.Node
}

// unknownReporter is a core model's own record of the keys it did not name.
type unknownReporter interface{ GetUnknownProperties() []string }

// undeclaredKeys returns the keys model's source object wrote that its model
// names no field for, with the mapping node they were written on, in the
// library's order (census sorts them). A model with no census, or a typed nil,
// yields nothing.
//
// An empty census does not prove nothing is undeclared: a core model shaped as
// a sequenced map folds an unrecognized key into that map and reports nothing
// (docs/parser-library.md#embedded-map-keys).
// That is right for Paths, Responses and Callback, whose every key is a
// legitimate entry. A Path Item's keys go to UnknownKeysNamed instead, and
// another such object would go quiet here.
func undeclaredKeys(model any) ([]string, *yaml.Node) {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, nil
	}
	obj, ok := model.(parsedObject)
	if !ok {
		return nil, nil
	}
	core, ok := obj.GetCoreAny().(unknownReporter)
	if !ok {
		return nil, nil
	}
	return core.GetUnknownProperties(), obj.GetRootNode()
}
