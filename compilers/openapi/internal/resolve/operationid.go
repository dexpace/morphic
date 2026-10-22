package resolve

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
)

// OperationIDAt returns the operationId of the operation written at pointer,
// and "" where none is there or it names none. raw is the document's YAML,
// read as the library's pointer walk reads it (navigation.Step): a mapping
// written there is the operation, as a reference resolves to it, siblings of a
// path item's $ref included. model, the source's, answers only a pointer raw
// holds nothing at, one that reaches its operation through a reference. Either
// reads any scalar but null as text.
func OperationIDAt(raw *yaml.Node, model *soa.OpenAPI, pointer jsontext.Pointer) string {
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return ""
	}
	if raw != nil {
		if written, ok := walk(raw, tokens); ok {
			return rawOperationID(written)
		}
	}
	if model == nil {
		return ""
	}
	written, ok := walk(model, tokens)
	if !ok {
		return ""
	}
	op, _ := written.(*soa.Operation)
	return op.GetOperationID()
}

// walk steps tokens down from root as navigation.Step does, which reads
// through a raw YAML alias, and returns what it reaches, or false where a
// token finds nothing.
func walk(root any, tokens []string) (any, bool) {
	node := root
	for _, token := range tokens {
		next, ok := navigation.Step(node, token)
		if !ok {
			return nil, false
		}
		node = next
	}
	return node, true
}

// rawOperationID is the operationId written in what raw YAML holds at an
// operation's position, and "" where it writes none: only a mapping has one.
// A value written as an alias reads as its anchored scalar, which one hop
// reaches, since yaml.v3 parses no alias of an alias.
func rawOperationID(written any) string {
	id, _ := navigation.Step(written, "operationId")
	scalar, ok := id.(*yaml.Node)
	if ok && scalar.Kind == yaml.AliasNode {
		scalar = scalar.Alias
	}
	if !ok || scalar.Kind != yaml.ScalarNode || scalar.ShortTag() == "!!null" {
		return ""
	}
	return scalar.Value
}
