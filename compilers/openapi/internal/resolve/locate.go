package resolve

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
)

// located is where the chain of the last reference a walk of a pointer
// through the model passed ends, and whether it passed one.
type located struct {
	end    End
	passes bool
}

// locate walks pointer through Doc as the resolver's walk reads it, a token at
// a time, noting each reference Ends names that it steps past, read as what it
// resolved to. A schema's own $ref is read as a keyword, so the walk ends at a
// schema, as it does at a token that leaves the model (navigation.ReadingOf):
// raw YAML holds no reference the resolver resolved (see leave). Without Ends
// it notes nothing.
func (s Scope) locate(pointer jsontext.Pointer) located {
	var l located
	tokens, ok := navigation.Tokens(pointer)
	if !ok || s.Ends == nil {
		return l
	}
	node := s.Doc
	for _, token := range tokens {
		if _, schema := node.(*oas3.JSONSchema[oas3.Referenceable]); schema {
			return l
		}
		reading, raw := navigation.ReadingOf(node, token)
		if reading == navigation.Leaving {
			l.leave(s, node, raw, token)
			return l
		}
		next, ok := navigation.Step(node, token)
		if !ok {
			return l
		}
		if e, isRef := s.Ends(node); isRef {
			l.end, l.passes = e, true
		}
		node = next
	}
	return l
}

// leave notes node passed where the walk leaves the model from it for raw: a
// reference is stepped past only where raw, the mapping its target was built
// from, holds token, as the resolver's read of the pointer finds it (see
// Scope.Holds). Raw YAML is no reference, and the walk passes nothing it holds.
func (l *located) leave(s Scope, node any, raw *yaml.Node, token string) {
	if _, isRaw := node.(*yaml.Node); isRaw {
		return
	}
	if e, isRef := s.Ends(node); isRef && held(s.Holds, node, raw, token) {
		l.end, l.passes = e, true
	}
}

// held reports whether the library's read of token from node, which leaves the
// model for raw, finds a node: holds's answer, or the library's without one.
func held(holds func(*yaml.Node, string) bool, node any, raw *yaml.Node, token string) bool {
	if holds != nil {
		return holds(raw, token)
	}
	_, found := navigation.Step(node, token)
	return found
}
