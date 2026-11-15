// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// The characters a text file cannot hold safely, spelled as bytes, the
// backslash, and the document every fixture below extends.
const (
	srNEL = "\xc2\x85"
	srLS  = "\xe2\x80\xa8"
	srPS  = "\xe2\x80\xa9"
	srE0  = "\xee\x80\x80"
	srBOM = "\xef\xbb\xbf"
	srBS  = "\\"

	srMin = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"

	// srVersionReading is the code of a document read as YAML 1.2 that
	// declares another version.
	srVersionReading = "openapi/yaml-version-reading"
)

// compileText compiles data as the one source of a compile, with opts. A
// refusal is a diagnostic and a nil document, never a Go error.
func compileText(t *testing.T, name, data string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name, Data: []byte(data)}}, compilers.Options{FormatOptions: opts})
	require.NoError(t, err, "a spec problem is a diagnostic, not a Go error")
	return doc, diags
}

// compileDir compiles main.yaml under dir, written by layout, reading
// what its references name from disk.
func compileDir(t *testing.T, dir string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	opts.AllowExternalRefs = true
	data, err := os.ReadFile(filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: data}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: opts})
	require.NoError(t, err)
	return doc, diags
}

// byCode returns the diagnostics carrying code.
func byCode(diags []ir.Diagnostic, code string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// lines renders diags as severity, code, pointer and message, which is what a
// comparison of two compiles needs: a position is left out, so a twin written
// with a different column still compares equal.
func lines(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %d %s %s", d.Severity, d.Code, d.Provenance.Source, d.Provenance.Pointer, d.Message))
	}
	return out
}

// irTree is doc as the generic JSON value its encoding decodes to, so a test
// can name a fact by the path it is written at.
func irTree(t *testing.T, doc *ir.Document) any {
	t.Helper()
	require.NotNil(t, doc)
	enc, err := json.Marshal(doc, json.Deterministic(true))
	require.NoError(t, err)
	var v any
	require.NoError(t, json.Unmarshal(enc, &v))
	return v
}

// findByID returns the object under v whose "id" is id, or nil.
func findByID(v any, id string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if got, ok := x["id"].(string); ok && got == id {
			return x
		}
		for _, c := range x {
			if found := findByID(c, id); found != nil {
				return found
			}
		}
	case []any:
		for _, c := range x {
			if found := findByID(c, id); found != nil {
				return found
			}
		}
	}
	return nil
}

// jsonOf encodes doc without its sources and diagnostics, as withoutVersion
// does, which a twin comparison and a substring search both read.
func jsonOf(t *testing.T, doc *ir.Document) string {
	t.Helper()
	require.NotNil(t, doc)
	return withoutVersion(t, doc)
}

// containsJSON reports whether doc's encoding holds fragment.
func containsJSON(t *testing.T, doc *ir.Document, fragment string) bool {
	t.Helper()
	return strings.Contains(jsonOf(t, doc), fragment)
}
