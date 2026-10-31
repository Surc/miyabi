package netx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// This fixed HTTPS endpoint bootstraps real DNS when the system resolver
// returns proxy placeholders. It never follows a URL supplied by a download.
var publicDNSClient = &http.Client{
	Transport: NewTransport(nil),
	Timeout:   5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func isFakeIP(ip net.IP) bool {
	ip = ip.To4()
	return ip != nil && ip[0] == 198 && (ip[1]&0xfe) == 18
}

func lookupSafeIPs(ctx context.Context, hostname string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve hostname %s: %w", hostname, err)
	}
	var fake bool
	for _, ip := range ips {
		if isFakeIP(ip) {
			fake = true
		} else if IsPrivateOrLoopbackIP(ip) {
			return nil, fmt.Errorf("domain %s resolved to private/loopback IP %s", hostname, ip)
		}
	}
	if fake {
		ips, err = lookupPublicIPs(ctx, hostname)
		if err != nil {
			return nil, fmt.Errorf("resolve Fake-IP hostname %s: %w", hostname, err)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP resolved for %s", hostname)
	}
	for _, ip := range ips {
		if IsPrivateOrLoopbackIP(ip) {
			return nil, fmt.Errorf("domain %s resolved to private/loopback IP %s", hostname, ip)
		}
	}
	return ips, nil
}

func lookupPublicIPs(ctx context.Context, hostname string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ips []net.IP
	// Check both families so a public A record cannot hide a private AAAA record.
	for _, recordType := range []string{"A", "AAAA"} {
		query := url.Values{"name": {hostname}, "type": {recordType}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://dns.alidns.com/resolve?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/dns-json")
		resp, err := publicDNSClient.Do(req)
		if err != nil {
			return nil, err
		}
		var answer struct {
			Status *int
			Answer []struct {
				Type int
				Data string
			}
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("public DNS HTTP status %d", resp.StatusCode)
		}
		if err != nil {
			return nil, fmt.Errorf("read public DNS response: %w", err)
		}
		if len(body) > 64<<10 {
			return nil, fmt.Errorf("public DNS response exceeds size limit")
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			return nil, fmt.Errorf("decode public DNS response: %w", err)
		}
		if answer.Status == nil {
			return nil, fmt.Errorf("missing public DNS status")
		}
		if *answer.Status != 0 {
			return nil, fmt.Errorf("public DNS status %d", *answer.Status)
		}
		for _, record := range answer.Answer {
			if record.Type != 1 && record.Type != 28 {
				continue
			}
			ip := net.ParseIP(record.Data)
			if ip == nil {
				return nil, fmt.Errorf("invalid public DNS address %q", record.Data)
			}
			ips = append(ips, ip)
		}
	}
	return ips, nil
}
