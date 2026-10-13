package irverify_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// listFixture is one []string field a rule holds: where a fixture puts its
// entries, the path the walk reaches that list by, and whether it is a set
// rather than a path.
type listFixture struct {
	set   func(doc *ir.Document, entries []string)
	path  string
	isSet bool
}

// operation is the one operation heldListDoc declares.
func operation(doc *ir.Document) *ir.Operation {
	return &doc.Services[0].Groups[0].Operations[0]
}

// heldLists are the IR's path and set lists. TypeCommon is embedded, so its
// list sits on the type's own path.
var heldLists = map[string]listFixture{
	"TypeCommon.Namespace": {
		set:  func(doc *ir.Document, s []string) { doc.Types["t/x/A"].Common().Namespace = s },
		path: "doc.Types[t/x/A].Namespace",
	},
	"Service.Namespace": {
		set:  func(doc *ir.Document, s []string) { doc.Services[0].Namespace = s },
		path: "doc.Services[0].Namespace",
	},
	"GraphQLBinding.FieldPath": {
		set:  func(doc *ir.Document, s []string) { operation(doc).Bindings.GraphQL.FieldPath = s },
		path: "doc.Services[0].Groups[0].Operations[0].Bindings.GraphQL.FieldPath",
	},
	"Visibility.Only": {
		set: func(doc *ir.Document, s []string) {
			doc.Types["t/x/A"].(*ir.Model).Properties[0].Visibility.Only = s
		},
		path:  "doc.Types[t/x/A].Properties[0].Visibility.Only",
		isSet: true,
	},
	"Operation.ParameterVisibility": {
		set:   func(doc *ir.Document, s []string) { operation(doc).ParameterVisibility = s },
		path:  "doc.Services[0].Groups[0].Operations[0].ParameterVisibility",
		isSet: true,
	},
	"Operation.ReturnTypeVisibility": {
		set:   func(doc *ir.Document, s []string) { operation(doc).ReturnTypeVisibility = s },
		path:  "doc.Services[0].Groups[0].Operations[0].ReturnTypeVisibility",
		isSet: true,
	},
}

// heldListDoc returns a document that verifies clean and holds every list in
// heldLists, each empty until a case sets one.
func heldListDoc() *ir.Document {
	doc := opDoc(ir.Operation{
		ID:       "op/x/S/op",
		Name:     named("op"),
		Bindings: ir.OpBindings{GraphQL: &ir.GraphQLBinding{Kind: "query"}},
	})
	doc.IRVersion = ir.IRVersion
	doc.Services[0].Groups[0].Name = named("g")
	doc.Types = ir.TypeRegistry{"t/x/A": &ir.Model{ID: "t/x/A", Name: named("a"),
		Properties: []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "t/x/A"}}}}}
	return doc
}

// TestVerify_HeldListFixturesMatchTheRules holds heldLists to the fields
// stringLists holds and to each one's rule, so a list added to stringLists
// without a fixture, or a fixture expecting the other rule, reddens here.
func TestVerify_HeldListFixturesMatchTheRules(t *testing.T) {
	t.Parallel()
	fixtures := map[string]bool{}
	for name, list := range heldLists {
		fixtures[name] = list.isSet
	}
	assert.Equal(t, irverify.HeldLists(), fixtures)
}

// TestVerify_BlankListEntryIsAViolation holds each path and set list to its
// blank rule. An entry made only of runes Unicode calls invisible is as blank as
// "", and each is reported at the entry, so the visible one beside them is not.
func TestVerify_BlankListEntryIsAViolation(t *testing.T) {
	t.Parallel()
	require.Empty(t, verifyDeclared(heldListDoc()), "the fixture must draw nothing of its own")
	for name, list := range heldLists {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, message := "ir/path-segment-blank", "path segment is blank, so it names no level"
			if list.isSet {
				code, message = "ir/set-entry-blank", "set entry is blank, so it names nothing"
			}
			doc := heldListDoc()
			list.set(doc, []string{"", " ", "\u3164", "acme"})

			got := verifyDeclared(doc)
			require.Len(t, got, 3, "one violation per blank entry")
			for i, v := range got {
				assert.Equal(t, code, v.Code)
				assert.Equal(t, fmt.Sprintf("%s[%d]", list.path, i), v.Path)
				assert.Equal(t, message, v.Message)
			}
		})
	}
}

// TestVerify_RepeatedListEntry holds the line between the two rules. A path may
// repeat a segment, since com.acme.acme has three levels; a set may not, and
// the repeat is reported at the later entry, naming the earlier.
func TestVerify_RepeatedListEntry(t *testing.T) {
	t.Parallel()
	for name, list := range heldLists {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := heldListDoc()
			list.set(doc, []string{"acme", "com", "acme"})
			got := verifyDeclared(doc)
			if !list.isSet {
				assert.Empty(t, got, "a repeated segment is a level of its own")
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, "ir/set-entry-duplicate", got[0].Code)
			assert.Equal(t, list.path+"[2]", got[0].Path)
			assert.Equal(t, `set entry "acme" is listed here and at index 0`, got[0].Message)
		})
	}
}

// TestVerify_IllFormedListEntryIsReportedOnce pins that an entry whose bytes do
// not decode is checkUTF8's alone. Its bytes read as U+FFFD, which is visible,
// so no blank rule fires; and two such entries are no set repeat, since until
// they decode they name nothing to compare.
func TestVerify_IllFormedListEntryIsReportedOnce(t *testing.T) {
	t.Parallel()
	ill := string([]byte{'c', 'a', 'f', 0xe9})
	for name, list := range heldLists {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := heldListDoc()
			list.set(doc, []string{ill, ill})
			assert.Equal(t, []string{"ir/invalid-utf8", "ir/invalid-utf8"}, codesOf(verifyDeclared(doc)))
		})
	}
}

// TestVerify_ListEntryIsSpelledAsTheWalkWould ties the hand-built entry path to
// ir.WalkValues' own grammar, on each list and past the single digits, where a
// hand-built index and a formatted one last agree.
func TestVerify_ListEntryIsSpelledAsTheWalkWould(t *testing.T) {
	t.Parallel()
	const size = 12
	for name, list := range heldLists {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entries := make([]string, size)
			for i := range entries {
				entries[i] = fmt.Sprintf("entry.%d", i)
			}
			doc := heldListDoc()
			list.set(doc, entries)
			walked := map[string]string{} // entry → the path the walk renders for it
			ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
				if v.Kind() == reflect.String {
					walked[v.String()] = path
				}
				return true
			})

			list.set(doc, make([]string, size))
			got := verifyDeclared(doc)
			require.Len(t, got, size)
			reported := make([]string, 0, size) // Verify sorts [10] before [2]
			want := make([]string, 0, size)
			for i, v := range got {
				reported = append(reported, v.Path)
				want = append(want, walked[entries[i]])
			}
			assert.ElementsMatch(t, want, reported)
		})
	}
}

// TestVerify_VerbatimAndVersionListsAreNotHeld pins two classifications by
// example: a type's Tags, beside the Namespace on the same node, and the
// document's Versions. Each holds a blank and a repeat, and neither is a rule
// of the list: tags are the source's own (GitHub #738), versions answer to
// Document.Versions as a whole (GitHub #860).
func TestVerify_VerbatimAndVersionListsAreNotHeld(t *testing.T) {
	t.Parallel()
	doc := heldListDoc()
	doc.Types["t/x/A"].Common().Tags = []string{"", "a", "a"}
	doc.Versions = []string{"", "v1", "v1"}
	assert.Empty(t, verifyDeclared(doc))
}
