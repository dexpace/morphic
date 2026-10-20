package lintrules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

// plantRoot is the tree the tests lay out like the module, and plantDir where
// it sits from this package's directory.
const (
	plantRoot = Module + "/internal/lintrules/testdata/mod"
	plantDir  = "testdata/mod"
)

// TestAnalyzers_DrawExactlyThePlantedFindings runs every rule, its scopes read
// against the planted tree, over each package in it and one beside it. A line
// ending in `// want "<analyzer>: <message>"` must draw that finding, and
// nothing else may draw one, so a rule that recognizes nothing fails, and so
// does one that refuses what it must leave alone.
func TestAnalyzers_DrawExactlyThePlantedFindings(t *testing.T) {
	t.Parallel()
	want := wantedFindings(t, plantDir)
	require.NotEmpty(t, want, "no // want markers under %s, so a clean result proves nothing", plantDir)

	got := drawnFindings(t, layout{root: plantRoot}.analyzers(), "./"+plantDir+"/...", "./testdata/outside")
	assert.Empty(t, cmp.Diff(want, got), "the findings drawn are not the marked ones (-want +got)")
}

// TestAnalyzers_ReadTheModulesLayout runs the rules as cmd/morphic-vet does,
// rooted at the module. There the planted compiler sits under internal/, which
// the registry rule holds and the ID rule, holding compilers only, does not.
func TestAnalyzers_ReadTheModulesLayout(t *testing.T) {
	t.Parallel()
	analyzers := Analyzers()
	require.NoError(t, analysis.Validate(analyzers))

	names := make([]string, 0, len(analyzers))
	for _, a := range analyzers {
		names = append(names, a.Name)
	}
	assert.Equal(t, []string{"registrywrites", "fragmentreadings", "namechannels", "idderivations"}, names)

	got := strings.Join(drawnFindings(t, analyzers, "./"+plantDir+"/compilers/graphql"), "\n")
	assert.Contains(t, got, "registrywrites: ")
	assert.NotContains(t, got, "idderivations: ")
}

// wantMarker is a line's marker: one or more quoted findings after "// want".
var wantMarker = regexp.MustCompile(`// want ((?:"(?:[^"\\]|\\.)*"\s*)+)$`)

// wantedFindings returns every marked finding under dir, as
// "<file>:<line>: <analyzer>: <message>" with file relative to dir, sorted.
func wantedFindings(t *testing.T, dir string) []string {
	t.Helper()
	var want []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			m := wantMarker.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			for _, q := range regexp.MustCompile(`"(?:[^"\\]|\\.)*"`).FindAllString(m[1], -1) {
				msg, err := strconv.Unquote(q)
				if err != nil {
					return fmt.Errorf("%s:%d: %w", rel, i+1, err)
				}
				want = append(want, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), i+1, msg))
			}
		}
		return nil
	})
	require.NoError(t, err)
	slices.Sort(want)
	return want
}

// drawnFindings loads each package the patterns name, with its tests, and runs
// the analyzers over it, returning each finding once in wantedFindings' form,
// sorted. Each package is loaded alone: go/types can race when one load checks
// several packages sharing an import (golang/go#81122). A plant has external
// tests only, whose package imports the one it tests, so the two are checked
// in turn, not at once.
func drawnFindings(t *testing.T, analyzers []*analysis.Analyzer, patterns ...string) []string {
	t.Helper()
	dir, err := filepath.Abs(plantDir)
	require.NoError(t, err)
	readDeclarers(t)

	seen := map[string]bool{}
	for _, path := range listPackages(t, patterns) {
		cfg := &packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
				packages.NeedTypesInfo | packages.NeedTypesSizes,
			Tests: true,
			Env:   append(os.Environ(), "GOPACKAGESDRIVER=off"),
		}
		pkgs, err := packages.Load(cfg, path)
		require.NoError(t, err)
		require.Zero(t, packages.PrintErrors(pkgs), "the plants must type-check, or they prove nothing")
		pkgs = slices.DeleteFunc(pkgs, func(p *packages.Package) bool { return strings.HasSuffix(p.ID, ".test") })

		graph, err := checker.Analyze(analyzers, pkgs, nil)
		require.NoError(t, err)
		for _, act := range graph.Roots {
			require.NoError(t, act.Err, "%s on %s", act.Analyzer.Name, act.Package.PkgPath)
			for _, d := range act.Diagnostics {
				pos := act.Package.Fset.Position(d.Pos)
				file, err := filepath.Rel(dir, pos.Filename)
				require.NoError(t, err)
				seen[fmt.Sprintf("%s:%d: %s: %s", filepath.ToSlash(file), pos.Line, act.Analyzer.Name, d.Message)] = true
			}
		}
	}
	got := make([]string, 0, len(seen))
	for f := range seen {
		got = append(got, f)
	}
	slices.Sort(got)
	return got
}

// readDeclarers reads the sources of the packages declaring what the rules key
// on. go list reads them in a process go test's cache does not see, and the
// cache keys a result on the files a test opens, so without this an edit to
// one, such as a renamed field the plants spell, replays a stale pass.
func readDeclarers(t *testing.T) {
	t.Helper()
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	pkgs, err := packages.Load(cfg, irPath, compilePath, referencesPath, "github.com/speakeasy-api/openapi/expression")
	require.NoError(t, err)
	require.Len(t, pkgs, 4)
	for _, p := range pkgs {
		require.NotEmpty(t, p.GoFiles, "%s lists no file to read", p.PkgPath)
		for _, f := range p.GoFiles {
			_, err := os.ReadFile(f)
			require.NoError(t, err)
		}
	}
}

// listPackages returns the import paths the patterns, relative to this
// package's directory, name, requiring each to name at least one. An import
// path pattern would match nothing: go list skips a testdata directory there.
func listPackages(t *testing.T, patterns []string) []string {
	t.Helper()
	var paths []string
	for _, pattern := range patterns {
		cfg := &packages.Config{Mode: packages.NeedName, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
		pkgs, err := packages.Load(cfg, pattern)
		require.NoError(t, err)
		require.NotEmpty(t, pkgs, "%s names no package", pattern)
		for _, p := range pkgs {
			paths = append(paths, p.PkgPath)
		}
	}
	return paths
}
