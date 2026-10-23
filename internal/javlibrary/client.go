// Package javlibrary is an isolated, read-only adapter for public film ratings
// and comments. It never writes ratings into Miyabi's scraped metadata.
package javlibrary

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/net/html"
)

const site = "https://www.javlibrary.com/cn/"
const maxBody = 2 << 20

var filmID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

type Film struct {
	ID     string
	Rating domain.ExternalRating
}
type Client struct {
	http *http.Client
	base *url.URL
}

func New(proxy *netx.ProxyManager) *Client {
	base, _ := url.Parse(site)
	transport := netx.NewTransport(proxy)
	observeTransport(transport)
	c := &Client{base: base, http: &http.Client{Timeout: 15 * time.Second, Transport: transport}}
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		reason := ""
		switch {
		case len(via) >= 3:
			reason = "redirect_limit"
		case req.URL.Scheme != base.Scheme:
			reason = "redirect_scheme"
		case req.URL.Host != base.Host:
			reason = "redirect_host"
		case req.URL.User != nil:
			reason = "redirect_credentials"
		}
		diagnosticsFrom(req.Context()).update(func(info *requestInfo) {
			info.Redirects = len(via)
			if reason != "" {
				info.Phase = "redirect"
				info.RedirectTarget = diagnosticURL(req.URL, true)
				if req.Response != nil {
					info.Status = req.Response.StatusCode
				}
			}
		})
		if reason != "" {
			return failure("redirect", reason)
		}
		return nil
	}
	return c
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

type pageResult struct {
	doc      *html.Node
	location *url.URL
	trace    *requestDiagnostics
}

func (c *Client) page(ctx context.Context, operation, path string, query url.Values) (pageResult, error) {
	u := c.base.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})
	d := &requestDiagnostics{started: time.Now(), info: requestInfo{AttemptID: nextAttempt.Add(1), Operation: operation, Phase: "request", Target: diagnosticURL(u, true), Proxy: "not_resolved"}}
	result := pageResult{trace: d}
	ctx = context.WithValue(ctx, diagnosticsKey{}, d)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, d.wrap(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.5")
	response, err := c.http.Do(req)
	if err != nil {
		return result, d.wrap(err)
	}
	defer response.Body.Close()
	result.location = response.Request.URL
	d.update(func(info *requestInfo) {
		info.Phase = "response"
		info.Target = diagnosticURL(result.location, true)
		info.Status = response.StatusCode
		info.ContentType = diagnosticContentType(response.Header.Get("Content-Type"))
	})
	if response.StatusCode != http.StatusOK {
		return result, d.wrap(failure("http_status", "non_200_response"))
	}
	d.update(func(info *requestInfo) { info.Phase = "body" })
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	d.update(func(info *requestInfo) { info.Bytes = len(body) })
	if err != nil {
		return result, d.wrap(err)
	}
	if len(body) > maxBody {
		return result, d.wrap(failure("body_limit", "response_too_large"))
	}
	d.update(func(info *requestInfo) { info.Phase = "parse" })
	result.doc, err = parseDocument(body)
	if err != nil {
		return result, d.wrap(&sourceError{category: "unexpected_page", reason: "html_parse_failed", cause: err})
	}
	shape := pageShape(result.doc)
	d.update(func(info *requestInfo) { info.Shape = shape })
	if problem := pageProblem(result.doc); problem != "" {
		return result, d.wrap(failure("unexpected_page", problem))
	}
	d.logPage(ctx)
	return result, nil
}

func (c *Client) Lookup(ctx context.Context, code string) (Film, error) {
	code = codeid.Normalize(code)
	empty := Film{Rating: domain.ExternalRating{Source: "javlibrary", Max: 10}}
	if code == "" {
		return empty, domain.E(domain.KindInvalid, "影片番号不能为空", nil)
	}
	page, err := c.page(ctx, "search", "vl_searchbyid.php", url.Values{"keyword": {code}})
	if err != nil {
		return empty, err
	}
	if byID(page.doc, "video_id") != nil {
		film, err := parseFilm(page.doc, page.location.Query().Get("v"), code)
		return film, page.trace.wrap(err)
	}
	id, err := searchFilm(page.doc, code)
	if err != nil || id == "" {
		return empty, page.trace.wrap(err)
	}
	page, err = c.page(ctx, "detail", "", url.Values{"v": {id}})
	if err != nil {
		return empty, err
	}
	if page.location.Query().Get("v") != id {
		return empty, page.trace.wrap(failure("identity", "film_redirect_changed_identity"))
	}
	film, err := parseFilm(page.doc, id, code)
	return film, page.trace.wrap(err)
}

func (c *Client) Comments(ctx context.Context, id string, page int) (domain.MovieComments, error) {
	result := domain.MovieComments{Source: "javlibrary", Page: page, Items: []domain.MovieComment{}}
	if !filmID.MatchString(id) || page < 1 || page > 1000 {
		return result, domain.E(domain.KindInvalid, "无效的影片或评论页码", nil)
	}
	response, err := c.page(ctx, "comments", "videocomments.php", url.Values{"v": {id}, "page": {strconv.Itoa(page)}})
	if err != nil {
		return result, err
	}
	if response.location.Query().Get("v") != id {
		return result, response.trace.wrap(failure("identity", "comments_redirect_changed_identity"))
	}
	result, err = parseComments(response.doc, id, page)
	return result, response.trace.wrap(err)
}
