// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers"
)

// MaxRedirects is the most redirects one read follows: each hop is a dial,
// judged as the first is, and a chain past it fails the read.
const MaxRedirects = 10

// fetchTimeout bounds one read through the default client, which sets none
// of its own: a server that accepts and never answers fails the read once it
// passes, where it would stall the compile. A caller's own Client keeps its
// own Timeout.
const fetchTimeout = 30 * time.Second

// dialTimeout and dialKeepAlive are what http.DefaultTransport dials with.
const (
	dialTimeout   = 30 * time.Second
	dialKeepAlive = 30 * time.Second
)

// guard is one read's judge of the addresses it dials: each connection is
// judged at the address the kernel is about to connect to, so a name that
// resolves anew at each dial gains nothing, and the space of the address that
// answered is noted. Through a proxy the dial is the proxy's, so the host the
// URL writes is judged instead (hostSpace).
type guard struct {
	reach    compilers.AddressSpace
	classify func(netip.AddrPort) compilers.AddressSpace
	resolver *net.Resolver
	// space is the space of the last address dialled, or of the last host
	// sent through a proxy.
	space atomic.Uint32
	// proxied says the request being sent goes through a proxy, whose dial is
	// not the request's address.
	proxied atomic.Bool
}

// admits reports the refusal of a request bounded by g's reach to an address
// in space, or nil.
func (g *guard) admits(address string, space compilers.AddressSpace) error {
	if g.reach != compilers.SpaceUnstated && space > g.reach {
		return &compilers.AddressSpaceError{Address: address, Space: space}
	}
	return nil
}

// control is the dialer's control hook: it judges the address being connected
// to, unless the connection is to a proxy. An address it cannot read is
// refused to a bounded read.
func (g *guard) control(_ context.Context, _, address string, _ syscall.RawConn) error {
	if g.proxied.Load() {
		return nil
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return g.admits(address, compilers.SpaceLocal)
	}
	return g.admits(address, g.classify(ap))
}

// dial connects as a dialer under control does, and notes the space of the
// address that answered.
func (g *guard) dial(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive, ControlContext: g.control, Resolver: g.resolver}
	conn, err := d.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if g.proxied.Load() {
		return conn, nil
	}
	if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		g.space.Store(uint32(g.classify(tcp.AddrPort())))
	}
	return conn, nil
}

// proxy wraps a transport's proxy choice, which it never changes: a request
// keeps the route the caller's transport chose. Through a proxy the dial is
// the proxy's, out of the guard's sight, so a read bounded by a reach has the
// host ranked instead (hostRank), a name by every address the guard's own
// resolver gives it (resolvedSpace), and one past the reach is refused before
// anything is dialled.
func (g *guard) proxy(choose func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		via, err := choose(req)
		if err != nil || via == nil {
			g.proxied.Store(false)
			return via, err
		}
		host := req.URL.Hostname()
		space, name := hostRank(host)
		if name && g.reach != compilers.SpaceUnstated {
			if space, err = g.resolvedSpace(req.Context(), host); err != nil {
				return nil, err
			}
		}
		if err := g.admits(host, space); err != nil {
			return nil, err
		}
		g.proxied.Store(true)
		g.space.Store(uint32(space))
		return via, nil
	}
}

// resolvedSpace is the most private space among the addresses the guard's
// resolver gives the name host, which a read through a proxy is judged by. A
// name that resolves to none cannot be judged, so a bounded read of it fails
// with ErrUnboundedRead. The proxy resolves the name again, and may be given
// another answer, which the guard cannot see.
func (g *guard) resolvedSpace(ctx context.Context, host string) (compilers.AddressSpace, error) {
	addrs, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return compilers.SpaceUnstated, fmt.Errorf("%s resolves to no address to judge: %w", host, compilers.ErrUnboundedRead)
	}
	space := compilers.SpacePublic
	for _, a := range addrs {
		space = max(space, g.classify(netip.AddrPortFrom(a, 0)))
	}
	return space, nil
}

// guardedClient returns the client a read bounded by reach goes through, and
// the guard judging it, or nil where client's transport is one the guard
// cannot hook: then a bounded read fails with ErrUnboundedRead, and an
// unbounded one goes through client as it is, stating no space.
func guardedClient(client *http.Client, reach compilers.AddressSpace,
	classify func(netip.AddrPort) compilers.AddressSpace, resolver *net.Resolver,
) (*http.Client, *guard, error) {
	tr, ok := hookable(client.Transport)
	switch {
	case !ok && reach != compilers.SpaceUnstated:
		return nil, nil, compilers.ErrUnboundedRead
	case !ok:
		return client, nil, nil
	}
	g := &guard{reach: reach, classify: classify, resolver: resolver}
	clone := tr.Clone()
	// No connection outlives its read, so none dialled for one read, judged
	// under its reach, carries another.
	clone.DisableKeepAlives = true
	clone.DialContext = g.dial
	if clone.Proxy != nil {
		clone.Proxy = g.proxy(clone.Proxy)
	}
	guarded := *client
	guarded.Transport = clone
	guarded.CheckRedirect = redirects(client.CheckRedirect)
	return &guarded, g, nil
}

// hookable returns the transport a guard can hook for rt: for nil,
// http.DefaultTransport's, whose dialer is net/http's own and is replaced
// with one judging the same connections; else rt, where it is an
// *http.Transport dialling through no function of the caller's. net/http
// dials https through DialTLSContext in preference to DialContext, and a
// caller's own dialer cannot be replaced without changing where it connects.
func hookable(rt http.RoundTripper) (*http.Transport, bool) {
	if rt == nil {
		tr, ok := http.DefaultTransport.(*http.Transport)
		return tr, ok
	}
	tr, ok := rt.(*http.Transport)
	if !ok || tr.DialContext != nil || tr.DialTLSContext != nil {
		return nil, false
	}
	return tr, tr.Dial == nil && tr.DialTLS == nil
}

// redirects is the redirect policy of a guarded read: at most MaxRedirects
// hops, then the caller's own policy, if any.
func redirects(own func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", MaxRedirects)
		}
		if own != nil {
			return own(req, via)
		}
		return nil
	}
}

// spaceOf ranks addr by how private it is, an IPv4 address mapped into IPv6,
// or embedded by NAT64 or 6to4, ranked as that IPv4 address is.
func spaceOf(addr netip.Addr) compilers.AddressSpace {
	addr = addr.Unmap()
	if embedded, ok := embeddedIPv4(addr); ok {
		addr = embedded
	}
	switch {
	case addr.IsLoopback(), addr.IsUnspecified(), thisNetwork.Contains(addr):
		return compilers.SpaceLocal
	case addr.IsPrivate(), addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(), addr.IsMulticast():
		return compilers.SpacePrivate
	}
	for _, p := range privatePrefixes {
		if p.Contains(addr) {
			return compilers.SpacePrivate
		}
	}
	return compilers.SpacePublic
}

// thisNetwork is 0.0.0.0/8, which a host reads as itself (RFC 1122 §3.2.1.3).
var thisNetwork = netip.MustParsePrefix("0.0.0.0/8")

// privatePrefixes are the ranges no public host lies in beside those
// netip.Addr's methods name: shared, reserved, benchmarking and documentation
// space, IPv6 site-local, Teredo and the IPv6 space the IETF reserves.
var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("::/8"),
}

// embeddedIPv4 returns the IPv4 address an IPv6 one carries by NAT64 (RFC
// 6052, 64:ff9b::/96 and the local-use 64:ff9b:1::/48) or 6to4 (2002::/16).
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	if !addr.Is6() {
		return netip.Addr{}, false
	}
	b := addr.As16()
	switch {
	case nat64.Contains(addr):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case nat64Local.Contains(addr):
		return netip.AddrFrom4([4]byte{b[6], b[7], b[9], b[10]}), true
	case sixToFour.Contains(addr):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

var (
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48")
	sixToFour  = netip.MustParsePrefix("2002::/16")
)

// hostSpace ranks the host a URL writes, as a read through a proxy judges
// it (hostRank).
func hostSpace(host string) compilers.AddressSpace {
	space, _ := hostRank(host)
	return space
}

// hostRank ranks the host a URL writes, and reports whether it is a name its
// text cannot rank, which resolving ranks: an
// address netip reads, by spaceOf; a host ending in a number, read as a URL
// parser reads an IPv4 address (decimal, 0x hexadecimal, leading-zero octal,
// one to four parts), and local where those rules read none; localhost and a
// name under it, local; any other name public. net/http maps a host that is
// not ASCII by IDNA before it uses it, which this reader cannot repeat, so
// such a host, and one with an empty label, ranks local: what it would be
// dialled as is not known.
func hostRank(host string) (compilers.AddressSpace, bool) {
	host = strings.ToLower(strings.TrimRight(host, "."))
	if !isASCII(host) || host == "" || strings.Contains(host, "..") {
		return compilers.SpaceLocal, false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return spaceOf(addr), false
	}
	if endsInNumber(host) {
		addr, ok := parseIPv4Host(host)
		if !ok {
			return compilers.SpaceLocal, false
		}
		return spaceOf(addr), false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return compilers.SpaceLocal, false
	}
	return compilers.SpacePublic, true
}

// isASCII reports whether s holds ASCII alone.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// endsInNumber reports whether host's last label is a number a URL parser
// reads: decimal digits, or 0x and hexadecimal ones.
func endsInNumber(host string) bool {
	last := host[strings.LastIndexByte(host, '.')+1:]
	if hex, ok := strings.CutPrefix(last, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") == ""
	}
	return last != "" && strings.Trim(last, "0123456789") == ""
}

// parseIPv4Host reads host as the IPv4 address a URL parser reads it as: one
// to four parts, each decimal, 0x hexadecimal or leading-zero octal, the last
// filling the bytes the others leave.
func parseIPv4Host(host string) (netip.Addr, bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	var out [4]byte
	for i, part := range parts {
		n, ok := ipv4Number(part)
		if !ok {
			return netip.Addr{}, false
		}
		if i < len(parts)-1 {
			if n > 255 {
				return netip.Addr{}, false
			}
			out[i] = byte(n)
			continue
		}
		rest := 4 - i
		if n >= 1<<(8*rest) {
			return netip.Addr{}, false
		}
		for j := 3; j >= i; j-- {
			out[j] = byte(n)
			n >>= 8
		}
	}
	return netip.AddrFrom4(out), true
}

// ipv4Number reads one part of an IPv4 host: 0x hexadecimal, leading-zero
// octal or decimal.
func ipv4Number(part string) (uint64, bool) {
	base := 10
	switch {
	case strings.HasPrefix(part, "0x"):
		part, base = part[2:], 16
		if part == "" {
			return 0, true
		}
	case len(part) > 1 && part[0] == '0':
		part, base = part[1:], 8
	}
	n, err := strconv.ParseUint(part, base, 32)
	return n, err == nil
}
