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
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
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
// private one, a local one or none, answers each asker as that asker's own
// read would, in its own words, in every order of two and three askers: a file
// always, a public document only where it resolves public, a private one where
// it resolves public or private. The document's own reference reaches as a
// public document's does in every order.
func TestExternal_WhoReadsAProxiedDocumentTurnsOnNoOrderOfAsking(t *testing.T) {
	t.Parallel()
	direct := spacedReads()
	direct.docs[orderPrivate] = spacedDoc{data: "Q: {type: string}\n", space: compilers.SpacePrivate}
	for resolved, admitted := range map[compilers.AddressSpace]map[string]bool{
		compilers.SpacePublic:   {"file": true, "public": true, "private": true},
		compilers.SpacePrivate:  {"file": true, "public": false, "private": true},
		compilers.SpaceLocal:    {"file": true, "public": false, "private": false},
		compilers.SpaceUnstated: {"file": true, "public": false, "private": false},
	} {
		why := map[string]string{}
		for _, order := range orderSequences() {
			external := proxiedExternal(&proxiedReader{resolved: resolved, direct: direct})
			for _, asker := range []string{reachPub, orderPrivate} {
				_, err := external.document(t.Context(), asker, "spec.yaml")
				require.NoError(t, err)
			}
			for _, role := range order {
				_, err := external.document(t.Context(), orderProxied, orderAskers[role])
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
			assert.Equal(t, compilers.SpacePublic, external.reachOf(orderProxied), "resolved %d, order %v", resolved, order)
			_, err := external.document(t.Context(), reachPriv, orderProxied)
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
	proxiedReaderValue := &proxiedReader{resolved: compilers.SpacePublic, direct: direct}
	external := proxiedExternal(proxiedReaderValue)
	for _, asker := range []string{"spec.yaml", reachPub, reachPub, orderPrivate} {
		if asker != "spec.yaml" {
			_, err := external.document(t.Context(), asker, "spec.yaml")
			require.NoError(t, err)
		}
		_, err := external.document(t.Context(), orderProxied, asker)
		require.NoError(t, err, asker)
	}
	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated, compilers.SpacePublic}, proxiedReaderValue.reqs,
		"read for the file, judged once for the public document, and the private one admitted unread")
}

// proxiedExternal reads through proxiedReaderValue beside a source at spec.yaml.
func proxiedExternal(proxiedReaderValue *proxiedReader) *external {
	src := sourceDocument{path: "spec.yaml", judged: judgment{checker: specrules.NewChecker()}}
	return newExternal(Options{Reader: proxiedReaderValue}, newExternalReads(src, budget.Unbounded()), &aside{})
}

// TestExternal_AJudgementIsKeptForItsReachAndItsBoundForEvery pins what a
// judgement leaves for later askers: a refusal kept for its reach, so a second
// asker there is refused with no read, and a bound that passed, by which an
// asker at any reach is judged with no read either.
func TestExternal_AJudgementIsKeptForItsReachAndItsBoundForEvery(t *testing.T) {
	t.Parallel()
	const pub2 = "https://pub2.example/b.yaml"
	direct := spacedReads()
	direct.docs[pub2] = spacedDoc{data: "Y: {type: string}\n", space: compilers.SpacePublic}
	proxiedReaderValue := &proxiedReader{resolved: compilers.SpacePrivate, direct: direct}
	external := proxiedExternal(proxiedReaderValue)
	for _, asker := range []string{reachPub, pub2} {
		_, err := external.document(t.Context(), asker, "spec.yaml")
		require.NoError(t, err)
	}
	_, err := external.document(t.Context(), orderProxied, "spec.yaml")
	require.NoError(t, err)
	for _, asker := range []string{reachPub, pub2} {
		_, err = external.document(t.Context(), orderProxied, asker)
		barredWith(t, err, "it is at a private address, which a reference in a document read from a public one")
	}
	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated, compilers.SpacePublic}, proxiedReaderValue.reqs,
		"read once, and judged once at the reach both askers share")

	passed := proxiedExternal(&proxiedReader{resolved: compilers.SpacePublic, direct: direct})
	_, err = passed.document(t.Context(), reachPub, "spec.yaml")
	require.NoError(t, err)
	_, err = passed.document(t.Context(), orderProxied, "spec.yaml")
	require.NoError(t, err)
	_, err = passed.document(t.Context(), orderProxied, reachPub)
	require.NoError(t, err)
	for _, reach := range []compilers.AddressSpace{compilers.SpacePublic, compilers.SpacePrivate} {
		err = passed.read.verdict(orderProxied, reach, func() (compilers.AddressSpace, error) {
			t.Errorf("reach %d is judged by the bound a judgement stated, with no read", reach)
			return compilers.SpaceUnstated, nil
		})
		assert.NoError(t, err, "reach %d", reach)
	}
}

// TestExternal_ADocumentWhoseReaderStatesNoSpaceIsRefusedToItsAsker pins that
// a fetched document's reference to one whose reader says nothing of where
// it read it is refused, read at the asker's own reach.
func TestExternal_ADocumentWhoseReaderStatesNoSpaceIsRefusedToItsAsker(t *testing.T) {
	t.Parallel()
	spacedReader := spacedReads()
	external := spacedExternal(spacedReader, "spec.yaml")
	_, err := external.document(t.Context(), reachPub, "spec.yaml")
	require.NoError(t, err)
	_, err = external.document(t.Context(), reachMute, reachPub)
	barredWith(t, err, "the reader did not say what address it was read from")
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, spacedReader.asked(reachMute))
}
