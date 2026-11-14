// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// fakeDNS answers each A query for a name from answers, the next address of
// the name's list each time it is asked, the last repeating; every other
// query is answered with no address. It is a resolver no test can leave the
// machine through.
type fakeDNS struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
}

// resolver is a resolver asking d.
func (d *fakeDNS) resolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go d.serve(server)
		return client, nil
	}}
}

// serve answers the one query conn carries, framed as DNS over a stream is,
// and closes conn. It ends when the query is answered or conn fails.
func (d *fakeDNS) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return
	}
	query := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(conn, query); err != nil || len(query) < 12 {
		return
	}
	reply := d.answer(query)
	_, _ = conn.Write(binary.BigEndian.AppendUint16(nil, uint16(len(reply))))
	_, _ = conn.Write(reply)
}

// answer is the reply to query.
func (d *fakeDNS) answer(query []byte) []byte {
	end := 12
	var labels []string
	for end < len(query) && query[end] != 0 {
		n := int(query[end])
		labels = append(labels, string(query[end+1:end+1+n]))
		end += 1 + n
	}
	question := query[12 : end+5]
	qtype := binary.BigEndian.Uint16(query[end+1:])
	reply := append([]byte{query[0], query[1], 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0}, question...)
	addr, ok := d.next(strings.Join(labels, "."), qtype == 1)
	switch {
	case !ok:
		reply[3] = 0x83 // no such name
		return reply
	case qtype != 1:
		return reply
	}
	reply[7] = 1
	ip := addr.As4()
	return append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4, ip[0], ip[1], ip[2], ip[3])
}

// next is the address name answers this time, taken from its list where
// take is set.
func (d *fakeDNS) next(name string, take bool) (netip.Addr, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, ok := d.answers[strings.ToLower(name)]
	if !ok || len(list) == 0 {
		return netip.Addr{}, false
	}
	if take && len(list) > 1 {
		d.answers[strings.ToLower(name)] = list[1:]
	}
	return list[0], true
}

// TestDisk_ANameIsJudgedByTheAddressEachDialResolvesItTo pins that a name is
// judged where it is dialled, at the address it resolves to then: a name
// resolving to a public address and then to a private one is read once and
// refused the second time, a public name answered with a private address is
// refused, and no numeric spelling of a loopback address, whatever the
// resolver makes of it, is read, with the server asked nothing.
func TestDisk_ANameIsJudgedByTheAddressEachDialResolvesItTo(t *testing.T) {
	t.Parallel()
	srv := newProbeServer(t, false, nil)
	port := srv.port(t)
	loop := netip.MustParseAddr("127.0.0.1")
	dns := &fakeDNS{answers: map[string][]netip.Addr{
		"rebind.example":   {loop, netip.MustParseAddr("127.0.0.2")},
		"intranet.example": {netip.MustParseAddr("10.0.0.5")},
		"0x7f.1":           {netip.MustParseAddr("127.0.0.3")}, "localhost": {netip.MustParseAddr("127.0.0.4")},
	}}
	served := netip.AddrPortFrom(loop, port)
	d := Disk{resolver: dns.resolver(), classify: func(ap netip.AddrPort) compilers.AddressSpace {
		if ap == served {
			return compilers.SpacePublic
		}
		return spaceOf(ap.Addr())
	}}
	read := func(host string) (compilers.DocumentRead, error) {
		return d.ReadDocument(t.Context(), compilers.DocumentRequest{
			Location: "http://" + net.JoinHostPort(host, srv.portString(t)) + "/x", Reach: compilers.SpacePublic,
		})
	}

	got, err := read("rebind.example")
	require.NoError(t, err)
	assert.Equal(t, compilers.SpacePublic, got.Space)
	_, err = read("rebind.example")
	require.ErrorIs(t, err, compilers.ErrAddressSpace, "the second dial resolves to a loopback address")
	for _, host := range []string{"intranet.example", "0x7f.1", "localhost."} {
		_, err := read(host)
		require.ErrorIs(t, err, compilers.ErrAddressSpace, host)
	}
	// Go's resolver reads a name of numeric labels alone as no domain name, and
	// a system resolver may read it as an address: either way nothing it names
	// is connected to unjudged.
	for _, host := range []string{"2130706433", "127.1", "0177.0.0.1"} {
		_, err := read(host)
		require.Error(t, err, host)
	}
	assert.EqualValues(t, 1, srv.requests.Load(), "the server was asked once, by the first read")
}

// portString is the server's port as a URL writes it.
func (p *probeServer) portString(t *testing.T) string {
	t.Helper()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(p.URL, "http://"))
	require.NoError(t, err)
	return port
}

// TestDisk_EverySpellingOfALocalAddressIsRefusedToAPublicRead pins the dial
// bound with no classifier but the reader's own: a loopback or unspecified
// address however the URL writes it, with credentials or on any port, is
// refused to a read bounded to public addresses before it connects.
func TestDisk_EverySpellingOfALocalAddressIsRefusedToAPublicRead(t *testing.T) {
	t.Parallel()
	srv := newProbeServer(t, false, nil)
	port := srv.portString(t)
	for _, host := range []string{
		"127.0.0.1:" + port, "user:secret@127.0.0.1:" + port, "0.0.0.0:" + port, "[::]:" + port,
		"[::ffff:127.0.0.1]:" + port, "[::1]:" + port,
	} {
		_, err := Disk{}.ReadDocument(t.Context(),
			compilers.DocumentRequest{Location: "http://" + host + "/x", Reach: compilers.SpacePublic})
		require.ErrorIs(t, err, compilers.ErrAddressSpace, host)
		var refused *compilers.AddressSpaceError
		require.ErrorAs(t, err, &refused, host)
		assert.Equal(t, compilers.SpaceLocal, refused.Space, host)
	}
	assert.Zero(t, srv.conns.Load(), "nothing was connected to")
}

// TestDisk_ARedirectChainIsJudgedAtEachHopAndCapped pins that a chain of
// redirects ending on a private address is refused at its last hop, which is
// asked nothing, and that a read follows at most MaxRedirects of them, after
// the caller's own policy.
func TestDisk_ARedirectChainIsJudgedAtEachHopAndCapped(t *testing.T) {
	t.Parallel()
	last := newProbeServer(t, false, nil)
	middle := newProbeServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, last.URL, http.StatusFound)
	})
	first := newProbeServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, middle.URL, http.StatusFound)
	})
	spaces := map[uint16]compilers.AddressSpace{last.port(t): compilers.SpacePrivate}
	_, err := guarded(nil, spaces).ReadDocument(t.Context(),
		compilers.DocumentRequest{Location: first.URL, Reach: compilers.SpacePublic})
	require.ErrorIs(t, err, compilers.ErrAddressSpace)
	assert.EqualValues(t, 1, middle.requests.Load())
	assert.Zero(t, last.requests.Load())

	var loop *probeServer
	loop = newProbeServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loop.URL, http.StatusFound)
	})
	_, err = guarded(nil, nil).ReadDocument(t.Context(), compilers.DocumentRequest{Location: loop.URL})
	require.ErrorContains(t, err, "stopped after 10 redirects")
	assert.EqualValues(t, MaxRedirects+1, loop.requests.Load())

	refuse := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	_, err = guarded(refuse, nil).ReadDocument(t.Context(), compilers.DocumentRequest{Location: first.URL})
	require.ErrorContains(t, err, "302 Found", "the caller's own policy still applies")
}

// TestDisk_ACompressedResponseIsCountedAsItExpands pins that the byte limit
// holds what a response decompresses to, not what it sends.
func TestDisk_ACompressedResponseIsCountedAsItExpands(t *testing.T) {
	t.Parallel()
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, err := zw.Write(make([]byte, 1<<20))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	srv := newProbeServer(t, false, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(packed.Bytes())
	})
	require.Less(t, packed.Len(), 4096, "the body sent is far below the limit")

	_, err = Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL, Limit: 4096})

	require.ErrorIs(t, err, compilers.ErrDocumentTooLarge)
}

// TestDisk_AStalledReadEndsAtItsDeadline pins that a read through the
// default client is bounded in time, and that a read through a caller's
// client keeps that client's own bound: a server that never answers fails
// the read once it passes.
func TestDisk_AStalledReadEndsAtItsDeadline(t *testing.T) {
	t.Parallel()
	assert.Equal(t, fetchTimeout, Disk{}.client().Timeout)
	srv := newProbeServer(t, false, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })

	start := time.Now()
	_, err := Disk{Client: &http.Client{Timeout: 50 * time.Millisecond}}.ReadDocument(t.Context(),
		compilers.DocumentRequest{Location: srv.URL, Reach: compilers.SpaceLocal})

	require.ErrorContains(t, err, "Client.Timeout exceeded")
	assert.Less(t, time.Since(start), 10*time.Second)
}

// TestHostSpace_ReadsANumberNoAddressSpellsAsLocal pins the spellings a URL
// parser takes for an IPv4 address and reads as none, and the IPv4 address
// NAT64's local-use prefix carries.
func TestHostSpace_ReadsANumberNoAddressSpellsAsLocal(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"0x", "1.2.3.4.5", "1.2.3.256", "1.09", "a.0x1g"} {
		want := compilers.SpaceLocal
		if host == "a.0x1g" {
			want = compilers.SpacePublic
		}
		assert.Equal(t, want, hostSpace(host), host)
	}
	assert.Equal(t, compilers.SpaceLocal, spaceOf(netip.MustParseAddr("64:ff9b:1:7f00:0:100::")))
	assert.Equal(t, compilers.SpacePrivate, spaceOf(netip.MustParseAddr("::7f00:1")), "IETF-reserved space")
}

// TestGuard_AnAddressItCannotReadIsRefusedToABoundedRead pins the control
// hook's own guard, which a dialer never reaches, handing it an address it
// resolved.
func TestGuard_AnAddressItCannotReadIsRefusedToABoundedRead(t *testing.T) {
	t.Parallel()
	g := &guard{reach: compilers.SpacePublic}
	require.ErrorIs(t, g.control(t.Context(), "tcp", "not-an-address", nil), compilers.ErrAddressSpace)
	g.reach = compilers.SpaceUnstated
	assert.NoError(t, g.control(t.Context(), "tcp", "not-an-address", nil))
}
