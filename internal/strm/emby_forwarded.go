package strm

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
)

func parseEmbyProxies(peers []string) ([]netip.Prefix, error) {
	var trusted []netip.Prefix
	for _, value := range peers {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			ip, err := netip.ParseAddr(value)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy IP or CIDR %q", value)
			}
			ip = ip.Unmap()
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		trusted = append(trusted, prefix)
	}
	return trusted, nil
}

type embyOrigin struct{ ip, scheme, host string }

func (p *embyProxy) trusts(ip netip.Addr) bool {
	for _, prefix := range p.trusted {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (p *embyProxy) requestOrigin(r *http.Request) embyOrigin {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	origin := embyOrigin{ip: host, scheme: "http", host: r.Host}
	if r.TLS != nil {
		origin.scheme = "https"
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !p.trusts(peer.Unmap()) {
		return origin
	}
	chain := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if chain == "" {
		chain = r.Header.Get("X-Real-IP")
	}
	hops := 0
	addresses := strings.Split(chain, ",")
	for i := len(addresses) - 1; i >= 0 && p.trusts(peer.Unmap()); i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(addresses[i]))
		if err != nil {
			break
		}
		peer = addr.Unmap()
		origin.ip = peer.String()
		hops++
	}
	// Some frontends overwrite these fields, while others append one per hop.
	forwarded := func(name string) string {
		values := strings.Split(strings.Join(r.Header.Values(name), ","), ",")
		return strings.TrimSpace(values[max(0, len(values)-max(1, hops))])
	}
	if scheme := strings.ToLower(forwarded("X-Forwarded-Proto")); scheme == "https" || scheme == "http" {
		origin.scheme = scheme
	}
	if host := forwarded("X-Forwarded-Host"); host != "" {
		if u, err := url.Parse("http://" + host); err == nil && u.Host == host && u.Hostname() != "" {
			origin.host = host
		}
	}
	return origin
}

func (p *embyProxy) setForwarded(req *httputil.ProxyRequest) {
	origin := p.requestOrigin(req.In)
	req.Out.Header.Set("X-Real-IP", origin.ip)
	req.Out.Header.Set("X-Forwarded-For", origin.ip)
	req.Out.Header.Set("X-Forwarded-Proto", origin.scheme)
	req.Out.Header.Set("X-Forwarded-Host", origin.host)
	req.Out.Header.Del("X-Forwarded-Port")
}
