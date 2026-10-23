package navigation

import (
	"github.com/speakeasy-api/openapi/jsonpointer"
	yaml "gopkg.in/yaml.v3"
)

// Walk reads tokens from node as the library's read of the pointer they spell
// walks them, a step at a time, until a step leaves the model. It returns where
// the walk stopped and the tokens left there, which the library reads together
// in that raw YAML: none when the model answered every token. Where a step
// finds nothing, the error is the library's, and the tokens left start at that
// step. An index the library retries (Retried) is left to the library, which
// reads the rest itself.
func Walk(node any, tokens []string) (any, []string, error) {
	for i, token := range tokens {
		r, raw := ReadingOf(node, token)
		switch r {
		case Leaving:
			return raw, tokens[i:], nil
		case Retried:
			target, err := jsonpointer.GetTarget(node, jsonpointer.PartsToJSONPointer(tokens[i:]), jsonpointer.WithStructTags("key"))
			return target, nil, err
		default:
			// A field or an entry answers the token, or the read fails here.
		}
		next, err := step(node, token)
		if err != nil {
			return nil, tokens[i:], err
		}
		node = next
	}
	return node, nil, nil
}

// Leaves returns the raw YAML the library's read of token from node goes on in,
// with the tokens after it read together there, and true: node itself when it
// is raw YAML, else the mapping an object was built from where none of its
// fields or embedded maps answers token. It is false where one does, where the
// read fails first, and where the library retries an index (Retried).
func Leaves(node any, token string) (*yaml.Node, bool) {
	r, raw := ReadingOf(node, token)
	return raw, r == Leaving
}

// Keyed is keyed, for the external tests.
var Keyed = keyed
