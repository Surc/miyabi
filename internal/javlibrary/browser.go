package javlibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/netx"
)

var ErrVerificationRequired = errors.New("JavLibrary browser verification required")

// BrowserConfig connects to the separately managed, user-verified browser.
// Credentials are startup-only and never sent to JavLibrary or the frontend.
type BrowserConfig struct {
	Enabled        bool   `json:"enabled"`
	ServerURL      string `json:"server_url,omitempty"`
	ConnectBack    bool   `json:"connect_back,omitempty"`
	APIToken       string `json:"api_token"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func LoadBrowserConfig(path string) (BrowserConfig, error) {
	if path == "" {
		return BrowserConfig{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return BrowserConfig{}, errors.New("cannot read JavLibrary browser config")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(body) > 8192 {
		return BrowserConfig{}, errors.New("invalid JavLibrary browser config size")
	}
	var config *BrowserConfig
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || config == nil || decoder.Decode(new(any)) != io.EOF {
		return BrowserConfig{}, errors.New("invalid JavLibrary browser config")
	}
	return *config, config.validate()
}

func (c BrowserConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if c.ConnectBack {
		if c.ServerURL != "" {
			return errors.New("JavLibrary browser connect_back and server_url are mutually exclusive")
		}
	} else {
		u, err := url.Parse(c.ServerURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("JavLibrary browser server_url must be an HTTP(S) origin")
		}
	}
	if len(c.APIToken) < 32 || len(c.APIToken) > 512 || strings.ContainsAny(c.APIToken, "\r\n\t ") {
		return errors.New("invalid JavLibrary browser API token")
	}
	if c.TimeoutSeconds != 0 && (c.TimeoutSeconds < 5 || c.TimeoutSeconds > 60) {
		return errors.New("JavLibrary browser timeout must be between 5 and 60 seconds")
	}
	return nil
}

// NewBrowserRatingReader reuses the existing search, identity checks and parser.
// With this opt-in channel every page uses the same actual browser session;
// verified cookies are never copied into a different TLS client.
func NewBrowserRatingReader(config BrowserConfig) (*Client, error) {
	if !config.Enabled {
		return nil, nil
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	if config.ConnectBack {
		return nil, errors.New("JavLibrary browser connect_back requires a relay")
	}
	fetcher := &browserHTTP{
		http: &http.Client{Transport: netx.NewTransport(nil),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		endpoint: strings.TrimRight(config.ServerURL, "/") + "/v1/page",
		token:    config.APIToken,
		slot:     make(chan struct{}, 1),
	}
	return newBrowserClient(config, fetcher.fetch, fetcher.http.CloseIdleConnections), nil
}

type browserFetch func(context.Context, string, time.Duration) (browserPage, error)

func newBrowserClient(config BrowserConfig, fetch browserFetch, closeIdle func()) *Client {
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := New(nil)
	client.http.Timeout = timeout
	client.http.Transport = &browserTransport{fetch: fetch, closeIdle: closeIdle, timeout: timeout}
	return client
}

type browserTransport struct {
	fetch     browserFetch
	closeIdle func()
	timeout   time.Duration
}

type browserPageRequest struct {
	URL       string `json:"url"`
	TimeoutMS int64  `json:"timeout_ms"`
}

func newBrowserPageRequest(ctx context.Context, target string, timeout time.Duration) browserPageRequest {
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	return browserPageRequest{URL: target, TimeoutMS: max(1, timeout.Milliseconds()-1000)}
}

type browserPage struct {
	URL       string   `json:"url"`
	Status    int      `json:"status"`
	HTML      string   `json:"html"`
	Redirects []string `json:"redirects"`
	Error     string   `json:"error"`
}

type browserHTTP struct {
	http     *http.Client
	endpoint string
	token    string
	slot     chan struct{}
}

func (t *browserHTTP) fetch(ctx context.Context, target string, timeout time.Duration) (browserPage, error) {
	var page browserPage
	select {
	case t.slot <- struct{}{}:
		defer func() { <-t.slot }()
	case <-ctx.Done():
		return page, ctx.Err()
	}
	payload, _ := json.Marshal(newBrowserPageRequest(ctx, target, timeout))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return page, failure("browser", "invalid_service_target")
	}
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := t.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return page, ctx.Err()
		}
		return page, failure("browser", "service_unavailable")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 6*maxBody+1))
	if err != nil || len(body) > 6*maxBody {
		return page, failure("browser", "invalid_service_response_size")
	}
	if json.Unmarshal(body, &page) != nil {
		return page, failure("browser", "invalid_service_response")
	}
	if response.StatusCode == http.StatusConflict && page.Error == "verification_required" {
		return page, ErrVerificationRequired
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Miyabi-Browser-Page") != "1" {
		return page, failure("browser", "unverified_service_response")
	}
	return page, nil
}

func (t *browserTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet || !browserPageURL(request.URL) {
		return nil, failure("browser", "invalid_page_target")
	}
	ctx := request.Context()
	diagnosticsFrom(ctx).update(func(info *requestInfo) { info.Proxy = "browser_session" })
	page, err := t.fetch(ctx, request.URL.String(), t.timeout)
	if err != nil {
		return nil, err
	}
	if page.Status != http.StatusOK || len(page.HTML) > maxBody || len(page.Redirects) > 2 {
		return nil, failure("browser", "unverified_service_response")
	}
	location, err := url.Parse(page.URL)
	if err != nil || !browserPageURL(location) {
		return nil, failure("redirect", "browser_redirect_target")
	}
	for _, target := range page.Redirects {
		address, err := url.Parse(target)
		if err != nil || !browserPageURL(address) {
			return nil, failure("redirect", "browser_redirect_target")
		}
	}
	if location.String() != request.URL.String() && (len(page.Redirects) == 0 || page.Redirects[len(page.Redirects)-1] != location.String()) {
		return nil, failure("redirect", "browser_redirect_unobserved")
	}
	diagnosticsFrom(ctx).update(func(info *requestInfo) { info.Redirects = len(page.Redirects) })
	resolved := request.Clone(ctx)
	resolved.URL = location
	return &http.Response{
		StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body: io.NopCloser(strings.NewReader(page.HTML)), ContentLength: int64(len(page.HTML)), Request: resolved,
	}, nil
}

func browserPageURL(address *url.URL) bool {
	if address == nil || address.Scheme != "https" || address.Host != "www.javlibrary.com" || address.User != nil || address.Fragment != "" || address.RawPath != "" {
		return false
	}
	query, err := url.ParseQuery(address.RawQuery)
	if err != nil {
		return false
	}
	if strings.HasPrefix(address.Path, "/cn/") && filmHTMLPath.MatchString(address.Path) {
		return filmIDFromURL(address) != "" && (len(query) == 0 || (len(query) == 1 && len(query["v"]) == 1))
	}
	if len(query) != 1 {
		return false
	}
	key := "v"
	if address.Path == "/cn/vl_searchbyid.php" {
		key = "keyword"
	} else if address.Path != "/cn/" {
		return false
	}
	values := query[key]
	return len(values) == 1 && len(values[0]) <= 200 && values[0] != "" && (key == "keyword" || filmID.MatchString(values[0]))
}

func (t *browserTransport) CloseIdleConnections() {
	if t.closeIdle != nil {
		t.closeIdle()
	}
}
