// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// compilerInternal is the directory holding the OpenAPI compiler's own packages.
const compilerInternal = "compilers/openapi/internal"

// layer is what a package of the OpenAPI compiler knows of the source, least
// first. A package may import its own layer and those below it.
type layer uint8

const (
	// layerInference reads the built document and no tree.
	layerInference layer = iota + 1
	// layerDocument reads bytes, trees, pointers and overlays.
	layerDocument
	// layerGrammar holds a format's objects as data and which rows read a position.
	layerGrammar
	// layerSchema reads JSON Schema.
	layerSchema
	// layerService reads OpenAPI's own objects, and the load that models them.
	layerService
)

var layerNames = map[layer]string{
	layerInference: "inference",
	layerDocument:  "document",
	layerGrammar:   "grammar",
	layerSchema:    "schema",
	layerService:   "service",
}

func (l layer) String() string {
	if name, ok := layerNames[l]; ok {
		return name
	}
	return fmt.Sprintf("layer(%d)", uint8(l))
}

// layers names the layer of each package under compilerInternal, keyed by its
// path beneath it, one row each. It holds no import: the import graph says
// what a package reaches, and TestLayers_NoPackageImportsAboveItsLayer reads
// that against this table. Packages that exist for tests, and canary, have no
// row (testSupport, unlayered).
var layers = map[string]layer{
	"diag":        layerInference,
	"ids":         layerInference,
	"compileddoc": layerInference,
	"runtimeexpr": layerInference,
	"promotion":   layerInference,
	"pagination":  layerInference,
	"replacement": layerInference,
	"convention":  layerInference,
	"stream":      layerInference,
	"paramchoice": layerInference,

	"budget":          layerDocument,
	"nodeview":        layerDocument,
	"doctree":         layerDocument,
	"sourcetreestats": layerDocument,
	"scan":            layerDocument,
	"provenance":      layerDocument,
	"value":           layerDocument,
	"jsonpath":        layerDocument,
	"overlay":         layerDocument,

	"fragment":  layerGrammar,
	"specrules": layerGrammar,
	"reftable":  layerGrammar,
	"dialect":   layerGrammar,

	"reftable/typed":  layerSchema,
	"annotation":      layerSchema,
	"componentlookup": layerSchema,
	"naming":          layerSchema,
	"typeformat":      layerSchema,
	"merge":           layerSchema,
	"lowering":        layerSchema,
	"statement":       layerSchema,
	"schema":          layerSchema,

	"load":      layerService,
	"detect":    layerService,
	"service":   layerService,
	"auth":      layerService,
	"servers":   layerService,
	"operation": layerService,
	"document":  layerService,
}

// layerLibraries is what each layer may import from outside the module, as
// allowlist entries. A layer with no entry, service, may import any library;
// one with a nil entry, inference, none.
var layerLibraries = map[layer][]string{
	layerInference: nil,
	layerDocument:  {"go.yaml.in/yaml/v3"},
	layerGrammar:   {"go.yaml.in/yaml/v3", "github.com/speakeasy-api/openapi/references"},
	layerSchema:    {"go.yaml.in/yaml/v3", "github.com/speakeasy-api/openapi" + subtreeSuffix},
}

// unlayered names the production packages under compilerInternal that have no
// layer although no test-support list names them, each with the reason.
var unlayered = map[string]string{
	"canary": "pins the parser library's behaviour from tests; its one production file is the package doc",
}

// importSite is one import of a production file: the package keyed as layers
// keys it, the file relative to the repository root, and the import path.
type importSite struct{ pkg, file, path string }

// TestLayers_NoPackageImportsAboveItsLayer holds each package's real imports
// to its layer: none of a layer above, and no library its layer may not reach.
// It reads the files, not the rules entries, so a subtree entry cannot hide
// what a package imports.
func TestLayers_NoPackageImportsAboveItsLayer(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)

	sites, err := layerImports(root, slices.Sorted(maps.Keys(layers)))
	require.NoError(t, err)
	require.NotEmpty(t, sites, "the walk read no import, so it checked nothing")

	for _, v := range layerViolations(sites, layers, layerLibraries) {
		t.Error(v)
	}
}

// TestLayers_EveryCompilerPackageHasOne holds layers and the compiler's
// production packages to each other: a package without a row fails, and so
// does a row, or an unlayered entry, naming no package.
func TestLayers_EveryCompilerPackageHasOne(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)

	all := productionPackages(t, root)
	for pkg := range unlayered {
		assert.Contains(t, all, path.Join(compilerInternal, pkg), "unlayered names %s, which holds no production Go files", pkg)
		assert.NotContains(t, layers, pkg, "%s is both layered and unlayered; pick one", pkg)
	}
	missing, stale := layerCoverage(compilerPackages(all), layers)

	for _, pkg := range missing {
		t.Errorf("%s has no row in layers: add it to the layer its imports and its role say", pkg)
	}
	for _, pkg := range stale {
		t.Errorf("layers names %s, which is no package of the compiler: drop the row", pkg)
	}
}

// TestLayerViolations_NamesEachCrossing plants one file importing one path in
// one package, under the live tables, and holds the report to exactly the
// crossing it names, or to none for an import its layer may make.
func TestLayerViolations_NamesEachCrossing(t *testing.T) {
	t.Parallel()
	const f = compilerInternal + "/%s/plant.go"
	internal := func(pkg string) string { return module + "/" + compilerInternal + "/" + pkg }
	tests := []struct {
		name, pkg, imp, want string
	}{
		{"inference reaching document", "pagination", internal("nodeview"),
			fmt.Sprintf(f+": pagination (inference) imports nodeview (document), a layer above its own", "pagination")},
		{"document reaching grammar", "nodeview", internal("reftable"),
			fmt.Sprintf(f+": nodeview (document) imports reftable (grammar), a layer above its own", "nodeview")},
		{"grammar reaching schema", "specrules", internal("reftable/typed"),
			fmt.Sprintf(f+": specrules (grammar) imports reftable/typed (schema), a layer above its own", "specrules")},
		{"schema reaching service", "lowering", internal("load"),
			fmt.Sprintf(f+": lowering (schema) imports load (service), a layer above its own", "lowering")},
		{"schema reaching a package of no layer", "schema", internal("openapitest"),
			fmt.Sprintf(f+": schema (schema) imports openapitest, which is in no layer", "schema")},
		{"inference reaching a library", "convention", "go.yaml.in/yaml/v3",
			fmt.Sprintf(f+": convention (inference) imports \"go.yaml.in/yaml/v3\", a library its layer may not reach", "convention")},
		{"document reaching the parser library", "nodeview", "github.com/speakeasy-api/openapi/yml",
			fmt.Sprintf(f+": nodeview (document) imports \"github.com/speakeasy-api/openapi/yml\", "+
				"a library its layer may not reach", "nodeview")},
		{"grammar reaching the parser's schema model", "reftable", "github.com/speakeasy-api/openapi/jsonschema/oas3",
			fmt.Sprintf(f+": reftable (grammar) imports \"github.com/speakeasy-api/openapi/jsonschema/oas3\", "+
				"a library its layer may not reach", "reftable")},

		{"a package of its own layer", "reftable", internal("fragment"), ""},
		{"a layer below", "operation", internal("nodeview"), ""},
		{"the YAML library from the document layer", "nodeview", "go.yaml.in/yaml/v3", ""},
		{"the parser library from the schema layer", "componentlookup", "github.com/speakeasy-api/openapi/openapi", ""},
		{"any library from the service layer", "load", "example.com/any/library", ""},
		{"the module's ir from the lowest layer", "diag", module + "/ir", ""},
		{"the standard library", "diag", "net/http", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeImporter(t, filepath.Join(root, compilerInternal, tc.pkg, "plant.go"), tc.imp)

			sites, err := layerImports(root, []string{tc.pkg})
			require.NoError(t, err)
			got := layerViolations(sites, layers, layerLibraries)

			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{tc.want}, got)
		})
	}
}

// TestLayerImports_ReadsProductionFilesOfOnePackage plants an import in a test
// file and another in a package beside the one asked for, and expects neither
// read: a test may import across layers, and a package is read by its own
// files alone.
func TestLayerImports_ReadsProductionFilesOfOnePackage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := filepath.Join(root, compilerInternal)
	writeImporter(t, filepath.Join(base, "pagination", "plant_test.go"), module+"/"+compilerInternal+"/load")
	writeImporter(t, filepath.Join(base, "reftable", "typed", "plant.go"), module+"/"+compilerInternal+"/load")
	writeImporter(t, filepath.Join(base, "reftable", "plant.go"), module+"/"+compilerInternal+"/fragment")

	sites, err := layerImports(root, []string{"reftable", "pagination", "nowhere"})

	require.NoError(t, err)
	assert.Equal(t, []importSite{{"reftable", compilerInternal + "/reftable/plant.go", module + "/" + compilerInternal + "/fragment"}}, sites)
}

// TestLayerCoverage_NamesAMissingAndAStaleRow plants a tree of two packages
// against a table of one of them and one that does not exist, beside a
// test-support package and the unlayered canary, which are no one's omission.
func TestLayerCoverage_NamesAMissingAndAStaleRow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, pkg := range []string{"kept", "added", "canary", "service/servicetest"} {
		writeImporter(t, filepath.Join(root, compilerInternal, pkg, "plant.go"), "example.com/x")
	}
	writeImporter(t, filepath.Join(root, "compilers", "openapi", "face.go"), "example.com/x")

	missing, stale := layerCoverage(compilerPackages(productionPackages(t, root)),
		map[string]layer{"kept": layerSchema, "removed": layerSchema})

	assert.Equal(t, []string{"added"}, missing)
	assert.Equal(t, []string{"removed"}, stale)
}

// layerImports returns the non-stdlib imports of each production file of the
// named packages beneath root's compilerInternal, in the order of the names
// and then the files. A package whose directory is missing is skipped:
// TestLayers_EveryCompilerPackageHasOne reports a row naming one.
func layerImports(root string, pkgs []string) ([]importSite, error) {
	var sites []importSite
	for _, pkg := range pkgs {
		dir := filepath.Join(root, compilerInternal, filepath.FromSlash(pkg))
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !isProductionGoFile(e.Name()) {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				return nil, err
			}
			for _, imp := range f.Imports {
				ip := strings.Trim(imp.Path.Value, `"`)
				if first, _, _ := strings.Cut(ip, "/"); !strings.Contains(first, ".") {
					continue // stdlib: first path element has no dot
				}
				sites = append(sites, importSite{pkg, path.Join(compilerInternal, pkg, e.Name()), ip})
			}
		}
	}
	return sites, nil
}

// layerViolations returns one message per import that crosses a layer the wrong
// way, sorted: of a package with a layer above its own, of one with no layer,
// or of a library its layer may not reach. An import of a module package
// outside the compiler's internal tree is left to rules; so is a site of a
// package with no row, which layerCoverage reports.
func layerViolations(sites []importSite, table map[string]layer, libraries map[layer][]string) []string {
	var out []string
	for _, s := range sites {
		from, ok := table[s.pkg]
		if !ok {
			continue
		}
		if msg := crossing(s, from, table, libraries); msg != "" {
			out = append(out, s.file+": "+msg)
		}
	}
	slices.Sort(out)
	return out
}

// crossing explains why site s, in a package of layer from, is refused, or
// returns "" when it is not.
func crossing(s importSite, from layer, table map[string]layer, libraries map[layer][]string) string {
	if rest, ok := strings.CutPrefix(s.path, module+"/"+compilerInternal+"/"); ok {
		to, layered := table[rest]
		switch {
		case !layered:
			return fmt.Sprintf("%s (%s) imports %s, which is in no layer", s.pkg, from, rest)
		case to > from:
			return fmt.Sprintf("%s (%s) imports %s (%s), a layer above its own", s.pkg, from, rest, to)
		default:
			return ""
		}
	}
	if strings.HasPrefix(s.path, module+"/") {
		return ""
	}
	allowed, restricted := libraries[from]
	if !restricted || slices.ContainsFunc(allowed, func(entry string) bool { return entryAdmits(entry, s.path) }) {
		return ""
	}
	return fmt.Sprintf("%s (%s) imports %q, a library its layer may not reach", s.pkg, from, s.path)
}

// compilerPackages narrows the production directories of a tree, as
// productionPackages lists them, to those that belong in layers: the compiler's
// own packages, keyed beneath compilerInternal, less the test-support packages
// and the unlayered ones.
func compilerPackages(dirs []string) []string {
	var pkgs []string
	for _, dir := range dirs {
		pkg, ok := strings.CutPrefix(dir, compilerInternal+"/")
		if !ok {
			continue
		}
		_, forTests := testSupport[dir]
		_, apart := unlayered[pkg]
		if !forTests && !apart {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// layerCoverage returns the packages with no row in table, and the rows of
// table naming no package, each sorted.
func layerCoverage(pkgs []string, table map[string]layer) (missing, stale []string) {
	for _, pkg := range pkgs {
		if _, ok := table[pkg]; !ok {
			missing = append(missing, pkg)
		}
	}
	for pkg := range table {
		if !slices.Contains(pkgs, pkg) {
			stale = append(stale, pkg)
		}
	}
	slices.Sort(missing)
	slices.Sort(stale)
	return missing, stale
}
