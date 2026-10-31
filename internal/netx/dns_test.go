package netx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// A local DNS server exercises the real resolver without changing system DNS.
func stubSystemDNS(t *testing.T, hosts map[string][]string) {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", server.LocalAddr().String())
	}}
	t.Cleanup(func() { net.DefaultResolver = previous; server.Close() })
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, addr, err := server.ReadFrom(buffer)
			if err != nil {
				return
			}
			var message dnsmessage.Message
			if message.Unpack(buffer[:n]) != nil {
				continue
			}
			message.Header.Response = true
			message.Header.RecursionAvailable = true
			for _, question := range message.Questions {
				for _, value := range hosts[strings.TrimSuffix(question.Name.String(), ".")] {
					ip := net.ParseIP(value)
					header := dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET, TTL: 1}
					if ip4 := ip.To4(); ip4 != nil && question.Type == dnsmessage.TypeA {
						message.Answers = append(message.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AResource{A: [4]byte(ip4)}})
					} else if ip4 == nil && question.Type == dnsmessage.TypeAAAA {
						message.Answers = append(message.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AAAAResource{AAAA: [16]byte(ip.To16())}})
					}
				}
			}
			response, err := message.Pack()
			if err == nil {
				server.WriteTo(response, addr)
			}
		}
	}()
}

type dnsTransport func(*http.Request) (*http.Response, error)

func (f dnsTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func stubPublicDNS(t *testing.T, handler dnsTransport) {
	t.Helper()
	previous := publicDNSClient
	client := *previous
	client.Transport = handler
	publicDNSClient = &client
	t.Cleanup(func() { publicDNSClient = previous })
}

func dnsResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestFakeIPDNSPreservesAddressChecks(t *testing.T) {
	for _, test := range []struct {
		name   string
		host   string
		system []string
		ipv4   string
		ipv6   string
		calls  int32
		want   string
	}{
		{name: "public system DNS", host: "subtitle.test", system: []string{"93.184.216.34"}},
		{name: "fake DNS", host: "subtitle.test", system: []string{"198.18.0.73"}, ipv4: "93.184.216.34", ipv6: "2606:4700:4700::1111", calls: 2},
		{name: "fake mixed with public", host: "subtitle.test", system: []string{"198.19.1.2", "93.184.216.35"}, ipv4: "93.184.216.34", calls: 2},
		{name: "fake mixed with private", host: "subtitle.test", system: []string{"198.18.0.73", "10.0.0.1"}, want: "private/loopback"},
		{name: "private system DNS", host: "subtitle.test", system: []string{"127.0.0.1"}, want: "private/loopback"},
		{name: "private public DNS", host: "subtitle.test", system: []string{"198.18.0.73"}, ipv4: "192.168.1.1", calls: 2, want: "private/loopback"},
		{name: "mixed public DNS families", host: "subtitle.test", system: []string{"198.18.0.73"}, ipv4: "93.184.216.34", ipv6: "fd00::1", calls: 2, want: "private/loopback"},
		{name: "mapped loopback", host: "subtitle.test", system: []string{"198.18.0.73"}, ipv6: "::ffff:127.0.0.1", calls: 2, want: "private/loopback"},
		{name: "fake public DNS", host: "subtitle.test", system: []string{"198.18.0.73"}, ipv4: "198.18.0.74", calls: 2, want: "private/loopback"},
		{name: "empty public DNS", host: "subtitle.test", system: []string{"198.18.0.73"}, calls: 2, want: "no IP resolved"},
		{name: "literal fake IP", host: "198.18.0.73", want: "prohibited"},
		{name: "literal other fake range", host: "198.19.0.1", want: "prohibited"},
		{name: "literal loopback", host: "127.0.0.1", want: "prohibited"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stubSystemDNS(t, map[string][]string{test.host: test.system})
			var calls atomic.Int32
			stubPublicDNS(t, func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				if req.URL.Scheme != "https" || req.URL.Host != "dns.alidns.com" || req.URL.Path != "/resolve" || req.URL.Query().Get("name") != test.host {
					t.Errorf("unexpected public DNS request: %s", req.URL)
				}
				body := `{"Status":0,"Answer":[]}`
				if req.URL.Query().Get("type") == "A" && test.ipv4 != "" {
					body = fmt.Sprintf(`{"Status":0,"Answer":[{"Type":1,"Data":%q}]}`, test.ipv4)
				} else if req.URL.Query().Get("type") == "AAAA" && test.ipv6 != "" {
					body = fmt.Sprintf(`{"Status":0,"Answer":[{"Type":28,"Data":%q}]}`, test.ipv6)
				}
				return dnsResponse(req, http.StatusOK, body), nil
			})
			_, err := ValidateSafeURL(t.Context(), "https://"+test.host+"/subtitle.srt")
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("URL validation error = %v, want %q", err, test.want)
			}
			if calls.Load() != test.calls {
				t.Fatalf("public DNS calls = %d, want %d", calls.Load(), test.calls)
			}
		})
	}
}

func TestFakeIPDNSFailsClosed(t *testing.T) {
	stubSystemDNS(t, map[string][]string{"subtitle.test": {"198.18.0.73"}})
	for _, test := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"HTTP failure", 503, `{}`, "HTTP status 503"},
		{"redirect", 302, `{}`, "HTTP status 302"},
		{"NXDOMAIN", 200, `{"Status":3}`, "DNS status 3"},
		{"invalid JSON", 200, `<html>Error</html>`, "decode public DNS"},
		{"invalid address", 200, `{"Status":0,"Answer":[{"Type":1,"Data":"not-an-ip"}]}`, "invalid public DNS address"},
		{"oversized response", 200, strings.Repeat(" ", 64<<10) + `{}`, "size limit"},
		{"missing status", 200, `{"Answer":[{"Type":1,"Data":"93.184.216.34"}]}`, "missing public DNS status"},
		{"null status", 200, `{"Status":null,"Answer":[{"Type":1,"Data":"93.184.216.34"}]}`, "missing public DNS status"},
		{"trailing JSON", 200, `{"Status":0,"Answer":[{"Type":1,"Data":"93.184.216.34"}]}{}`, "decode public DNS"},
		{"oversized valid prefix", 200, `{"Status":0,"Answer":[{"Type":1,"Data":"93.184.216.34"}]}` + strings.Repeat(" ", 64<<10), "size limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stubPublicDNS(t, func(req *http.Request) (*http.Response, error) {
				response := dnsResponse(req, test.status, test.body)
				response.Header.Set("Location", "http://127.0.0.1/secret")
				return response, nil
			})
			_, err := ValidateSafeURL(t.Context(), "https://subtitle.test/file.srt")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
	t.Run("cancellation", func(t *testing.T) {
		stubPublicDNS(t, func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		_, err := ValidateSafeURL(ctx, "https://subtitle.test/file.srt")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation error = %v", err)
		}
	})
}

func TestFakeIPDialPinsRealAddressesAndRechecksDNS(t *testing.T) {
	stubSystemDNS(t, map[string][]string{"subtitle.test": {"198.18.0.73"}})
	var rebound atomic.Bool
	stubPublicDNS(t, func(req *http.Request) (*http.Response, error) {
		ip := "93.184.216.34"
		if rebound.Load() {
			ip = "127.0.0.1"
		}
		body := `{"Status":0,"Answer":[]}`
		if req.URL.Query().Get("type") == "A" {
			body = fmt.Sprintf(`{"Status":0,"Answer":[{"Type":1,"Data":%q}]}`, ip)
		}
		return dnsResponse(req, http.StatusOK, body), nil
	})
	var targets []string
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		targets = append(targets, address)
		client, server := net.Pipe()
		server.Close()
		return client, nil
	}
	conn, err := dialSafe(t.Context(), "tcp", "subtitle.test:443", dial)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if !slices.Equal(targets, []string{"93.184.216.34:443"}) {
		t.Fatalf("dial targets = %v", targets)
	}
	if _, err := ValidateSafeURL(t.Context(), "https://subtitle.test/file.srt"); err != nil {
		t.Fatal(err)
	}
	rebound.Store(true)
	_, err = dialSafe(t.Context(), "tcp", "subtitle.test:443", dial)
	if err == nil || !strings.Contains(err.Error(), "private/loopback") || len(targets) != 1 {
		t.Fatalf("DNS rebinding: targets=%v error=%v", targets, err)
	}
	transport := NewSafeTransport(nil)
	defer transport.CloseIdleConnections()
	if _, err := transport.DialContext(t.Context(), "tcp", "subtitle.test:443"); err == nil || !strings.Contains(err.Error(), "private/loopback") {
		t.Fatalf("transport accepted rebound DNS: %v", err)
	}
}

func TestFakeIPDownloadsThroughProxyKeepRedirectChecks(t *testing.T) {
	stubSystemDNS(t, map[string][]string{
		"subtitle.test": {"198.18.0.73"}, "other.test": {"198.18.0.74"}, "private.test": {"10.0.0.1"},
	})
	stubPublicDNS(t, func(req *http.Request) (*http.Response, error) {
		body := `{"Status":0,"Answer":[]}`
		if req.URL.Query().Get("type") == "A" {
			body = `{"Status":0,"Answer":[{"Type":1,"Data":"93.184.216.34"}]}`
		}
		return dnsResponse(req, http.StatusOK, body), nil
	})
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://other.test/sub", http.StatusFound)
		case "/private":
			http.Redirect(w, r, "http://private.test/secret", http.StatusFound)
		case "/fake-literal":
			http.Redirect(w, r, "http://198.18.0.73/secret", http.StatusFound)
		default:
			io.WriteString(w, "subtitle")
		}
	}))
	defer proxy.Close()
	manager, err := NewProxyManager(ProxyConfig{Enabled: true, URL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := NewSafeDownloadClient(manager, time.Second)
	defer client.CloseIdleConnections()
	for _, test := range []struct {
		path  string
		calls int32
		want  string
	}{{"/sub", 1, ""}, {"/redirect", 2, ""}, {"/private", 1, "redirect blocked"}, {"/fake-literal", 1, "redirect blocked"}} {
		t.Run(test.path, func(t *testing.T) {
			calls.Store(0)
			body, err := SafeDownload(t.Context(), client, "http://subtitle.test"+test.path)
			if test.want == "" {
				if err != nil || string(body) != "subtitle" {
					t.Fatalf("download = %q, %v", body, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("download error = %v, want %q", err, test.want)
			}
			if calls.Load() != test.calls {
				t.Fatalf("proxy requests = %d, want %d", calls.Load(), test.calls)
			}
		})
	}
}
