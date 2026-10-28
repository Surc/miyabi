package javinizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/netx"
)

const maxBody = 2 << 20

// Keep the cause available to errors.Is/As without exposing arbitrary network
// error text (which may include a URL or credentials) to application logs.
type requestError struct{ cause error }

func (e *requestError) Error() string { return "Javinizer: request failed" }
func (e *requestError) Unwrap() error { return e.cause }

// Client owns only its HTTP pool. The engine, proxy and optional FlareSolverr
// have independent lifecycles and configuration; Miyabi never organizes files
// through this API. One slot bounds Miyabi HTTP requests, including queue time;
// an engine-side browser job may outlive a canceled HTTP request.
type Client struct {
	http     *http.Client
	endpoint string
	token    string
	timeout  time.Duration
	slot     chan struct{}
}

func New(config Config) (*Client, error) {
	if !config.Enabled {
		return nil, nil
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 45 * time.Second
	}
	return &Client{
		http: &http.Client{
			Transport: netx.NewTransport(nil), Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		endpoint: strings.TrimRight(config.ServerURL, "/") + "/api/v1/scrape",
		token:    config.APIToken, timeout: timeout, slot: make(chan struct{}, 1),
	}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

// Rating preserves JavLibrary's original ten-point score. The upstream API
// conflates missing/unrated pages with several failures, so ambiguous outcomes
// remain errors and cannot enter Miyabi's negative-result cache.
func (c *Client) Rating(ctx context.Context, code string) (domain.ExternalRating, error) {
	empty := domain.ExternalRating{Source: "javlibrary", Max: 10}
	code = codeid.Normalize(code)
	if len(code) > 200 || !codeid.Valid(code) {
		return empty, domain.E(domain.KindInvalid, "无效的影片番号", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case c.slot <- struct{}{}:
		defer func() { <-c.slot }()
	case <-ctx.Done():
		return empty, ctx.Err()
	}
	payload, _ := json.Marshal(struct {
		ID       string   `json:"id"`
		Force    bool     `json:"force"`
		Scrapers []string `json:"selected_scrapers"`
	}{code, true, []string{"javlibrary"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return empty, fmt.Errorf("Javinizer: invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		// Arbitrary transport/server messages can contain credentials or body
		// content. Report a stable cause; never append the upstream text.
		return empty, &requestError{cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("Javinizer: HTTP %d", resp.StatusCode)
	}
	// This capability header is produced by the reviewed, versioned engine patch.
	// Vanilla v1.6.5 can synthesize identity and fetch images; reject it explicitly.
	if resp.Header.Get("X-Miyabi-JavLibrary-Identity") != "page-v1" {
		return empty, fmt.Errorf("Javinizer: rating-only identity contract unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if err != nil || len(body) > maxBody {
		return empty, fmt.Errorf("Javinizer: invalid response size or read failure")
	}
	var result scrapeResponse
	if json.Unmarshal(body, &result) != nil {
		return empty, fmt.Errorf("Javinizer: invalid JSON response")
	}
	return result.rating(code)
}

// Only consume the fixed API fields we need; do not import an upstream internal
// package or mirror the whole movie schema. New unrelated fields remain allowed.
type scrapeResponse struct {
	Cached      *bool    `json:"cached"`
	SourcesUsed int      `json:"sources_used"`
	Errors      []string `json:"errors"`
	Movie       *struct {
		Code    string   `json:"code"`
		ID      string   `json:"id"`
		Source  string   `json:"source_name"`
		Score   *float64 `json:"rating_score"`
		Warning string   `json:"rating_warning"`
	} `json:"movie"`
}

func (r scrapeResponse) rating(code string) (domain.ExternalRating, error) {
	empty := domain.ExternalRating{Source: "javlibrary", Max: 10}
	m := r.Movie
	if r.Cached == nil || *r.Cached || r.SourcesUsed != 1 || len(r.Errors) != 0 || m == nil || m.Source != "javlibrary" {
		return empty, fmt.Errorf("Javinizer: unverified rating source")
	}
	if codeid.Normalize(m.Code) != code || codeid.Normalize(m.ID) != code {
		return empty, fmt.Errorf("Javinizer: movie identity mismatch")
	}
	if m.Score == nil || math.IsNaN(*m.Score) || math.IsInf(*m.Score, 0) || *m.Score <= 0 || *m.Score > 10 || m.Warning != "" {
		return empty, fmt.Errorf("Javinizer: rating unavailable or invalid")
	}
	return domain.ExternalRating{Source: "javlibrary", Max: 10, Found: true, Score: m.Score}, nil
}
