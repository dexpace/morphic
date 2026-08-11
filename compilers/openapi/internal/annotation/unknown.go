package annotation

import (
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
// It is a key the document does write. The parser reads a mapping through its
// `<<` merge keys, so a key merged in from an anchored mapping is reported here
// while the mapping this reads holds no pair for it; resolving that needs the
// merge-expanded view (internal/nodeview), which every raw-node reader in this
// package lacks and which is a mechanism of its own to thread through — see
// GitHub #395. Announced rather than passed over in the meantime, since a key
// that reaches the IR in no form at all is the loss this census exists to end.
//
// Warning rather than the error UnpreservableDiag gives a value that cannot be
// rendered: a document merging keys is legal input and still lowers, and an
// error would both refuse it under the default --fail-on and stop harness.Check
// before the invariant checks run.
func unreachableKeyDiag(entry, at string, srcIndex int) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyUnreachable,
		ir.Provenance{Source: srcIndex, Pointer: at},
		"%s is written at a key the source mapping does not present directly, most likely "+
			"merged in through a `<<`; it is represented in the IR in no form at all", entry)
}

// MaxUnknownKeys bounds how many keys one object contributes to the IR.
//
// The key set is the document's to choose the size of, and every collection in
// this compiler is bounded, so this one is too. It sits far above what a
// document writes by accident, so an object reaching it is generated or hostile
// rather than merely sloppy, and what it discards is announced under
// diag.UnknownKeyBudget rather than dropped in silence.
//
// It bounds what this census contributes, not what the object wrote: a key
// another reader already kept is filtered out before the bound applies, since
// spending a slot on an entry that is in the document either way would drop a
// key that is not.
const MaxUnknownKeys = 64

// DecidedKeywords are the JSON Schema keywords the library's schema model names
// no field for and this compiler has already decided about, so the census must
// not claim them as unread. Each decision is recorded where it was made, and the
// schema walk's 2020-12 vocabulary test fails if one starts being carried:
//
//   - $comment — 2020-12 §8.3 forbids presenting it to end users, so no SDK
//     emitter may see it. Dropped on purpose.
//   - $dynamicAnchor — read by the anchor index as a reference target, which is
//     what lets a $dynamicRef expand; declaring one says nothing about the shape.
//   - $dynamicRef — carried by the dynamic-reference lowering, which either
//     expands it into the position's type or keeps it under a reason of its own.
//     An entry beside an expanded one would tell a consumer the compiler ignored
//     a reference it had in fact resolved.
//
// The other 2020-12 keywords with no field of their own — $vocabulary and
// dependentRequired — need no entry here. Their readers write to the same map,
// so the census finds them already recorded and leaves them alone.
var DecidedKeywords = []string{"$comment", "$dynamicAnchor", "$dynamicRef"}

// UnknownKeywordsIn records on p the keywords s writes that no field of the JSON
// Schema model names, and announces each.
//
// OpenAPI 3.1 schemas are JSON Schema 2020-12, where an unrecognized keyword is
// legal input: the specification requires an implementation to ignore what it
// does not recognize and allows such a keyword to carry meaning for other
// tooling. So this reports a decision rather than a fault, and is graded
// accordingly — see diag.UnknownSchemaKeyword.
//
// It keeps only what no other reader kept, which is why it runs after all of
// them: `$vocabulary` and `dependentRequired` have no field in the model either
// and are read straight off the raw node by readers with more to say about them,
// so the census finds those already recorded and leaves them alone. A keyword no
// reader leaves a trace of needs naming in DecidedKeywords instead.
func UnknownKeywordsIn(p *ir.Unmodeled, s *oas3.Schema, pointer string, srcIndex int) []ir.Diagnostic {
	return census(p, s, srcIndex, pointer, "", keyClass{
		code:     diag.UnknownSchemaKeyword,
		severity: ir.SeverityInfo,
		skip:     DecidedKeywords,
		message: "keyword %q has no field in the schema model this compiler lowers and no IR " +
			"position of its own; kept verbatim under Unmodeled",
	})
}

// UnknownKeysIn records on p the keys an OpenAPI object writes that the
// specification neither defines nor admits as an extension, for an object
// lowering to a node with an Unmodeled map of its own. owner is the object's own
// source pointer.
//
// Unlike its schema neighbour this reports a fault: OpenAPI gives each of its
// objects a closed key set and requires every extension to be prefixed x-, so a
// key that is neither is nothing the document is permitted to write — in
// practice a misspelling of the field beside it. It is kept all the same,
// because invariant 2 does not bend for invalid input, and a misspelt key is the
// one a reader most needs to find.
func UnknownKeysIn(p *ir.Unmodeled, model any, srcIndex int, owner string) []ir.Diagnostic {
	return UnknownKeysUnder(p, model, srcIndex, owner, "")
}

// UnknownKeysUnder is UnknownKeysIn with every entry keyed beneath scope, for
// the objects with no Unmodeled map of their own, whose keys ride on the nearest
// node that has one — an info object's on the document, a tag's on the document.
//
// scope says which object wrote them: the source path from the carrier down to
// the object. Several objects reach one map, where "openapi:status" from two of
// them would be a single key and the entry that survived would depend on which
// lowering ran last.
func UnknownKeysUnder(p *ir.Unmodeled, model any, srcIndex int, owner, scope string) []ir.Diagnostic {
	return census(p, model, srcIndex, owner, scope, keyClass{
		code:     diag.UnknownObjectKey,
		severity: ir.SeverityWarning,
		message: "key %q is not defined by the OpenAPI object it is written on and is not an " +
			"x- extension; kept verbatim under Unmodeled",
	})
}

// keyClass is how a key the model does not name is graded: which diagnostic
// announces it, and at what severity.
//
// The reason is not part of it. Both classes carry ReasonOutOfScope, because
// that is a property of the construct rather than of the document: no IR node is
// coming for a key the format does not define, nor for one a schema dialect
// defines and this compiler does not model, so an emitter policy layer is the
// only consumer either has. Which of the two a key is says something about the
// source, and the diagnostic channel is where this compiler says that.
type keyClass struct {
	code     string
	severity ir.Severity
	skip     []string // keywords already decided about; see DecidedKeywords
	message  string   // one %q, filled with the key
}

// census records on p every key model's source object wrote that its own model
// names no field for, each under its own key beneath scope.
//
// A key p already holds is left alone and not announced: the census is the
// complement of everything the compiler read, not only of what the model names,
// and a reader with a reason of its own for a keyword has already said it
// better. Those are filtered before the bound applies — see MaxUnknownKeys.
func census(p *ir.Unmodeled, model any, srcIndex int, owner, scope string, cl keyClass) []ir.Diagnostic {
	keys, root := undeclaredKeys(model)
	fresh := unrecorded(p, keys, scope, cl.skip)
	if len(fresh) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	if len(fresh) > MaxUnknownKeys {
		diags = append(diags, budgetDiag(len(fresh), owner, srcIndex))
		fresh = fresh[:MaxUnknownKeys]
	}
	for _, key := range fresh {
		entry, at := "openapi:"+scoped(scope, key), owner+ids.Ptr(key)
		node := RawChildNode(root, key)
		if node == nil {
			diags = append(diags, unreachableKeyDiag(entry, at, srcIndex))
			continue
		}
		kept, keptDiags := PreserveNodeInto(p, entry, node, ir.ReasonOutOfScope, at, srcIndex)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, diag.Newf(cl.severity, cl.code,
			ir.Provenance{Source: srcIndex, Pointer: at}, cl.message, key))
	}
	return diags
}

// unrecorded returns the keys this census has to contribute: the ones no reader
// already kept on p, less the ones cl has decided about.
func unrecorded(p *ir.Unmodeled, keys []string, scope string, skip []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, recorded := (*p)["openapi:"+scoped(scope, key)]; recorded || slices.Contains(skip, key) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// scoped spells one entry's key on the carrier holding it.
//
// The key is escaped as one segment while scope is a path of literals already
// spelled that way, which is what stops a key holding a "/" from reading as a
// scope of its own: a root key spelled "info/contact/slack" would otherwise be
// the very entry the contact object's own "slack" keys under, and the second
// site to reach the carrier would find the first already there and drop its key
// without a word. ids.Scope records that rule for the scopes a document chooses
// the segments of; a key the document chose every character of needs it too.
func scoped(scope, key string) string {
	if scope == "" {
		return ids.Scope(key)
	}
	return scope + "/" + ids.Scope(key)
}

// budgetDiag reports the keys past MaxUnknownKeys, which reach the IR in no form.
func budgetDiag(total int, owner string, srcIndex int) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyBudget,
		ir.Provenance{Source: srcIndex, Pointer: owner},
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

// undeclaredKeys returns, sorted, the keys model's source object wrote that its
// model names no field for, and the mapping node they were written on.
//
// Sorted, and on a copy: the library fills that list from a parallel walk of the
// mapping under a mutex, so its order is neither source order nor stable, and
// the slice it hands back is the model's own. An unsorted read would order this
// compiler's diagnostics by something the source does not decide, which
// invariant 7 forbids.
//
// A model reporting no census yields nothing rather than panicking. The receiver
// may be a typed nil — an absent object is what the getters return for one the
// document omitted — and a promoted method on one of those dereferences it.
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
	return slices.Sorted(slices.Values(core.GetUnknownProperties())), obj.GetRootNode()
}
