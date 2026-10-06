package openapitest

import (
	"context"
	"encoding/json/jsontext"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"
)

// MaxTreeDepth bounds how deep Positions reads the tree a model was built
// from. The corpus nests far shallower, so a spec reaching it has outgrown the
// walk, which fails rather than leave the paths below unread.
const MaxTreeDepth = 64

// SpecFiles returns every spec under root, in filepath.WalkDir's lexical order:
// a .yaml, .yml or .json file that is no .golden.json IR snapshot, the filter
// internal/harness sweeps a directory with.
func SpecFiles(t TB, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".golden.json") {
			return err
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the specs under %s: %v", root, err)
		return nil
	}
	if len(files) == 0 {
		t.Fatalf("no spec under %s", root)
	}
	return files
}

// Positions returns the pointers a differential over doc asks about: each place
// the model's walk reaches, each path through the tree the model was built
// from, each path below an internal $ref's target read through the $ref, and
// each of those with a token appended that names nothing, an index, the empty
// key, an escape or a schema keyword. A read that answers otherwise than the
// library's at any of them shows.
func Positions(ctx context.Context, t TB, doc *soa.OpenAPI) []jsontext.Pointer {
	t.Helper()
	var tree []jsontext.Pointer
	var refs []written
	if !treePaths(t, doc.GetRootNode(), "", &tree, &refs, 0) {
		return nil
	}
	var bases []jsontext.Pointer
	for item := range soa.Walk(ctx, doc) {
		bases = append(bases, jsontext.Pointer(item.Location.ToJSONPointer()))
	}
	bases = append(bases, tree...)
	slices.Sort(tree)
	for _, ref := range refs {
		bases = append(bases, throughReference(ref, tree)...)
	}
	seen := map[jsontext.Pointer]bool{}
	var out []jsontext.Pointer
	for _, base := range bases {
		for _, extra := range []jsontext.Pointer{"", "/0", "/x", "/", "/~1", "/properties"} {
			if p := base + extra; !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// written is a $ref the tree holds: the position of the mapping writing it, and
// its value.
type written struct {
	site  jsontext.Pointer
	value string
}

// throughReference returns, for ref an internal $ref, each path of tree, which
// is sorted, below the position it names, read from where it is written
// through it: its site, then the path below the target.
func throughReference(ref written, tree []jsontext.Pointer) []jsontext.Pointer {
	r := references.Reference(ref.value)
	target := jsontext.Pointer(r.GetJSONPointer())
	if r.GetURI() != "" || !strings.HasPrefix(string(target), "/") {
		return nil
	}
	below := string(target) + "/"
	var out []jsontext.Pointer
	for i, _ := slices.BinarySearch(tree, jsontext.Pointer(below)); i < len(tree) && strings.HasPrefix(string(tree[i]), below); i++ {
		out = append(out, ref.site+tree[i][len(target):])
	}
	return out
}

// treePaths appends the pointer of each node in the tree under n, below at, to
// out, and each $ref it writes to refs, and reports false once past
// MaxTreeDepth, which fails t. A model's tree is the mapping it was built
// from, and one built in code has none.
func treePaths(t TB, n *yaml.Node, at jsontext.Pointer, out *[]jsontext.Pointer, refs *[]written, depth int) bool {
	if depth >= MaxTreeDepth {
		t.Fatalf("the tree under %q nests past %d levels", at, MaxTreeDepth)
		return false
	}
	if n == nil {
		return true
	}
	var children []*yaml.Node
	var tokens []string
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			children, tokens = append(children, value), append(tokens, key.Value)
			if key.Value == "$ref" && value.Kind == yaml.ScalarNode {
				*refs = append(*refs, written{site: at, value: value.Value})
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			children, tokens = append(children, c), append(tokens, strconv.Itoa(i))
		}
	default:
		// A scalar or an alias holds no path below it.
	}
	for i, c := range children {
		p := jsontext.Pointer(string(at) + "/" + escape(tokens[i]))
		*out = append(*out, p)
		if !treePaths(t, c, p, out, refs, depth+1) {
			return false
		}
	}
	return true
}

// escape spells token as a pointer token, byte for byte: jsontext would spell a
// byte that is no UTF-8 as U+FFFD, a key the library never compares.
func escape(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}
