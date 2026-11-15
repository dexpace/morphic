// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

const (
	orderProxied = "http://name.example/d.yaml"
	orderPrivate = "http://10.0.0.9/q.yaml"
)

// proxiedReader serves orderProxied as a reader sending it through a proxy
// does, its name resolving to resolved, or to nothing where that is unstated:
// an unbounded read is served stating neither space nor bound, and a bounded
// one is judged by resolved, stating it as the bound. Every other document is
// spacedReads' own, dialled directly.
type proxiedReader struct {
	resolved compilers.AddressSpace
	direct   *spacedReader

	mu   sync.Mutex
	reqs []compilers.AddressSpace
}

// ReadDocument implements compilers.DocumentReader.
func (r *proxiedReader) ReadDocument(ctx context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	if req.Location != orderProxied {
		got, err := r.direct.ReadDocument(ctx, req)
		got.Bound = got.Space
		return got, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req.Reach)
	read := compilers.DocumentRead{Data: []byte("D: {$ref: '" + reachPriv + "#/S'}\n")}
	switch {
	case req.Reach == compilers.SpaceUnstated:
		return read, nil
	case r.resolved == compilers.SpaceUnstated:
		return compilers.DocumentRead{}, compilers.ErrUnboundedRead
	case r.resolved > req.Reach:
		return compilers.DocumentRead{}, &compilers.AddressSpaceError{Address: "name.example", Space: r.resolved}
	}
	read.Bound = r.resolved
	return read, nil
}

// orderAskers are the documents asking for orderProxied, by role: the source,
// a file; a document read from a public address; one read from a private one.
var orderAskers = map[string]string{"file": "spec.yaml", "public": reachPub, "private": orderPrivate}

// orderSequences are every order of two and of three askers.
func orderSequences() [][]string {
	roles := []string{"file", "public", "private"}
	var out [][]string
	for _, a := range roles {
		for _, b := range roles {
			if b == a {
				continue
			}
			out = append(out, []string{a, b})
			for _, c := range roles {
				if c != a && c != b {
					out = append(out, []string{a, b, c})
				}
			}
		}
	}
	return out
}

// TestExternal_WhoReadsAProxiedDocumentTurnsOnNoOrderOfAsking pins that a
// document read through a proxy, its name resolving to a public address, a
// private one or none, answers each asker as that asker's own read would, in
// every order of two and three askers: a file always, a public document only
// where it resolves public, a private one where it resolves to any address.
// The document's own reference reaches as a public document's does in every
// order. Read first by the file, then by the public document, it was once
// served to both, whatever its name resolved to.
func TestExternal_WhoReadsAProxiedDocumentTurnsOnNoOrderOfAsking(t *testing.T) {
	t.Parallel()
	direct := spacedReads()
	direct.docs[orderPrivate] = spacedDoc{data: "Q: {type: string}\n", space: compilers.SpacePrivate}
	for resolved, admitted := range map[compilers.AddressSpace]map[string]bool{
		compilers.SpacePublic:   {"file": true, "public": true, "private": true},
		compilers.SpacePrivate:  {"file": true, "public": false, "private": true},
		compilers.SpaceUnstated: {"file": true, "public": false, "private": false},
	} {
		why := map[string]string{}
		for _, order := range orderSequences() {
			e := proxiedExternal(&proxiedReader{resolved: resolved, direct: direct})
			for _, asker := range []string{reachPub, orderPrivate} {
				_, err := e.document(t.Context(), asker, "spec.yaml")
				require.NoError(t, err)
			}
			for _, role := range order {
				_, err := e.document(t.Context(), orderProxied, orderAskers[role])
				assert.Equal(t, admitted[role], err == nil, "resolved %d, order %v: %s: %v", resolved, order, role, err)
				msg := ""
				if err != nil {
					msg = err.Error()
				}
				if seen, ok := why[role]; ok {
					assert.Equal(t, seen, msg, "resolved %d, order %v: %s is answered alike in every order", resolved, order, role)
				}
				why[role] = msg
			}
			assert.Equal(t, compilers.SpacePublic, e.reachOf(orderProxied), "resolved %d, order %v", resolved, order)
			_, err := e.document(t.Context(), reachPriv, orderProxied)
			barredWith(t, err, "it is at a private address, which a reference in a document read from a public one does not reach")
		}
	}
}

// TestExternal_AJudgementThatPassesBoundsTheDocumentForLaterAskers pins that
// a bounded asker's read of a document a wider read stated no bound for is a
// judgement only, made once per reach: a later asker its bound admits is
// answered unread.
func TestExternal_AJudgementThatPassesBoundsTheDocumentForLaterAskers(t *testing.T) {
	t.Parallel()
	direct := spacedReads()
	direct.docs[orderPrivate] = spacedDoc{data: "Q: {type: string}\n", space: compilers.SpacePrivate}
	r := &proxiedReader{resolved: compilers.SpacePublic, direct: direct}
	e := proxiedExternal(r)
	for _, asker := range []string{"spec.yaml", reachPub, reachPub, orderPrivate} {
		if asker != "spec.yaml" {
			_, err := e.document(t.Context(), asker, "spec.yaml")
			require.NoError(t, err)
		}
		_, err := e.document(t.Context(), orderProxied, asker)
		require.NoError(t, err, asker)
	}
	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated, compilers.SpacePublic}, r.reqs,
		"read for the file, judged once for the public document, and the private one admitted unread")
}

// proxiedExternal reads through r beside a source at spec.yaml.
func proxiedExternal(r *proxiedReader) *external {
	src := sourceDocument{path: "spec.yaml", judged: judgment{checker: oas.NewChecker()}}
	return newExternal(Options{Reader: r}, newExternalReads(src, budget.Unbounded()), &aside{})
}
