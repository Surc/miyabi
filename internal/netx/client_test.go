package netx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func TestSafeRestyClientGuardsMetadataRequests(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("User-Agent") != "go-resty/"+resty.Version+" (https://github.com/go-resty/resty)" {
			t.Errorf("changed source user agent: %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
		case "/large":
			io.WriteString(w, strings.Repeat("x", int(MaxSafeDownloadBytes)+1))
		case "/cookie":
			if _, err := r.Cookie("session"); err != nil {
				t.Error("source client lost its session")
			}
		default:
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "test", Path: "/"})
			io.WriteString(w, "metadata")
		}
	}))
	defer proxy.Close()
	manager, err := NewProxyManager(ProxyConfig{Enabled: true, URL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := NewSafeRestyClient(manager, time.Second)
	defer client.GetClient().CloseIdleConnections()
	const origin = "http://93.184.216.34"
	if response, err := client.R().Get(origin); err != nil || response.String() != "metadata" {
		t.Fatalf("metadata request: %v, %v", response, err)
	}
	if _, err := client.R().Get(origin + "/cookie"); err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	if _, err := client.R().Get("http://127.0.0.1/private"); err == nil || requests.Load() != before {
		t.Fatalf("private target reached proxy: requests=%d error=%v", requests.Load(), err)
	}
	if _, err := client.R().Get(origin + "/redirect"); err == nil || !strings.Contains(err.Error(), "redirect blocked") || requests.Load() != before+1 {
		t.Fatalf("private redirect reached proxy: requests=%d error=%v", requests.Load(), err)
	}
	if _, err := client.R().Get(origin + "/large"); !errors.Is(err, resty.ErrResponseBodyTooLarge) {
		t.Fatalf("oversized metadata error=%v", err)
	}
}

func TestRestyClientResolvesProxyPerRequest(t *testing.T) {
	manager, err := NewProxyManager(ProxyConfig{})
	if err != nil {
		t.Fatal(err)
	}
	client := NewRestyClient(manager, RestyOptions{Timeout: time.Second, ResponseHeaderTimeout: 2 * time.Second})
	transport := client.GetClient().Transport.(*http.Transport)
	if transport.ResponseHeaderTimeout != 2*time.Second || client.GetClient().Timeout != time.Second {
		t.Fatalf("timeouts were not applied: %+v", transport)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if proxy, _ := transport.Proxy(request); proxy != nil {
		t.Fatalf("direct client resolved proxy %v", proxy)
	}
	if err := manager.Update(ProxyConfig{Enabled: true, URL: "socks5://127.0.0.1:1080"}); err != nil {
		t.Fatal(err)
	}
	if proxy, _ := transport.Proxy(request); proxy == nil || proxy.Scheme != "socks5" {
		t.Fatalf("updated proxy was not picked up: %v", proxy)
	}
}

func TestDirectRestyClientIgnoresManager(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8888")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8888")
	client := NewDirectRestyClient(RestyOptions{})
	transport := client.GetClient().Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatalf("direct client transport.Proxy must be nil, got non-nil proxy function")
	}
	if NewRestyClient(nil, RestyOptions{}).GetClient().Transport.(*http.Transport).Proxy != nil {
		t.Fatal("nil manager must also ignore environment proxies")
	}
}

func TestTransportProxyChangesApplyToExistingClient(t *testing.T) {
	proxy := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Host != "media.example" {
				t.Errorf("unexpected proxy target: %s", r.URL)
			}
			_, _ = io.WriteString(w, body)
		}))
	}
	first, second := proxy("first"), proxy("second")
	defer first.Close()
	defer second.Close()
	manager, err := NewProxyManager(ProxyConfig{})
	if err != nil {
		t.Fatal(err)
	}
	transport := NewTransport(manager)
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, step := range []struct{ url, body string }{{first.URL, "first"}, {second.URL, "second"}} {
		if err := manager.Update(ProxyConfig{Enabled: true, URL: step.url}); err != nil {
			t.Fatal(err)
		}
		response, err := client.Get("http://media.example/image")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != step.body {
			t.Fatalf("proxy response %q: %v", body, err)
		}
	}
	if err := manager.Update(ProxyConfig{}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://media.example/image", nil)
	if resolved, err := transport.Proxy(req); err != nil || resolved != nil {
		t.Fatalf("disabled proxy: %v %v", resolved, err)
	}
}

func TestFingerprintClientBuildsWithAndWithoutProxy(t *testing.T) {
	_, proxy, err := Normalize(ProxyConfig{Enabled: true, URL: "http://127.0.0.1:7890"})
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range []FingerprintOptions{{Timeout: time.Second}, {Timeout: time.Second, Proxy: proxy, CookieJar: true}} {
		client, err := NewFingerprintClient(options)
		if err != nil {
			t.Fatalf("NewFingerprintClient(%+v): %v", options, err)
		}
		client.CloseIdleConnections()
	}
}
