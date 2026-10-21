package canary_test

// The binding oracle (GitHub #914): the compiler binds schema $refs itself
// (compilers/openapi/internal/bind), and this holds what it binds to what the
// parser library's resolver, which the compiler never asks, resolves the same
// $ref to. The two agree except where the library is wrong, or where the
// compiler reads a reference by a rule of its own; each such disagreement is
// listed in oracleDisagreements with the reason, and one the list does not
// hold fails.
//
// The library runs in children, since it overflows its stack on a loop of
// $refs (docs/parser-library.md#schema-cycle-overflow). A child that dies at a
// $ref is run again past it, so one loop costs the rest of the document
// nothing.

import (
	"bufio"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// update rewrites oracleDisagreements from what the oracle finds now,
// keeping each reason a disagreement still has and marking a new one
// unclassified, which fails until a reason is written for it.
var update = flag.Bool("update", false, "rewrite "+oracleDisagreements+" from what the oracle finds")

const (
	// oracleDisagreements lists every disagreement and its reason.
	oracleDisagreements = "testdata/binding_disagreements.txt"
	// oracleSuite holds the JSON Schema Test Suite's draft 2020-12 files
	// whose groups write a $ref, vendored with the suite's licence.
	oracleSuite = "testdata/json-schema-test-suite/tests/draft2020-12"
	// oracleSkipEnv tells a child where to start: a document and a site in
	// it, the one past where the last child died.
	oracleSkipEnv = "MORPHIC_CANARY_ORACLE_SKIP"
	// oracleShards is how many children run at once, each over its share of
	// the documents.
	oracleShards = 8
	// oracleTimeout bounds one child's run over its share.
	oracleTimeout = 2 * time.Minute
	// unclassified is the reason update gives a disagreement it has not seen.
	unclassified = "unclassified"
)

// What the oracle's child prints, each opening a line of its own: the index
// of the document it starts, each $ref site of that document in walk order,
// and the answer for a site once the library has resolved it.
const (
	docMark    = "oracle-doc: "
	siteMark   = "oracle-site: "
	answerMark = "oracle-answer: "
)

// The answers the oracle compares, beyond "resolved L:C", the position of the
// node a chain ends on.
const (
	answerUnresolved = "unresolved"
	answerCyclic     = "cyclic"
	answerOverBudget = "over-budget"
	answerMissing    = "missing"
)

// oracleReasons are the reasons a disagreement may give, each saying which
// side reads the reference otherwise, and what decides it.
var oracleReasons = map[string]string{
	"loop": "the library overflows its stack on a loop of $refs, reports a $ref into one unresolved, or " +
		"answers it with nothing and no error (docs/parser-library.md#schema-cycle-overflow); the compiler " +
		"reports the loop",
	"defs-rule": `the library finds a "#/$defs/..." definition by a rule of its own, from the schema a ` +
		"resolution before carried it to, and not from the ancestors of the position it is written at " +
		"(docs/parser-library.md#defs-order); the compiler reads it by the $defs rule " +
		"(docs/compilers/openapi.md#references)",
	"root-defs": "once a chain has reached a schema whose own $defs holds the key, the library reads that, " +
		"not the root's (docs/parser-library.md#root-defs)",
	"percent-defs": `the library looks a "#/$defs/..." pointer up before percent-decoding it, so an escape ` +
		"in it names no definition; RFC 6901 §6 decodes the fragment first",
	"second-hash": "the library reads a fragment holding a second '#' up to it; RFC 3986 §3.5 admits no " +
		"'#' in a fragment, and the compiler reads none",
	"plus":        "the library decodes a '+' in a fragment as a space, which RFC 3986 §2.1 does not (GitHub #911)",
	"empty-token": "the library reads an empty reference token, or a pointer ending in '/', otherwise than RFC 6901 does",
	"through-reference": "the library resolves no schema $ref whose pointer passes a reference object, though " +
		"it reads past one for a reference object's own $ref; the compiler reads both alike " +
		"(docs/compilers/openapi.md#references)",
	"anchor-id": "the compiler reads a plain-name fragment as the $anchor or $dynamicAnchor the resource it " +
		"names declares, and a URI an $id declares as that schema, every schema under no $id being in the " +
		"document's resource, as OAS 3.1 reads one (JSON Schema 2020-12 §8.2, docs/compilers/openapi.md#references); " +
		"the library looks for an anchor only inside the top-level schema it resolves, whatever URI names it, " +
		"reads no $dynamicAnchor as one, and reads a URI an $id declares as a document to fetch",
	"id-base": "the compiler reads a $ref under an $id against the base the $id gives: a fragment's pointer from " +
		"the root of the resource it names, with no fallback, and a relative URI as an identifier, never a " +
		"document to fetch (JSON Schema 2020-12 §8.2.1); under none, a fragment is read in the document, so \"#\" " +
		"names its root; the library reads a fragment's pointer from another root: the document's, the " +
		"top-level schema's, or one its $defs rule finds",
	"duplicate-identifier": "two or more schemas declare the $anchor, $dynamicAnchor or $id the reference " +
		"names, which JSON Schema 2020-12 §8.2 forbids, so the compiler reads it as naming none of them; the " +
		"library takes one",
	"alias-position": "the compiler reads a YAML alias where it is written, as the document's JSON places it; " +
		"the library, where its anchor is",
	"duplicate-key": "the compiler reads a key written twice in a mapping as its last occurrence wherever the " +
		"mapping is, as the parser's model reads one; the library's read of raw YAML takes the first " +
		"(docs/parser-library.md#duplicate-key)",
}

// oracleDoc is one document the oracle compiles, under a name naming where
// it came from.
type oracleDoc struct {
	name, spec string
}

// TestBindingOracle_AgreesWithTheLibrary is the oracle. For each $ref site
// the library's walk reaches in each document, it compares the chain end the
// compiler binds with the one the library resolves, and requires every
// disagreement to be listed with its reason, and every listing to be one.
func TestBindingOracle_AgreesWithTheLibrary(t *testing.T) {
	t.Parallel()
	docs := oracleDocs(t)
	binder := make([]map[jsontext.Pointer]string, len(docs))
	var compared []int
	for i, d := range docs {
		answers, loaded := binderAnswers(t, d.spec)
		if loaded {
			binder[i] = answers
			compared = append(compared, i)
		}
	}
	library := libraryAnswersOf(t, docs, compared)

	found := make([]disagreement, 0, len(compared))
	for _, i := range compared {
		found = append(found, disagreements(docs[i].name, binder[i], library[i])...)
	}
	listed := readDisagreements(t)
	if *update {
		writeDisagreements(t, found, listed)
		return
	}
	checkDisagreements(t, found, listed)
}

// TestBindingOracle_AnswersAlikeInEitherOrder holds the compiler's answers to
// the document rather than its order: every shape declared in both orders
// binds each site to the same end, by pointer, with the same status. None
// overflows, since this runs in the test's own process.
func TestBindingOracle_AnswersAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	for _, s := range oracleShapes() {
		if s.fixed {
			continue
		}
		forward, ok := binderEnds(t, s.document(false))
		require.True(t, ok, s.name)
		backward, ok := binderEnds(t, s.document(true))
		require.True(t, ok, s.name)
		assert.Equal(t, forward, backward, "%s: the order of the declarations decided an answer", s.name)
	}
}

// binderAnswers loads spec through the load phase and returns the answer the
// compiler binds each source $ref site to, and false when load refuses spec.
func binderAnswers(t *testing.T, spec string) (map[jsontext.Pointer]string, bool) {
	t.Helper()
	table, ok := loadedTable(t, spec)
	if !ok {
		return nil, false
	}
	view := nodeview.New()
	out := map[jsontext.Pointer]string{}
	for _, e := range table.Entries() {
		if e.Site.Doc != bind.Source {
			continue
		}
		out[e.Site.Pointer] = chainAnswer(e.Chain, func(at bind.Position) *yaml.Node {
			path, found := view.DocumentPath(table.Document(at.Doc).Root, at.Pointer)
			if !found {
				return nil
			}
			return path[len(path)-1]
		})
	}
	return out, true
}

// binderEnds is binderAnswers by the pointer each chain ends on, which no
// order of the declarations moves.
func binderEnds(t *testing.T, spec string) (map[jsontext.Pointer]string, bool) {
	t.Helper()
	table, ok := loadedTable(t, spec)
	if !ok {
		return nil, false
	}
	out := map[jsontext.Pointer]string{}
	for _, e := range table.Entries() {
		if e.Site.Doc == bind.Source {
			out[e.Site.Pointer] = fmt.Sprintf("%s %d#%s", e.Chain.Status, e.Chain.End.Doc, e.Chain.End.Pointer)
		}
	}
	return out, true
}

// loadedTable loads spec and returns its binding table, or false when load
// refuses spec.
func loadedTable(t *testing.T, spec string) (*bind.Table, bool) {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: location, Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	if doc == nil {
		return nil, false
	}
	table := doc.Bindings.Table()
	require.NotNil(t, table)
	return table, true
}

// chainAnswer is the answer a chain gives: the position of the node it ends
// on, read by nodeAt, or why it ends on none.
func chainAnswer(c bind.Chain, nodeAt func(bind.Position) *yaml.Node) string {
	switch c.Status {
	case bind.Resolved:
		return nodeAnswer(nodeAt(c.End))
	case bind.Cyclic:
		return answerCyclic
	case bind.OverBudget:
		return answerOverBudget
	default:
		return answerUnresolved
	}
}

// nodeAnswer is "resolved L:C", the line and column n is written at, an
// alias read as its anchor's node.
func nodeAnswer(n *yaml.Node) string {
	if n = nodeview.Deref(n); n == nil {
		return answerUnresolved
	}
	return fmt.Sprintf("resolved %d:%d", n.Line, n.Column)
}

// libraryAnswers is the oracle's child: over the JSON list of documents it
// is given, from where oracleSkipEnv says, it prints each document's index,
// its $ref sites in the library's walk order, then each site's answer as the
// library resolves them in that order, external references off.
func libraryAnswers(ctx context.Context, input []byte) string {
	var specs []string
	if err := json.Unmarshal(input, &specs); err != nil {
		return "input: " + err.Error()
	}
	startDoc, startSite := parseSkip(os.Getenv(oracleSkipEnv))
	for i := startDoc; i < len(specs); i++ {
		skip := 0
		if i == startDoc {
			skip = startSite
		}
		answerDocument(ctx, i, specs[i], skip)
	}
	return "done"
}

// answerDocument prints what libraryAnswers prints for one document, the
// first skip sites left unresolved.
func answerDocument(ctx context.Context, index int, spec string, skip int) {
	fmt.Printf("%s%d\n", docMark, index)
	doc, _, err := soa.Unmarshal(ctx, strings.NewReader(spec))
	if err != nil || doc == nil {
		return
	}
	sites, at := walkSites(ctx, doc)
	for _, s := range sites {
		fmt.Printf("%s%s\n", siteMark, s)
	}
	opts := references.ResolveOptions{TargetLocation: location, RootDocument: doc, DisableExternalRefs: true}
	for _, s := range sites[min(skip, len(sites)):] {
		answer, detail := libraryAnswer(ctx, at[s], opts)
		fmt.Printf("%s%s\t%s\t%q\n", answerMark, s, answer, detail)
	}
}

// walkSites returns the pointer of each schema the library's walk of doc
// reaches that writes a $ref, in walk order, each once, and the schema there.
func walkSites(ctx context.Context, doc *soa.OpenAPI) ([]jsontext.Pointer, map[jsontext.Pointer]*oas3.JSONSchema[oas3.Referenceable]) {
	var sites []jsontext.Pointer
	at := map[jsontext.Pointer]*oas3.JSONSchema[oas3.Referenceable]{}
	for item := range soa.Walk(ctx, doc) {
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{Schema: func(js *oas3.JSONSchema[oas3.Referenceable]) error {
			p := typed.PointerOf(item.Location)
			if _, seen := at[p]; js.IsReference() && !seen {
				sites, at[p] = append(sites, p), js
			}
			return nil
		}})
	}
	return sites, at
}

// libraryAnswer resolves js as the library does and answers as chainAnswer
// does: the node its chain ends on, a loop the library names as one, or none,
// with the error the library gave, if any.
func libraryAnswer(ctx context.Context, js *oas3.JSONSchema[oas3.Referenceable], opts references.ResolveOptions) (string, string) {
	if !js.IsResolved() {
		if _, err := js.Resolve(ctx, opts); err != nil {
			if strings.Contains(err.Error(), "circular reference") {
				return answerCyclic, err.Error()
			}
			return answerUnresolved, err.Error()
		}
	}
	end := js.GetResolvedSchema()
	if end == nil {
		return answerUnresolved, "no schema and no error"
	}
	return nodeAnswer(end.GetCore().GetRootNode()), ""
}

// parseSkip reads oracleSkipEnv's "document:site", or none.
func parseSkip(s string) (int, int) {
	d, site, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0
	}
	doc, errDoc := strconv.Atoi(d)
	at, errSite := strconv.Atoi(site)
	if errDoc != nil || errSite != nil {
		return 0, 0
	}
	return doc, at
}

// libraryAnswersOf runs the documents of docs at indices through children,
// oracleShards at once, and returns each one's answers by site, in the
// library's walk order.
func libraryAnswersOf(t *testing.T, docs []oracleDoc, indices []int) map[int][]siteAnswer {
	t.Helper()
	out := map[int][]siteAnswer{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for shard := range oracleShards {
		var mine []int
		for k := shard; k < len(indices); k += oracleShards {
			mine = append(mine, indices[k])
		}
		wg.Go(func() {
			got := runShard(t, docs, mine)
			mu.Lock()
			defer mu.Unlock()
			maps.Copy(out, got)
		})
	}
	wg.Wait()
	return out
}

// siteAnswer is what the library answered for one site, and the error it
// gave, if any.
type siteAnswer struct {
	site           jsontext.Pointer
	answer, detail string
}

// shardRun is what one shard's children have printed so far, by the shard's
// own index of each document.
type shardRun struct {
	sites   map[int][]jsontext.Pointer
	answers map[int]map[jsontext.Pointer]siteAnswer
	last    int
}

// runShard runs the documents at indices through one child after another,
// each starting past the site the last one died at, until one returns.
func runShard(t *testing.T, docs []oracleDoc, indices []int) map[int][]siteAnswer {
	t.Helper()
	specs := make([]string, len(indices))
	for k, i := range indices {
		specs[k] = docs[i].spec
	}
	input, err := json.Marshal(specs)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "shard.json")
	require.NoError(t, os.WriteFile(path, input, 0o600))

	run := shardRun{sites: map[int][]jsontext.Pointer{}, answers: map[int]map[jsontext.Pointer]siteAnswer{}, last: -1}
	skip := "0:0"
	for range maxChildren(specs) {
		r := runOracleChild(t, path, skip)
		run.read(r.output)
		if r.outcome == returned {
			break
		}
		next, ok := run.died(string(r.outcome))
		require.True(t, ok, "a child died before it reached a document:\n%.2000s", r.output)
		skip = next
	}
	out := map[int][]siteAnswer{}
	for k, i := range indices {
		for _, s := range run.sites[k] {
			out[i] = append(out[i], run.answers[k][s])
		}
	}
	return out
}

// maxChildren bounds the children one shard runs: one more than every site
// its documents could die at, counted as a site per line.
func maxChildren(specs []string) int {
	n := 1
	for _, s := range specs {
		n += strings.Count(s, "\n") + 1
	}
	return n
}

// runOracleChild runs the oracle's child over the documents at path, from
// skip, and reads how it ended.
func runOracleChild(t *testing.T, path, skip string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), oracleTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), childEnv+"=binding-oracle", inputEnv+"="+path, oracleSkipEnv+"="+skip)
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		require.NoError(t, ctx.Err(), "the child did not finish within %s", oracleTimeout)
		require.NoError(t, err, "the child did not start")
	}
	r, ok := classify(string(out), code, ctx.Err() != nil)
	require.True(t, ok, "the oracle's child ended in a way no outcome names (%v):\n%.2000s", err, out)
	return r
}

// read takes in what a child printed.
func (s *shardRun) read(out string) {
	lines := bufio.NewScanner(strings.NewReader(out))
	lines.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for lines.Scan() {
		line := lines.Text()
		switch {
		case strings.HasPrefix(line, docMark):
			s.last, _ = strconv.Atoi(strings.TrimPrefix(line, docMark))
			s.sites[s.last] = nil
			if s.answers[s.last] == nil {
				s.answers[s.last] = map[jsontext.Pointer]siteAnswer{}
			}
		case strings.HasPrefix(line, siteMark):
			s.sites[s.last] = append(s.sites[s.last], jsontext.Pointer(strings.TrimPrefix(line, siteMark)))
		case strings.HasPrefix(line, answerMark):
			fields := strings.SplitN(strings.TrimPrefix(line, answerMark), "\t", 3)
			a := siteAnswer{site: jsontext.Pointer(fields[0])}
			if len(fields) == 3 {
				a.answer = fields[1]
				a.detail, _ = strconv.Unquote(fields[2])
			}
			s.answers[s.last][a.site] = a
		}
	}
}

// died records how the child ended at the first site of the last document it
// started that it had no answer for, and returns where the next child starts.
// It is false when the child died before it started any document.
func (s *shardRun) died(how string) (string, bool) {
	if s.last < 0 {
		return "", false
	}
	for k, site := range s.sites[s.last] {
		if _, answered := s.answers[s.last][site]; !answered {
			s.answers[s.last][site] = siteAnswer{site: site, answer: how}
			return fmt.Sprintf("%d:%d", s.last, k+1), true
		}
	}
	return fmt.Sprintf("%d:0", s.last+1), true
}

// disagreement is a site the compiler and the library answer differently,
// with the error the library gave, which no listing records.
type disagreement struct {
	doc, site, binder, library, detail string
}

// key identifies a disagreement whatever its reason.
func (d disagreement) key() string {
	return strings.Join([]string{d.doc, d.site, d.binder, d.library}, "\t")
}

// disagreements returns each site of doc the two answer differently. A site
// only the compiler reads, in raw YAML the library's walk does not reach, is
// none. Answers are compared by kind, and two ends at different positions as
// "resolved" against "resolved elsewhere".
func disagreements(doc string, binder map[jsontext.Pointer]string, library []siteAnswer) []disagreement {
	var out []disagreement
	for _, a := range library {
		b, ok := binder[a.site]
		if !ok {
			b = answerMissing
		}
		if b == a.answer {
			continue
		}
		d := disagreement{doc: doc, site: string(a.site), binder: kindOf(b), library: kindOf(a.answer), detail: a.detail}
		if d.binder == d.library {
			d.library += " elsewhere"
		}
		out = append(out, d)
	}
	return out
}

// kindOf is an answer without the position it resolved at.
func kindOf(answer string) string {
	kind, _, _ := strings.Cut(answer, " ")
	return kind
}

// readDisagreements reads oracleDisagreements: one disagreement a line, its
// fields tab-separated, and the reason last.
func readDisagreements(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(oracleDisagreements)
	if errors.Is(err, fs.ErrNotExist) && *update {
		return map[string]string{}
	}
	require.NoError(t, err, "read %s (run with -update to create it)", oracleDisagreements)
	out := map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 5, "%s: %q is not a document, a site, two answers and a reason", oracleDisagreements, line)
		d := disagreement{doc: fields[0], site: fields[1], binder: fields[2], library: fields[3]}
		_, dup := out[d.key()]
		require.False(t, dup, "%s lists %q twice", oracleDisagreements, line)
		out[d.key()] = fields[4]
	}
	return out
}

// checkDisagreements requires every disagreement found to be listed, with a
// reason oracleReasons names, and every one listed to be found.
func checkDisagreements(t *testing.T, found []disagreement, listed map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	for _, d := range found {
		seen[d.key()] = true
		reason, ok := listed[d.key()]
		if !assert.True(t, ok, "a disagreement %s does not list: %s (the library: %q)", oracleDisagreements, d.key(), d.detail) {
			continue
		}
		assert.Contains(t, oracleReasons, reason, "%s gives a reason oracleReasons does not name: %s", d.key(), reason)
	}
	for key := range listed {
		assert.True(t, seen[key], "%s lists a disagreement the oracle no longer finds: %s", oracleDisagreements, key)
	}
}

// oracleHeader opens oracleDisagreements.
const oracleHeader = `# Generated by:
#   go test ./compilers/openapi/internal/canary -run TestBindingOracle_AgreesWithTheLibrary -update
#
# Each site where the schema $ref the compiler binds ends on another node, or
# fails otherwise, than the parser library's resolver: document, site, the
# compiler's answer, the library's, and the reason, a key of oracleReasons.
# Regenerating keeps each reason a disagreement still has and marks a new one
# unclassified, which fails until a reason is written for it.
`

// writeDisagreements writes oracleDisagreements from what was found, each
// keeping the reason listed for it.
func writeDisagreements(t *testing.T, found []disagreement, listed map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(oracleHeader)
	for _, d := range found {
		reason, ok := listed[d.key()]
		if !ok {
			reason = unclassified
		}
		b.WriteString(d.key() + "\t" + reason + "\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(oracleDisagreements), 0o755))
	require.NoError(t, os.WriteFile(oracleDisagreements, []byte(b.String()), 0o644))
}

// oracleDocs returns every document the oracle compiles: each shape in each
// order it is declared in, each Test Suite group writing a reference, each
// fixture of the repository's corpus, and the documents the grammar of
// reachdocs_test.go generates (GitHub #916).
func oracleDocs(t *testing.T) []oracleDoc {
	t.Helper()
	var docs []oracleDoc
	for _, s := range oracleShapes() {
		docs = append(docs, oracleDoc{name: "shape/" + s.name, spec: s.document(false)})
		if !s.fixed {
			docs = append(docs, oracleDoc{name: "shape/" + s.name + "/reversed", spec: s.document(true)})
		}
	}
	docs = append(docs, suiteDocs(t)...)
	docs = append(docs, fixtureDocs(t)...)
	for i, spec := range genReachFuzzDocs() {
		docs = append(docs, oracleDoc{name: fmt.Sprintf("generated/%03d", i), spec: spec})
	}
	return docs
}

// suiteDocs wraps each Test Suite group whose schema writes a reference as
// the component T of a document of its own.
func suiteDocs(t *testing.T) []oracleDoc {
	t.Helper()
	var docs []oracleDoc
	err := filepath.WalkDir(oracleSuite, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var groups []struct {
			Schema jsontext.Value `json:"schema"`
		}
		if err := json.Unmarshal(data, &groups, json.RejectUnknownMembers(false)); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		rel, err := filepath.Rel(oracleSuite, path)
		if err != nil {
			return err
		}
		for i, g := range groups {
			if !strings.Contains(string(g.Schema), `"$ref"`) {
				continue
			}
			docs = append(docs, oracleDoc{name: fmt.Sprintf("suite/%s/%d", filepath.ToSlash(rel), i),
				spec: head + "components:\n  schemas:\n    T: " + string(g.Schema.Clone()) + "\n"})
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, docs, "the suite's files are where %s says", oracleSuite)
	return docs
}

// fixtureRoots are the corpus directories whose specs the oracle reads.
var fixtureRoots = []string{"testdata/openapi", "testdata/conformance", "testdata/golden"}

// fixtureDocs returns each YAML spec of the repository's corpus.
func fixtureDocs(t *testing.T) []oracleDoc {
	t.Helper()
	var docs []oracleDoc
	for _, root := range fixtureRoots {
		dir := filepath.Join(repoRoot, root)
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			docs = append(docs, oracleDoc{name: "fixture/" + filepath.ToSlash(rel), spec: string(data)})
			return nil
		})
		require.NoError(t, err)
	}
	slices.SortFunc(docs, func(a, b oracleDoc) int { return strings.Compare(a.name, b.name) })
	require.NotEmpty(t, docs)
	return docs
}
