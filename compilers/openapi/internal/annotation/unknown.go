package annotation

import (
	"encoding/json/jsontext"
	"reflect"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// unreachableKeyDiag reports a key the census named whose value the raw mapping
// does not present, so nothing of it reached the IR.
//
// The document does write the key: the parser reads a mapping through its `<<`
// merge keys, so a key merged in from an anchored mapping is reported here
// while the mapping this reads holds no pair for it. Resolving that needs the
// merge-expanded view (internal/nodeview), which no raw-node reader here has
// (GitHub #395).
//
// A warning, not the error UnpreservableDiag gives: merging keys is legal input
// that still lowers, and an error would refuse it under the default --fail-on.
func unreachableKeyDiag(entry string, at ir.Provenance) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyUnreachable, at,
		"%s is written at a key the source mapping does not present directly, most likely "+
			"merged in through a `<<`; it is represented in the IR in no form at all", entry)
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
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyEntryTaken, at,
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

// DecidedKeywords are the JSON Schema keywords the schema model names no field
// for and this compiler has already decided about, so the census must not claim
// them as unread. The schema walk's 2020-12 vocabulary test fails if one starts
// being carried:
//
//   - $comment: 2020-12 §8.3 forbids presenting it to end users, so it is
//     dropped on purpose.
//   - $dynamicAnchor: read by the anchor index as a reference target, so a
//     $dynamicRef can expand.
//   - $dynamicRef: carried by the dynamic-reference lowering, which expands it
//     or keeps it under its own reason. A census entry beside an expanded one
//     would say a resolved reference was ignored.
var DecidedKeywords = []string{"$comment", "$dynamicAnchor", "$dynamicRef"}

// UnknownKeywordsIn records on p the keywords s writes that no field of the
// JSON Schema model names, and announces each.
//
// OpenAPI 3.1 schemas are JSON Schema 2020-12, where an unrecognized keyword is
// legal input that an implementation must ignore. So this reports a decision,
// not a fault (see diag.UnknownSchemaKeyword for the grading).
//
// It keeps only what no other reader kept, so it runs after all of them:
// `$vocabulary` and `dependentRequired` have no model field and are read off
// the raw node by readers with more to say. A keyword no reader leaves a trace
// of needs naming in DecidedKeywords.
func UnknownKeywordsIn(p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer, locate Locator) []ir.Diagnostic {
	keys, root := undeclaredKeys(s)
	return census(p, keys, root, locate, pointer, "", keyClass{
		code:     diag.UnknownSchemaKeyword,
		severity: ir.SeverityInfo,
		skip:     DecidedKeywords,
		message: "keyword %q has no field in the schema model this compiler lowers and no IR " +
			"position of its own; kept verbatim under Unmodeled",
	})
}

// UnknownKeysIn records on p the keys an OpenAPI object writes that the
// specification neither defines nor admits as an extension, for an object
// lowering to a node with an Unmodeled map of its own. owner is the object's
// source pointer.
//
// Unlike its schema neighbour this reports a fault: OpenAPI gives each object a
// closed key set and requires extensions to be prefixed x-, so such a key is
// usually a misspelling. It is kept all the same, because the IR is lossless
// even for invalid input, and a misspelt key is the one most worth finding.
func UnknownKeysIn(p *ir.Unmodeled, model any, locate Locator, owner jsontext.Pointer) []ir.Diagnostic {
	return UnknownKeysUnder(p, model, locate, owner, "")
}

// UnknownKeysUnder is UnknownKeysIn with every entry keyed beneath scope, for
// the objects with no Unmodeled map of their own, whose keys ride on the nearest
// node that has one — an info object's on the document, a tag's on the document.
//
// scope says which object wrote them: the source path from the carrier down to
// the object. Several objects reach one map, where "openapi:status" from two of
// them would be a single key and the entry that survived would depend on which
// lowering ran last.
func UnknownKeysUnder(p *ir.Unmodeled, model any, locate Locator, owner jsontext.Pointer, scope string) []ir.Diagnostic {
	return UnknownKeysDecided(p, model, locate, owner, scope, nil)
}

// UnknownKeysDecided is UnknownKeysUnder for an object one of whose keys a
// reader has already read raw: `decided` names those keys, and the census leaves
// them alone rather than reporting a key the document does define as undefined.
//
// The parser's model has no field for the keys OpenAPI 3.2 added, such as a
// Response Object's `summary`, so each is read off the raw node (GitHub #615).
// `decided` must be empty below 3.2, where the same key is a misspelling and
// the warning is owed.
func UnknownKeysDecided(p *ir.Unmodeled, model any, locate Locator, owner jsontext.Pointer, scope string, decided []string) []ir.Diagnostic {
	keys, root := undeclaredKeys(model)
	return census(p, keys, root, locate, owner, scope, objectKeyClass(decided))
}

// objectKeyClass grades a key the OpenAPI object it is written on does not
// define, with the keys a reader has already taken raw left out.
func objectKeyClass(decided []string) keyClass {
	return keyClass{
		code:     diag.UnknownObjectKey,
		severity: ir.SeverityWarning,
		skip:     decided,
		message: "key %q is not defined by the OpenAPI object it is written on and is not an " +
			"x- extension; kept verbatim under Unmodeled",
	}
}

// UnknownKeysNamed is UnknownKeysUnder for an object whose model keeps no
// census of its own, so the caller names the keys and hands over the mapping
// node they were written on.
//
// One object needs it: a Path Item Object, whose core model embeds the map of
// its operations. The unmarshaller folds an unrecognized key into that map
// rather than recording it as undeclared, so the object's census is empty
// however much the document wrote (speakeasy-api/openapi v1.24.1). Its
// leftovers are graded as any undeclared key is, because UnknownKeysUnder
// delegates here.
func UnknownKeysNamed(p *ir.Unmodeled, keys []string, root *yaml.Node,
	locate Locator, owner jsontext.Pointer, scope string,
) []ir.Diagnostic {
	return census(p, keys, root, locate, owner, scope, objectKeyClass(nil))
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
	code     string
	severity ir.Severity
	skip     []string // keywords already decided about; see DecidedKeywords
	message  string   // one %q, filled with the key
}

// census records on p every key in keys, read off the mapping node root they
// were written on, each under its own key beneath scope.
//
// It sorts a copy: neither source of keys yields document order, and sorting in
// place would reorder the slice under its owner (invariant 7).
//
// A key p already holds for this very construct is left alone and unannounced,
// since the reader that recorded it said it better (see unrecorded). That
// happens before the bound applies (see MaxUnknownKeys).
func census(p *ir.Unmodeled, keys []string, root *yaml.Node,
	locate Locator, owner jsontext.Pointer, scope string, cl keyClass,
) []ir.Diagnostic {
	if len(keys) == 0 {
		return nil // the common case: most objects write no key their model misses
	}
	fresh := unrecorded(p, slices.Sorted(slices.Values(keys)), owner, scope, cl.skip)
	if len(fresh) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	if len(fresh) > MaxUnknownKeys {
		diags = append(diags, budgetDiag(len(fresh), locate(owner)))
		fresh = fresh[:MaxUnknownKeys]
	}
	for _, key := range fresh {
		diags = append(diags, keep(p, root, key, locate, owner, scope, cl)...)
	}
	return diags
}

// keep writes one key's value under its entry and announces it, or says why it
// could not.
func keep(p *ir.Unmodeled, root *yaml.Node, key string, locate Locator, owner jsontext.Pointer, scope string, cl keyClass) []ir.Diagnostic {
	entry, at := "openapi:"+scoped(scope, key), locate(owner+ids.Ptr(key))
	if taken, occupied := (*p)[entry]; occupied {
		return []ir.Diagnostic{occupiedEntryDiag(entry, at, taken.Provenance.Pointer)}
	}
	node := RawChildNode(root, key)
	if node == nil {
		return []ir.Diagnostic{unreachableKeyDiag(entry, at)}
	}
	kept, diags := PreserveNodeInto(p, entry, node, ir.ReasonOutOfScope, at)
	if !kept {
		return diags
	}
	return append(diags, diag.Newf(cl.severity, cl.code, at, cl.message, key))
}

// unrecorded returns the keys this census has to answer for: those not in skip,
// less those a reader already recorded for the very construct.
//
// Sameness is the entry's provenance, not its presence. A reader with more to
// say about a keyword writes it at the pointer the census would use
// (`$vocabulary` and `dependentRequired` on a schema's own map), so the census
// has nothing to add. An entry pointing elsewhere is a different construct
// spelling the same key, a collision that keep reports.
func unrecorded(p *ir.Unmodeled, keys []string, owner jsontext.Pointer, scope string, skip []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if slices.Contains(skip, key) {
			continue
		}
		if e, recorded := (*p)["openapi:"+scoped(scope, key)]; recorded &&
			e.Provenance.Pointer == owner+ids.Ptr(key) {
			continue
		}
		out = append(out, key)
	}
	return out
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
// a sequenced map folds an unrecognized key into that map and reports nothing.
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
