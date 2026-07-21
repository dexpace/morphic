package protobuf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/reporter"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// loaded is the successful output of the load phase: one fully linked root file
// descriptor plus the identity metadata the rest of the compiler needs. A nil
// *loaded with error-severity diagnostics means the source is a spec problem the
// compiler refuses to lower (a parse error or an unresolvable import).
type loaded struct {
	File   protoreflect.FileDescriptor // linked, feature-resolved root file
	Format compilers.SourceFormat      // "protobuf" + syntax digit
	Source ir.SourceInfo               // format tag, path, content hash
}

// load parses, links, and feature-resolves one .proto source. Well-known-type
// imports resolve from the parser's bundle; any other import is unresolvable
// because the compiler holds only the root bytes and does no file I/O. Spec
// problems become ir.Diagnostic values; the parser recovers its own panics into
// errors, so no panic escapes and the Go error return is unused here.
//
//nolint:unparam // srcIndex varies once Compile drives a multi-source loop
func load(ctx context.Context, srcIndex int, src compilers.Source, _ Options) (*loaded, []ir.Diagnostic, error) {
	var diags []ir.Diagnostic
	rep := reporter.NewReporter(
		func(err reporter.ErrorWithPos) error {
			diags = append(diags, parseDiag(srcIndex, err))
			return err // stop at the first hard error; it is already recorded
		},
		func(warn reporter.ErrorWithPos) {
			diags = append(diags, diagf(ir.SeverityWarning, codeWarning, posOf(srcIndex, warn), "%s", warn.Error()))
		},
	)

	root, err := compileRoot(ctx, src, rep)
	if err != nil {
		if len(diags) == 0 { // reporter never fired: a resolution or internal error
			diags = append(diags, diagf(ir.SeverityError, importOrCompileCode(err),
				ir.Provenance{Source: srcIndex, Pointer: src.Path}, "%s", err.Error()))
		}
		return nil, diags, nil // refuse to lower, do not abort the batch
	}
	return &loaded{
		File:   root,
		Format: compilers.SourceFormat{Name: "protobuf", Version: syntaxDigit(root)},
		Source: ir.SourceInfo{
			Format: "protobuf@" + syntaxDigit(root),
			Path:   src.Path,
			Hash:   sourceHash(src.Data),
		},
	}, diags, nil
}

// compileRoot links the single root file against the bundled well-known types
// and returns its linked descriptor.
func compileRoot(ctx context.Context, src compilers.Source, rep reporter.Reporter) (protoreflect.FileDescriptor, error) {
	resolver := protocompile.WithStandardImports(&protocompile.SourceResolver{
		Accessor: func(path string) (io.ReadCloser, error) {
			if path == src.Path {
				return io.NopCloser(bytes.NewReader(src.Data)), nil
			}
			return nil, fs.ErrNotExist // any non-root, non-WKT import is unresolvable
		},
	})
	c := protocompile.Compiler{
		Resolver:       resolver,
		SourceInfoMode: protocompile.SourceInfoStandard, // comments → Docs, positions → provenance
		Reporter:       rep,
	}
	compiled, err := c.Compile(ctx, src.Path)
	if err != nil {
		return nil, fmt.Errorf("compile %q: %w", src.Path, err)
	}
	return compiled[0], nil
}

// parseDiag converts one reporter error into a diagnostic, classifying an
// unresolved import distinctly from a general compile error.
func parseDiag(srcIndex int, err reporter.ErrorWithPos) ir.Diagnostic {
	return diagf(ir.SeverityError, importOrCompileCode(err), posOf(srcIndex, err), "%s", err.Error())
}

// importOrCompileCode selects the unresolved-import code when the error is an
// import-resolution failure, else the general compile-error code.
func importOrCompileCode(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return codeUnresolvedImport
	}
	return codeCompile
}

// posOf builds line:col provenance from a positioned parser error.
func posOf(srcIndex int, err reporter.ErrorWithPos) ir.Provenance {
	p := err.GetPosition()
	return ir.Provenance{Source: srcIndex, Pointer: fmt.Sprintf("%d:%d", p.Line, p.Col)}
}

// syntaxDigit maps a file's syntax to the version digit the compiler reports:
// "2" for proto2, "2023" for the 2023 edition, and "3" for proto3.
func syntaxDigit(fd protoreflect.FileDescriptor) string {
	switch fd.Syntax() {
	case protoreflect.Proto2:
		return "2"
	case protoreflect.Editions:
		return "2023"
	default:
		return "3"
	}
}

// sourceHash returns the lowercase hex SHA-256 of the raw source bytes, used as
// the SourceInfo content hash for caching and golden-snapshot identity.
func sourceHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
