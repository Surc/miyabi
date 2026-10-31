package javlibrary

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/time/rate"
)

// NewRatingReader ports Amane's browser-fingerprinted request strategy using
// existing Go dependencies. It shares Lookup's exact identity and score checks;
// it does not run Amane, a browser, or a challenge solver.
func NewRatingReader(proxy *netx.ProxyManager) *Client {
	c := New(proxy)
	c.http.Transport = &ratingTransport{proxy: proxy, limiter: rate.NewLimiter(5, 1)}
	return c
}

func (c *Client) Rating(ctx context.Context, code string) (domain.ExternalRating, error) {
	film, err := c.Lookup(ctx, code)
	return film.Rating, err
}

var ratingProfiles = []struct {
	profile profiles.ClientProfile
	agent   string
}{
	{profiles.Chrome_124, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"},
	{profiles.Chrome_131, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"},
	{profiles.Firefox_133, "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0"},
	{profiles.Firefox_135, "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0"},
}

type ratingSession struct {
	client  tlsclient.HttpClient
	proxy   string
	profile int
}

// HTTP redirects remain owned by Client's standard http.Client, retaining its
// same-origin and identity checks. Each session owns its cookies; changing proxy
// or fingerprint discards them rather than sharing a challenge session across IPs.
type ratingTransport struct {
	proxy   *netx.ProxyManager
	limiter *rate.Limiter
	mu      sync.Mutex
	session *ratingSession
}

func (t *ratingTransport) current() (*ratingSession, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	proxy := ""
	if t.proxy != nil {
		if address := t.proxy.Resolve(); address != nil {
			proxy = address.String()
		}
	}
	if t.session == nil || t.session.proxy != proxy {
		return t.install(proxy, 0)
	}
	return t.session, nil
}

// install is called with mu held. In-flight requests keep their old session.
func (t *ratingTransport) install(proxy string, profile int) (*ratingSession, error) {
	options := []tlsclient.HttpClientOption{
		tlsclient.WithTimeoutSeconds(15),
		tlsclient.WithClientProfile(ratingProfiles[profile].profile),
		tlsclient.WithNotFollowRedirects(),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
	}
	if proxy != "" {
		options = append(options, tlsclient.WithProxyUrl(proxy))
	}
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	if t.session != nil {
		t.session.client.CloseIdleConnections()
	}
	t.session = &ratingSession{client: client, proxy: proxy, profile: profile}
	return t.session, nil
}

func (t *ratingTransport) rotate(rejected *ratingSession) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session != rejected {
		return true, nil
	}
	if rejected.profile+1 == len(ratingProfiles) {
		return false, nil
	}
	_, err := t.install(rejected.proxy, rejected.profile+1)
	return err == nil, err
}

func (t *ratingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// At most four profiles per request. Rejections stay remembered for this
	// proxy; later requests do not start another full rotation after exhaustion.
	for attempt := 0; ; attempt++ {
		if err := t.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
		session, err := t.current()
		if err != nil {
			return nil, err
		}
		address, _ := url.Parse(session.proxy)
		diagnosticsFrom(req.Context()).update(func(info *requestInfo) {
			info.Proxy = "direct"
			if session.proxy != "" {
				info.Proxy = diagnosticURL(address, false)
			}
		})
		request, err := fhttp.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), nil)
		if err != nil {
			return nil, err
		}
		request.Header = fhttp.Header(req.Header.Clone())
		request.Header.Set("User-Agent", ratingProfiles[session.profile].agent)
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		response, err := session.client.Do(request)
		if err != nil {
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
			return nil, err
		}
		if (response.StatusCode == 403 || response.StatusCode == 406) && attempt < len(ratingProfiles)-1 {
			retry, err := t.rotate(session)
			if err != nil || retry {
				response.Body.Close()
				if err != nil {
					return nil, err
				}
				continue
			}
		}
		return &http.Response{
			Status: response.Status, StatusCode: response.StatusCode,
			Proto: response.Proto, ProtoMajor: response.ProtoMajor, ProtoMinor: response.ProtoMinor,
			Header: http.Header(response.Header), Body: response.Body,
			ContentLength: response.ContentLength, TransferEncoding: response.TransferEncoding,
			Close: response.Close, Uncompressed: response.Uncompressed,
			Trailer: http.Header(response.Trailer), Request: req,
		}, nil
	}
}

func (t *ratingTransport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session != nil {
		t.session.client.CloseIdleConnections()
	}
}
