package graphql

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/parser"

	"github.com/dexpace/morphic/ir"
)

// errParse marks a hard failure to parse the SDL inputs — reserved for a parser
// panic, an I/O- or programmer-level fault distinct from a spec problem reported
// as a diagnostic. Ordinary syntax errors are diagnostics, not this error.
var errParse = errors.New("parse sdl")

// maxTokenLimit bounds the parser's token budget (styleguide bounded-everything
// rule): a pathologically large SDL document is refused rather than exhausting
// memory. It is generous enough for any hand- or tool-authored schema.
const maxTokenLimit = 1 << 22

// loaded is the successful output of the load phase: the parsed SDL document,
// the per-source provenance index, and the SourceInfo records.
type loaded struct {
	doc      *ast.SchemaDocument
	srcIndex map[*ast.Source]int // ast.Source -> Document.Sources index
	sources  []ir.SourceInfo
}

// load parses every input as one merged SDL document. Syntax errors become
// error-severity diagnostics with a nil document (a refusal to lower that does
// not abort the batch); the Go error return is reserved for a parser panic.
func load(srcs []ir.SourceInfo, inputs []*ast.Source, index map[*ast.Source]int) (*loaded, []ir.Diagnostic, error) {
	doc, perr := parseAll(inputs)
	if perr != nil {
		if errors.Is(perr, errParse) {
			return nil, nil, perr
		}
		return nil, parseDiags(perr), nil
	}
	return &loaded{doc: doc, srcIndex: index, sources: srcs}, nil, nil
}

// parseAll runs the SDL parser over every input, converting a parser panic into
// an errParse Go error so the compiler upholds the no-panics-escape invariant.
// The named returns are reset in the recover so a partial document never leaks.
func parseAll(inputs []*ast.Source) (doc *ast.SchemaDocument, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc = nil
			err = fmt.Errorf("parser panicked (%v): %w", r, errParse)
		}
	}()
	parsed, perr := parser.ParseSchemasWithLimit(maxTokenLimit, inputs...)
	if perr != nil {
		return nil, perr
	}
	return parsed, nil
}

// parseDiags converts SDL parse errors into error-severity diagnostics with
// line:col provenance drawn from the gqlparser error locations.
func parseDiags(err error) []ir.Diagnostic {
	var gqlErr *gqlerror.Error
	if errors.As(err, &gqlErr) {
		return []ir.Diagnostic{diagf(ir.SeverityError, codeParse, gqlErrProvenance(gqlErr),
			"%s", gqlErr.Message)}
	}
	return []ir.Diagnostic{diagf(ir.SeverityError, codeParse, ir.Provenance{}, "%s", err.Error())}
}

// gqlErrProvenance builds provenance from a gqlparser error's first location.
// gqlparser locations carry no *ast.Source back-reference, so the source index
// defaults to the first input; single-file schemas are exact.
func gqlErrProvenance(err *gqlerror.Error) ir.Provenance {
	if len(err.Locations) == 0 {
		return ir.Provenance{}
	}
	loc := err.Locations[0]
	return ir.Provenance{Pointer: fmt.Sprintf("%d:%d", loc.Line, loc.Column)}
}

// sourcesFrom builds the ast.Source inputs, the SourceInfo records, and the
// provenance index from the caller's pre-read bytes. Format is stamped per file;
// federation detection refines it after parsing.
func sourcesFrom(in []loadInput) ([]*ast.Source, []ir.SourceInfo, map[*ast.Source]int) {
	inputs := make([]*ast.Source, 0, len(in))
	infos := make([]ir.SourceInfo, 0, len(in))
	index := make(map[*ast.Source]int, len(in))
	for i, li := range in {
		src := &ast.Source{Name: li.path, Input: string(li.data)}
		inputs = append(inputs, src)
		index[src] = i
		infos = append(infos, ir.SourceInfo{Format: "graphql@sdl", Path: li.path, Hash: sourceHash(li.data)})
	}
	return inputs, infos, index
}

// loadInput is one pre-read SDL source: a path and its bytes.
type loadInput struct {
	path string
	data []byte
}

// sourceHash returns the lowercase hex SHA-256 of the raw source bytes, used as
// the SourceInfo content hash for caching and golden-snapshot identity.
func sourceHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
