package irverify

import (
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// stringListType is the field type stringLists classifies. ir.Lifecycle is an
// alias of string, so a []ir.Lifecycle field is one too.
var stringListType = reflect.TypeFor[[]string]()

// listRule is what the entries of one []string field are held to.
type listRule int

const (
	// listPath is a path, one segment per level: a namespace or a field path.
	// No segment may be blank, but one may repeat (com.acme.acme). It is the
	// zero value, so a field stringLists does not list is held to it rather
	// than let through.
	listPath listRule = iota
	// listSet is a set: a lifecycle set (ir-design §5.2), or the methods a
	// token endpoint accepts (ir-design §9). No entry may be blank, and none may
	// repeat, since a repeat admits nothing more.
	listSet
	// listAliases is Naming.Aliases, which checkNaming holds to rules of its
	// own (ir-design §3.2).
	listAliases
	// listVerbatim copies a source's own entries, and whether one may repeat
	// or be blank is that format's to say: OpenAPI may list a tag twice, and
	// "" is a legal server variable value. Normalizing one is its compiler's
	// (GitHub #738 holds OpenAPI's tags and scopes).
	listVerbatim
	// listVersions holds version labels, which answer to Document.Versions as
	// a whole rather than to a rule of their own list (GitHub #860).
	listVersions
	// listMembers names members by wire name, which checkRequires holds: one
	// may be blank, as a property's may, but none repeats or names one of the
	// model's own properties.
	listMembers
)

// stringLists records the decision for every []string field the ir sources
// declare, keyed "Type.Field". TestStringLists_AreAllClassified fails when a
// field is added without one.
var stringLists = map[string]listRule{
	"GraphQLBinding.FieldPath": listPath,
	"Service.Namespace":        listPath,
	"TypeCommon.Namespace":     listPath,

	"OAuthFlow.TokenEndpointAuthMethods": listSet,
	"Operation.ParameterVisibility":      listSet,
	"Operation.ReturnTypeVisibility":     listSet,
	"Visibility.Only":                    listSet,

	"Naming.Aliases": listAliases,

	"Channel.Tags":                    listVerbatim,
	"FileInfo.ContentTypes":           listVerbatim,
	"HTTPBinding.RequestContentTypes": listVerbatim,
	"Message.Tags":                    listVerbatim,
	"Operation.Tags":                  listVerbatim,
	"PartEncoding.ContentTypes":       listVerbatim,
	"RequestCompression.Encodings":    listVerbatim,
	"SchemeUse.Scopes":                listVerbatim,
	"Server.Tags":                     listVerbatim,
	"ServerVariable.Enum":             listVerbatim,
	"TypeCommon.Tags":                 listVerbatim,

	"Model.Requires": listMembers,
	// ir.FlattenedProperties' result and its walk's own state, which no
	// Document holds: names Requires lists, each once.
	"Flattening.Undeclared": listMembers,
	"flattener.requires":    listMembers,

	"Availability.Added":   listVersions,
	"Availability.Removed": listVersions,
	"Document.Versions":    listVersions,
}

// heldList is one []string field of a struct type that a rule here holds.
type heldList struct {
	index int
	name  string
	rule  listRule
}

// checkStringLists holds every []string field the walk reaches to the rule
// stringLists records for it: a path segment is never blank, and a set entry
// is neither blank nor repeated. A repeat is reported at its later entry,
// naming the earlier.
//
// Field lists are read once per struct type, and paths match the walk's
// spelling. An entry that does not decode is never blank, since its bytes read
// as U+FFFD, and it is no repeat either: checkUTF8 alone reports it.
func checkStringLists(*ir.Document) walkCheck {
	var vs []Violation
	fields := map[reflect.Type][]heldList{}
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Struct {
				return true
			}
			byType, seen := fields[v.Type()]
			if !seen {
				byType = heldListsOf(v.Type())
				fields[v.Type()] = byType
			}
			for _, f := range byType {
				vs = appendListViolations(vs, v.Field(f.index), at, f)
			}
			return true
		},
		report: reporting(&vs),
	}
}

// heldListsOf returns t's []string fields that a rule here holds, an unlisted
// one included. A field promoted into t from an embedded struct is read where
// the walk reaches its declaring type, so once.
func heldListsOf(t reflect.Type) []heldList {
	var out []heldList
	for i := range t.NumField() {
		sf := t.Field(i)
		rule := stringLists[t.Name()+"."+sf.Name]
		if sf.Type != stringListType || (rule != listPath && rule != listSet) {
			continue
		}
		out = append(out, heldList{index: i, name: sf.Name, rule: rule})
	}
	return out
}

// appendListViolations reports each entry of the held list f that breaks its
// rule, at the entry itself; owner is where the struct holding it sits.
func appendListViolations(vs []Violation, list reflect.Value, owner ir.WalkPath, f heldList) []Violation {
	first := map[string]int{}
	for i := range list.Len() {
		entry := list.Index(i).String()
		switch earlier, repeated := first[entry]; {
		case isBlankName(entry):
			vs = append(vs, blankEntry(f.rule, entryPath(owner, f, i)))
		case !utf8.ValidString(entry):
			// checkUTF8 reports it.
		case f.rule == listSet && repeated:
			vs = append(vs, Violation{
				Code:    "ir/set-entry-duplicate",
				Message: "set entry " + strconv.Quote(entry) + " is listed here and at index " + strconv.Itoa(earlier),
				Path:    entryPath(owner, f, i),
			})
		default:
			first[entry] = i
		}
	}
	return vs
}

// entryPath spells entry i of the held list f of the struct at owner, as the walk
// spells the entry itself.
func entryPath(owner ir.WalkPath, f heldList, i int) string {
	return owner.String() + "." + f.name + "[" + strconv.Itoa(i) + "]"
}

// blankEntry is the violation a blank entry of a held list draws.
func blankEntry(rule listRule, where string) Violation {
	if rule == listSet {
		return Violation{Code: "ir/set-entry-blank", Message: "set entry is blank, so it names nothing", Path: where}
	}
	return Violation{Code: "ir/path-segment-blank", Message: "path segment is blank, so it names no level", Path: where}
}
