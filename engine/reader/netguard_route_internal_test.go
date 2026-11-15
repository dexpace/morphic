// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// TestDisk_AHopRedirectedPastTheProxyIsJudgedAtTheDial pins that the route a
// proxy function chooses is chosen per request: a first hop sent through a
// proxy that redirects to a host the function answers nil for (as a NO_PROXY
// match does) is dialled directly, and judged at that dial. Read to public
// addresses it is refused with the target asked nothing; read as far as
// loopback it is answered, so the redirect was followed.
func TestDisk_AHopRedirectedPastTheProxyIsJudgedAtTheDial(t *testing.T) {
	t.Parallel()
	target := newProbeServer(t, false, nil)
	proxy := newProbeServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	})
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	tr := &http.Transport{Proxy: func(req *http.Request) (*url.URL, error) {
		if req.URL.Hostname() == "name.example" {
			return proxyURL, nil
		}
		return nil, nil
	}}
	t.Cleanup(tr.CloseIdleConnections)
	dns := &fakeDNS{answers: map[string][][]netip.Addr{"name.example": {{netip.MustParseAddr("93.184.216.34")}}}}
	d := Disk{Client: &http.Client{Transport: tr}, resolver: dns.resolver()}

	_, err = d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "http://name.example/x", Reach: compilers.SpacePublic})
	require.ErrorIs(t, err, compilers.ErrAddressSpace)
	assert.EqualValues(t, 1, proxy.requests.Load(), "the first hop went through the proxy")
	assert.Zero(t, target.requests.Load(), "the second was refused at the dial, unasked")

	got, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "http://name.example/x", Reach: compilers.SpaceLocal})
	require.NoError(t, err)
	assert.Equal(t, "document", string(got.Data), "the redirect is followed where the reach allows it")
	assert.EqualValues(t, 1, target.requests.Load())
	assert.Equal(t, compilers.SpaceLocal, got.Space, "the last hop was dialled, so it states where it answered from")
}

// TestDisk_AProxiedHostWithATrailingDotIsRankedAsWithout pins that a host
// written with a trailing dot, which names the same host, states through a
// proxy what its dotless spelling does: a literal address its space, both as
// space and bound, bounded or not, and a name no space and, bounded, the
// bound its resolved addresses give.
func TestDisk_AProxiedHostWithATrailingDotIsRankedAsWithout(t *testing.T) {
	t.Parallel()
	d, proxy := proxied(t)
	tests := []struct {
		name, host     string
		reach          compilers.AddressSpace
		space, bound   compilers.AddressSpace
		wantAddressErr bool
	}{
		{name: "a name, unbounded", host: "name.example.", space: compilers.SpaceUnstated, bound: compilers.SpaceUnstated},
		{name: "a name, bounded", host: "name.example.", reach: compilers.SpacePublic,
			space: compilers.SpaceUnstated, bound: compilers.SpacePublic},
		{name: "a loopback literal, unbounded", host: "127.0.0.1.", space: compilers.SpaceLocal, bound: compilers.SpaceLocal},
		{name: "a loopback literal, bounded to it", host: "127.0.0.1.", reach: compilers.SpaceLocal,
			space: compilers.SpaceLocal, bound: compilers.SpaceLocal},
		{name: "a private literal, unbounded", host: "10.0.0.5.", space: compilers.SpacePrivate, bound: compilers.SpacePrivate},
		{name: "a private literal, bounded to it", host: "10.0.0.5.", reach: compilers.SpacePrivate,
			space: compilers.SpacePrivate, bound: compilers.SpacePrivate},
		{name: "a loopback literal, bounded past it", host: "127.0.0.1.", reach: compilers.SpacePublic, wantAddressErr: true},
		{name: "a public literal, bounded", host: "93.184.216.34.", reach: compilers.SpacePublic,
			space: compilers.SpacePublic, bound: compilers.SpacePublic},
	}
	for _, tc := range tests {
		got, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{
			Location: "http://" + tc.host + "/x", Reach: tc.reach,
		})
		if tc.wantAddressErr {
			assert.ErrorIs(t, err, compilers.ErrAddressSpace, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		assert.Equal(t, "proxied", string(got.Data), tc.name)
		assert.Equal(t, tc.space, got.Space, "%s: space", tc.name)
		assert.Equal(t, tc.bound, got.Bound, "%s: bound", tc.name)
	}
	assert.EqualValues(t, len(tests)-1, proxy.requests.Load(), "every read but the refused one reached the proxy")
}

// TestDisk_ADefaultTransportWithALegacyTLSDialerIsNotHooked pins that the
// process-wide default transport carrying a TLS dialer of the legacy
// DialTLS kind, which net/http dials https through in preference to the
// guard's, is one the guard cannot judge: a bounded read through the default
// client fails with ErrUnboundedRead before anything is dialled. It runs
// alone, as it swaps http.DefaultTransport.
func TestDisk_ADefaultTransportWithALegacyTLSDialerIsNotHooked(t *testing.T) {
	srv := newProbeServer(t, true, nil)
	base, ok := srv.Client().Transport.(*http.Transport)
	require.True(t, ok, "the test server's client trusts its certificate through an *http.Transport")
	var calls atomic.Int64
	// DialTLS is deprecated and set by name, as it is what the guard must refuse.
	legacy := func(network, addr string) (net.Conn, error) {
		calls.Add(1)
		return (&tls.Dialer{Config: base.TLSClientConfig}).DialContext(context.Background(), network, addr)
	}
	tr := &http.Transport{TLSClientConfig: base.TLSClientConfig}
	reflect.ValueOf(tr).Elem().FieldByName("DialTLS").Set(reflect.ValueOf(legacy))
	old := http.DefaultTransport
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = old })

	d := guarded(nil, map[uint16]compilers.AddressSpace{srv.port(t): compilers.SpacePrivate})
	_, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL, Reach: compilers.SpacePublic})

	require.ErrorIs(t, err, compilers.ErrUnboundedRead)
	assert.Zero(t, calls.Load(), "the process's TLS dialer was not called")
	assert.Zero(t, srv.requests.Load(), "the server was asked nothing")
}
