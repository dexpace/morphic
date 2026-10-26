package load

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// phasedSpec is a source every phase does work on: a path item $ref with a
// field beside it, schema $refs, and a component the overlay grafts.
const phasedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/components/pathItems/A'
    summary: beside the $ref
components:
  pathItems:
    A:
      get:
        responses:
          '200':
            description: ok
            content:
              application/json:
                schema: {$ref: '#/components/schemas/Pet'}
  schemas:
    Pet:
      type: object
      properties:
        owner: {$ref: '#/components/schemas/Owner'}
    Owner: {type: object}
`

// phasedOptions are the options phasedSpec loads under, an overlay among them,
// telling entered of each phase the load enters.
func phasedOptions(entered func(phase)) Options {
	return Options{
		Overlay: &overlay.Options{Path: "patch.yaml", Data: []byte(describingOverlay)}, OverlaySrcIndex: 1,
		entered: entered,
	}
}

// allPhases is every phase a load enters, in order: the parse, then build's.
func allPhases() []phase {
	all := make([]phase, 0, len(steps())+1)
	all = append(all, phaseParse)
	for _, s := range steps() {
		all = append(all, s.phase)
	}
	return all
}

// TestLoad_EntersEveryPhaseInOrder pins the phases a load that lowers goes
// through, each entered once and in the order build lists them, which is the
// list the cancellation test below cancels at.
func TestLoad_EntersEveryPhaseInOrder(t *testing.T) {
	t.Parallel()
	var entered []phase

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(phasedSpec),
		phasedOptions(func(p phase) { entered = append(entered, p) }))

	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	assert.Equal(t, allPhases(), entered)
}

// TestLoad_ACancelAtAPhaseEntersNoLaterOne pins the check before each phase: a
// cancel landing as a phase is entered ends the load with the context's error
// and no document before that phase does any work, so no later phase is
// entered and the load does next to nothing more: the allocations it makes
// after the cancel are counted, no clock read, and held to a hundredth of a
// whole load's.
//
// It runs alone, since the allocation counts are the process's.
func TestLoad_ACancelAtAPhaseEntersNoLaterOne(t *testing.T) {
	whole := mallocs()
	_, _, err := Load(t.Context(), 0, openapitest.SourceOf(phasedSpec), phasedOptions(nil))
	require.NoError(t, err)
	whole = mallocs() - whole

	for _, at := range allPhases() {
		t.Run(string(at), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			var entered []phase
			var cancelledAt uint64

			doc, diags, err := Load(ctx, 0, openapitest.SourceOf(phasedSpec), phasedOptions(func(p phase) {
				entered = append(entered, p)
				if p == at {
					cancel()
					cancelledAt = mallocs()
				}
			}))
			after := mallocs() - cancelledAt

			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, doc)
			assert.Empty(t, diags)
			require.NotEmpty(t, entered)
			assert.Equal(t, at, entered[len(entered)-1], "no phase after the cancel was entered")
			assert.LessOrEqual(t, after, whole/100,
				"the load stopped where the cancel landed: %d allocations after it, of %d for a whole load", after, whole)
		})
	}
}

// mallocs is how many heap objects the process has allocated so far.
func mallocs() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Mallocs
}

// widePhasedSpec is phasedSpec grown by n paths, each with its schema, so
// every phase does work proportional to n and the checks inside its loops
// have many items to fall between.
func widePhasedSpec(n int) string {
	var b strings.Builder
	b.WriteString(strings.Replace(phasedSpec, "paths:\n", "paths:\n"+widePaths(n), 1))
	for i := range n {
		fmt.Fprintf(&b, "    S%d:\n      type: object\n      properties:\n        id: {type: string}\n"+
			"        next: {$ref: '#/components/schemas/S%d'}\n", i, (i+1)%n)
	}
	return b.String()
}

// widePaths writes n paths, each answering with its schema.
func widePaths(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "  /p%d:\n    get:\n      parameters: [{name: q, in: query, schema: {type: string}}]\n"+
			"      responses:\n        '200':\n          description: ok\n          content:\n"+
			"            application/json:\n              schema: {$ref: '#/components/schemas/S%d'}\n", i, i)
	}
	return b.String()
}

// splitSpec writes a source split across files in dir: n path items mounted
// by $ref from another document, each with a field beside its $ref, and each
// answering with a response in a third, so the load models fields beside
// $refs and resolves references written in other documents. It returns the
// source.
func splitSpec(t *testing.T, dir string, n int) compilers.Source {
	t.Helper()
	var root, items, responses strings.Builder
	root.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n")
	for i := range n {
		fmt.Fprintf(&root, "  /p%d:\n    $ref: 'items.yaml#/P%d'\n    summary: beside the $ref\n", i, i)
		fmt.Fprintf(&items, "P%d:\n  get:\n    responses:\n      '200': {$ref: 'responses.yaml#/R%d'}\n", i, i)
		fmt.Fprintf(&responses, "R%d: {description: ok}\n", i)
	}
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}
	write("items.yaml", items.String())
	write("responses.yaml", responses.String())
	return compilers.Source{Path: write("root.yaml", root.String()), Data: []byte(root.String())}
}

// asksAfterRefusal is the most a load asks its context once it has refused:
// once for each stage the busiest phase runs in sequence, each leaving at its
// first ask. It is a constant, where a loop asking on for every item it has
// left would ask as often as the document has items.
const asksAfterRefusal = 8

// TestLoad_ACancelAtAnyCheckEndsTheLoadCleanly cancels a load at every check
// of its context a whole load makes, one load per check, and requires the
// same end each time: the context's error, no document and no finding, with
// no more than asksAfterRefusal asks after the refusal. A phase cut short
// anywhere, a refusal it was partway to included, is never reported or built
// on, and no loop goes on once refused. The sources make every phase work: an
// overlay, fields beside $refs, and references written in other documents,
// more of each than asksAfterRefusal.
func TestLoad_ACancelAtAnyCheckEndsTheLoadCleanly(t *testing.T) {
	t.Parallel()
	for name, load := range map[string]func(ctx context.Context) (*Document, []ir.Diagnostic, error){
		"one document": func(ctx context.Context) (*Document, []ir.Diagnostic, error) {
			return Load(ctx, 0, openapitest.SourceOf(phasedSpec), phasedOptions(nil))
		},
		"split across documents": func(ctx context.Context) (*Document, []ir.Diagnostic, error) {
			dir := t.TempDir()
			return Load(ctx, 0, splitSpec(t, dir, 12), Options{Reader: reader.Disk{}, Within: dir})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			whole := liveFor(t, 1<<30)
			doc, diags, err := load(whole)
			require.NoError(t, err)
			require.NotNil(t, doc, "%+v", diags)
			require.Greater(t, whole.Calls, len(allPhases()), "the loops ask too, not only the phases")

			for left := range whole.Calls {
				ctx := liveFor(t, left)

				doc, diags, err := load(ctx)

				require.ErrorIs(t, err, context.Canceled, "cancelled at check %d of %d", left+1, whole.Calls)
				require.NotErrorIs(t, err, ErrParse, "check %d: a cancel is no source that will not parse", left+1)
				require.Nil(t, doc, "check %d", left+1)
				require.Empty(t, diags, "check %d", left+1)
				require.LessOrEqual(t, ctx.Calls, left+1+asksAfterRefusal, "check %d: asked on after the refusal",
					left+1)
			}
		})
	}
}

// recorder is a context that records, at each check of it, the allocations
// the process has made so far, and is never done.
type recorder struct {
	context.Context
	at []uint64
}

func (r *recorder) Err() error {
	r.at = append(r.at, mallocs())
	return nil
}

// TestLoad_ChecksTheContextThroughoutAPhase pins how much a load does between
// two checks of its context, which is what a cancel landing between them
// waits for. Counted in allocations, never read off a clock, every stretch is
// held to a twentieth of a whole load's, but one: the parser's model build,
// which reads no context and is one call (docs/parser-library.md#context-unread).
//
// It runs alone, since the allocation counts are the process's.
func TestLoad_ChecksTheContextThroughoutAPhase(t *testing.T) {
	src := openapitest.SourceOf(widePhasedSpec(300))
	rec := &recorder{Context: t.Context()}
	after := -1
	opts := phasedOptions(func(p phase) {
		if p == phaseSiblings {
			after = len(rec.at)
		}
	})

	start := mallocs()
	doc, diags, err := Load(rec, 0, src, opts)
	end := mallocs()
	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	require.Positive(t, after)

	whole := end - start
	stretches := append([]uint64{start}, rec.at...)
	stretches = append(stretches, end)
	for i := 1; i < len(stretches); i++ {
		// The check after the model phase is the one before the siblings
		// phase is entered, and the stretch ending there holds the model
		// build, after the phase's last walk of the tree.
		gap := stretches[i] - stretches[i-1]
		if i == after {
			assert.Greater(t, gap, whole/20, "the stretch left out is the model build, the long one")
			continue
		}
		assert.LessOrEqual(t, gap, whole/20, "%d allocations between checks %d and %d, of %d for the whole load",
			gap, i-1, i, whole)
	}
}
