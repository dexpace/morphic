// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// probeServer is a test server that counts the requests it was asked and the
// connections it accepted, and the space the classifier places it in.
type probeServer struct {
	*httptest.Server
	requests, conns atomic.Int64
}

// newProbeServer starts a server answering handle, or "document" where it is
// nil.
func newProbeServer(t *testing.T, useTLS bool, handle http.HandlerFunc) *probeServer {
	t.Helper()
	p := &probeServer{}
	if handle == nil {
		handle = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("document")) }
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.requests.Add(1)
		handle(w, r)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			p.conns.Add(1)
		}
	}
	if useTLS {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	p.Server = srv
	return p
}

// port is the port the server listens on.
func (p *probeServer) port(t *testing.T) uint16 {
	t.Helper()
	u, err := url.Parse(p.URL)
	require.NoError(t, err)
	ap, err := netip.ParseAddrPort(u.Host)
	require.NoError(t, err)
	return ap.Port()
}

// placing is a classifier standing the given ports for the given spaces, as
// two ports of 127.0.0.1 cannot differ in space by themselves; any other
// address is public.
func placing(spaces map[uint16]compilers.AddressSpace) func(netip.AddrPort) compilers.AddressSpace {
	return func(ap netip.AddrPort) compilers.AddressSpace {
		if s, ok := spaces[ap.Port()]; ok {
			return s
		}
		return compilers.SpacePublic
	}
}

// guarded is a Disk over client whose guard classifies by placing. The field
// the classifier is handed in is the one seam this file assumes: classify, on
// Disk.
func guarded(client *http.Client, spaces map[uint16]compilers.AddressSpace) Disk {
	return Disk{Client: client, classify: placing(spaces)}
}

// TestDisk_ABoundedReadIsJudgedBeforeAnythingIsRequested pins the dial bound:
// a read whose reach is public to a server placed at a private address fails
// with ErrAddressSpace and the server is asked nothing; read as far as private,
// or with no bound, it returns the bytes and the space the address answered
// from, which an unbounded read states too.
func TestDisk_ABoundedReadIsJudgedBeforeAnythingIsRequested(t *testing.T) {
	t.Parallel()
	srv := newProbeServer(t, false, nil)
	d := guarded(nil, map[uint16]compilers.AddressSpace{srv.port(t): compilers.SpacePrivate})

	got, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL, Reach: compilers.SpacePublic})
	require.ErrorIs(t, err, compilers.ErrAddressSpace)
	past, ok := errors.AsType[*compilers.AddressSpaceError](err)
	require.True(t, ok, "the refusal names the address and its space")
	assert.Equal(t, compilers.SpacePrivate, past.Space)
	assert.NotContains(t, err.Error(), past.Address+": "+past.Address, "the dial names its address once")
	assert.Empty(t, got.Data)
	assert.Zero(t, srv.requests.Load(), "the address was refused before the request was sent")

	for _, reach := range []compilers.AddressSpace{compilers.SpacePrivate, compilers.SpaceUnstated} {
		got, err = d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL, Reach: reach})
		require.NoError(t, err, "reach %d", reach)
		assert.Equal(t, "document", string(got.Data), "reach %d", reach)
		assert.Equal(t, compilers.SpacePrivate, got.Space, "reach %d: the read states where it was answered from", reach)
		assert.Equal(t, got.Space, got.Bound, "reach %d: a direct dial bounds the read where it answered", reach)
	}
	assert.EqualValues(t, 2, srv.requests.Load())
}

// TestDisk_ARedirectIsADialJudgedAtItsOwnAddress pins that each hop is judged:
// a public server redirecting to a private one is refused at the second hop
// with the second server asked nothing, and a private one redirecting to a
// public one, read as far as private, answers from the address that answered.
func TestDisk_ARedirectIsADialJudgedAtItsOwnAddress(t *testing.T) {
	t.Parallel()
	var target atomic.Pointer[string]
	a := newProbeServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		if to := target.Load(); to != nil {
			http.Redirect(w, r, *to, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("document"))
	})
	b := newProbeServer(t, false, nil)
	spaces := map[uint16]compilers.AddressSpace{a.port(t): compilers.SpacePublic, b.port(t): compilers.SpacePrivate}

	target.Store(&b.URL)
	_, err := guarded(nil, spaces).ReadDocument(t.Context(),
		compilers.DocumentRequest{Location: a.URL, Reach: compilers.SpacePublic})
	require.ErrorIs(t, err, compilers.ErrAddressSpace)
	assert.EqualValues(t, 1, a.requests.Load(), "the first hop was dialled and asked")
	assert.Zero(t, b.requests.Load(), "the second was refused unasked")

	back := newProbeServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, a.URL, http.StatusFound)
	})
	target.Store(nil)
	spaces[back.port(t)] = compilers.SpacePrivate
	got, err := guarded(nil, spaces).ReadDocument(t.Context(),
		compilers.DocumentRequest{Location: back.URL, Reach: compilers.SpacePrivate})
	require.NoError(t, err)
	assert.Equal(t, compilers.SpacePublic, got.Space, "the address that answered, not the one asked first")
}

// TestDisk_NoConnectionIsKeptBetweenReads pins that the guard judges every
// read at its own dial: two bounded reads of one server open two connections,
// and a bounded read after an unbounded one of a private server is refused
// without riding the connection the first opened.
func TestDisk_NoConnectionIsKeptBetweenReads(t *testing.T) {
	t.Parallel()
	pub := newProbeServer(t, false, nil)
	d := guarded(nil, map[uint16]compilers.AddressSpace{pub.port(t): compilers.SpacePublic})
	for range 2 {
		_, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: pub.URL, Reach: compilers.SpacePublic})
		require.NoError(t, err)
	}
	assert.EqualValues(t, 2, pub.conns.Load(), "one connection per read")

	priv := newProbeServer(t, false, nil)
	d = guarded(nil, map[uint16]compilers.AddressSpace{priv.port(t): compilers.SpacePrivate})
	_, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: priv.URL})
	require.NoError(t, err)
	require.EqualValues(t, 1, priv.conns.Load())
	_, err = d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: priv.URL, Reach: compilers.SpacePublic})
	require.ErrorIs(t, err, compilers.ErrAddressSpace)
	assert.EqualValues(t, 1, priv.conns.Load(), "the refused read dialled nothing and rode nothing")
	assert.EqualValues(t, 1, priv.requests.Load(), "the server saw the first read only")
}

// TestSpaceOf_RanksAnAddressByHowPrivateItIs pins the classification, with an
// IPv4 address mapped or embedded in IPv6 ranked as the IPv4 address is.
func TestSpaceOf_RanksAnAddressByHowPrivateItIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		want  compilers.AddressSpace
		addrs []string
	}{
		{compilers.SpaceLocal, []string{"127.0.0.1", "::1", "0.0.0.0", "::", "0.1.2.3", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "::ffff:0.1.2.3"}},
		{compilers.SpacePrivate, []string{"10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1",
			"fe80::1", "fc00::1", "fec0::1", "224.0.0.1", "ff02::1", "255.255.255.255", "198.18.0.1", "192.0.2.1",
			"2001:db8::1", "::ffff:10.0.0.1", "::ffff:100.64.0.1", "::ffff:198.18.0.1", "64:ff9b::a00:1", "2002:a00:1::"}},
		{compilers.SpacePublic, []string{"8.8.8.8", "2606:4700::1", "172.32.0.1", "100.128.0.1", "64:ff9b::808:808"}},
	}
	for _, tc := range tests {
		for _, a := range tc.addrs {
			assert.Equal(t, tc.want, spaceOf(netip.MustParseAddr(a)), a)
		}
	}
}

// proxied is a Disk reading through a proxy that answers every request with
// "proxied" and counts them, resolving name.example to a public address.
func proxied(t *testing.T) (Disk, *probeServer) {
	t.Helper()
	proxy := newProbeServer(t, false, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("proxied")) })
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	t.Cleanup(tr.CloseIdleConnections)
	dns := &fakeDNS{answers: map[string][][]netip.Addr{"name.example": {{netip.MustParseAddr("93.184.216.34")}}}}
	return Disk{Client: &http.Client{Transport: tr}, resolver: dns.resolver()}, proxy
}

// TestDisk_AReadThroughAProxyIsJudgedByTheHostTheURLWrites pins that the
// dial being the proxy's, the host is judged, in every spelling of an address
// a URL parser takes, and a name by the addresses it resolves to. A refused
// host is never sent to the proxy.
func TestDisk_AReadThroughAProxyIsJudgedByTheHostTheURLWrites(t *testing.T) {
	t.Parallel()
	d, proxy := proxied(t)
	for _, refused := range []string{
		"http://10.0.0.5/x", "http://localhost/x", "http://LOCALHOST./x", "http://127.1/x", "http://0177.0.0.1/x",
		"http://2130706433/x", "http://0x7f.1/x", "http://[::ffff:10.0.0.1]/x", "http://a.localhost/x",
	} {
		_, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: refused, Reach: compilers.SpacePublic})
		assert.ErrorIs(t, err, compilers.ErrAddressSpace, refused)
	}
	assert.Zero(t, proxy.requests.Load(), "the proxy saw nothing")

	got, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "http://name.example/x", Reach: compilers.SpacePublic})
	require.NoError(t, err)
	assert.Equal(t, "proxied", string(got.Data))
	assert.Equal(t, compilers.SpaceUnstated, got.Space, "the address the proxy dialled is out of sight")
	assert.Equal(t, compilers.SpacePublic, got.Bound, "a name resolving to public addresses alone is bounded there")
}

// TestTextSpace_ReadsAHostAsTheGuardDoes pins the guard's reading of a
// host's text, which a proxied read and a document Memory holds at a URL are
// both ranked by: an address as that address, a number spelled as no
// canonical literal in any base, a host no DNS name spells and localhost as
// local, and a DNS name as saying nothing.
func TestTextSpace_ReadsAHostAsTheGuardDoes(t *testing.T) {
	t.Parallel()
	tests := map[string]compilers.AddressSpace{
		"10.0.0.5": compilers.SpacePrivate, "localhost": compilers.SpaceLocal, "LOCALHOST.": compilers.SpaceLocal,
		"a.localhost": compilers.SpaceLocal, "127.1": compilers.SpaceLocal, "0177.0.0.1": compilers.SpaceLocal,
		"2130706433": compilers.SpaceLocal, "0x7f.1": compilers.SpaceLocal, "::ffff:10.0.0.1": compilers.SpacePrivate,
		"1.1.1.1": compilers.SpacePublic, "1.1": compilers.SpaceLocal, "999.1": compilers.SpaceLocal,
		"a_b.example": compilers.SpaceLocal, "Name.Example.": compilers.SpaceUnstated, "a..b": compilers.SpaceLocal,
		"127.0.0.0xff": compilers.SpaceLocal, "1.2.3.4.0": compilers.SpaceLocal, "1.256.3.4": compilers.SpaceLocal,
		"0x": compilers.SpaceLocal, "ab.c-": compilers.SpaceLocal, strings.Repeat("a", 64) + ".example": compilers.SpaceLocal,
		strings.Repeat("a.", 127) + "aa": compilers.SpaceLocal,
	}
	for host, want := range tests {
		got, said := textSpace(host)
		assert.Equal(t, want, got, host)
		assert.Equal(t, want != compilers.SpaceUnstated, said, host)
	}
}

// roundTripperFunc is an http.RoundTripper that is a function.
type roundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestDisk_AReadThroughATransportItCannotHookFailsBeforeDialling pins that a
// caller's own dialer, of any of the four kinds, or a transport that is not an
// *http.Transport, is one the guard cannot judge: a bounded read through it
// fails with ErrUnboundedRead with nothing called and nothing dialled, and
// an unbounded read goes through it as it would, stating no space.
func TestDisk_AReadThroughATransportItCannotHookFailsBeforeDialling(t *testing.T) {
	t.Parallel()
	srv := newProbeServer(t, true, nil)
	base, ok := srv.Client().Transport.(*http.Transport)
	require.True(t, ok, "the test server's client trusts its certificate through an *http.Transport")
	pool := base.TLSClientConfig
	var calls atomic.Int64
	plain := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	secure := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls.Add(1)
		return (&tls.Dialer{Config: pool}).DialContext(ctx, network, addr)
	}
	secureNoContext := func(network, addr string) (net.Conn, error) { return secure(context.Background(), network, addr) }
	plainNoContext := func(network, addr string) (net.Conn, error) { return plain(context.Background(), network, addr) }

	// The deprecated Dial and DialTLS are set by name: a typed assignment
	// would draw the deprecation, and they are what the guard must refuse.
	withField := func(field string, fn any) *http.Transport {
		tr := &http.Transport{TLSClientConfig: pool}
		reflect.ValueOf(tr).Elem().FieldByName(field).Set(reflect.ValueOf(fn))
		return tr
	}
	transports := map[string]http.RoundTripper{
		"a round tripper": roundTripperFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("not to be called")
		}),
		"DialTLSContext": &http.Transport{TLSClientConfig: pool, DialTLSContext: secure},
		"DialContext":    &http.Transport{TLSClientConfig: pool, DialContext: plain},
		"DialTLS":        withField("DialTLS", secureNoContext),
		"Dial":           withField("Dial", plainNoContext),
	}
	for name, rt := range transports {
		d := guarded(&http.Client{Transport: rt}, map[uint16]compilers.AddressSpace{srv.port(t): compilers.SpacePrivate})
		calls.Store(0)
		conns := srv.conns.Load()

		_, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL, Reach: compilers.SpacePublic})
		require.ErrorIs(t, err, compilers.ErrUnboundedRead, name)
		assert.Zero(t, calls.Load(), "%s: the transport was not called", name)
		assert.Equal(t, conns, srv.conns.Load(), "%s: nothing was dialled", name)

		if name == "a round tripper" {
			continue
		}
		got, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL})
		require.NoError(t, err, name)
		assert.Equal(t, "document", string(got.Data), name)
		assert.Equal(t, compilers.SpaceUnstated, got.Space, "%s: an unbounded read through it states no space", name)
		assert.Positive(t, calls.Load(), "%s: the caller's own transport was used", name)
	}
}

// TestDisk_ADefaultTransportWithATLSDialerOfItsOwnIsNotHooked pins that a
// process-wide default transport carrying a TLS dialer of the process's own,
// which net/http dials https through in preference to the guard's, is one the
// guard cannot judge: a bounded read through the default client fails with
// ErrUnboundedRead, and the dialer and the server are not reached. It runs
// alone, as it swaps http.DefaultTransport.
func TestDisk_ADefaultTransportWithATLSDialerOfItsOwnIsNotHooked(t *testing.T) {
	srv := newProbeServer(t, true, nil)
	base, ok := srv.Client().Transport.(*http.Transport)
	require.True(t, ok, "the test server's client trusts its certificate through an *http.Transport")
	var calls atomic.Int64
	tr := &http.Transport{TLSClientConfig: base.TLSClientConfig,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			calls.Add(1)
			return (&tls.Dialer{Config: base.TLSClientConfig}).DialContext(ctx, network, addr)
		}}
	old := http.DefaultTransport
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = old })

	d := guarded(nil, map[uint16]compilers.AddressSpace{srv.port(t): compilers.SpacePrivate})
	_, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL, Reach: compilers.SpacePublic})

	require.ErrorIs(t, err, compilers.ErrUnboundedRead)
	assert.Zero(t, calls.Load(), "the process's TLS dialer was not called")
	assert.Zero(t, srv.requests.Load(), "the server was asked nothing")
}

// TestDisk_AProxiedReadStatesNoSpaceItCannotSee pins what a read through a
// proxy states: a name's dialled address is out of sight, so no space, the
// rank of the document as a referrer; a bounded read the bound it judged the
// name at; an unbounded one, which asks the resolver nothing, no bound; and a
// literal address, which the proxy dials as written, its space as both.
func TestDisk_AProxiedReadStatesNoSpaceItCannotSee(t *testing.T) {
	t.Parallel()
	d, _ := proxied(t)
	d.resolver = (&fakeDNS{answers: map[string][][]netip.Addr{
		"private.example": {{netip.MustParseAddr("10.0.0.5")}},
	}}).resolver()
	read := func(location string, reach compilers.AddressSpace) compilers.DocumentRead {
		got, err := d.ReadDocument(t.Context(), compilers.DocumentRequest{Location: location, Reach: reach})
		require.NoError(t, err, location)
		return got
	}
	unasked := Disk{Client: d.Client, resolver: &net.Resolver{PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			t.Error("an unbounded read asked the resolver")
			return nil, errors.New("not to be asked")
		}}}
	got, err := unasked.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "http://private.example/x"})
	require.NoError(t, err)
	assert.Equal(t, compilers.DocumentRead{Data: []byte("proxied")}, got, "a name read unbounded states nothing")

	got = read("http://private.example/x", compilers.SpacePrivate)
	assert.Equal(t, compilers.SpaceUnstated, got.Space)
	assert.Equal(t, compilers.SpacePrivate, got.Bound)
	for _, reach := range []compilers.AddressSpace{compilers.SpaceUnstated, compilers.SpacePrivate} {
		got = read("http://10.0.0.5/x", reach)
		assert.Equal(t, compilers.SpacePrivate, got.Space, "a literal is dialled as written")
		assert.Equal(t, compilers.SpacePrivate, got.Bound)
	}
}
