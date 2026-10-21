// Package javlibrary is an isolated, read-only adapter for public film ratings
// and comments. It never writes ratings into Miyabi's scraped metadata.
package javlibrary

import (
	"context"
	"fmt"
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
	c := &Client{base: base, http: &http.Client{Timeout: 15 * time.Second, Transport: netx.NewTransport(proxy)}}
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != base.Scheme || req.URL.Host != base.Host || req.URL.User != nil {
			return fmt.Errorf("unsupported JavLibrary redirect")
		}
		return nil
	}
	return c
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) page(ctx context.Context, path string, query url.Values) (*html.Node, *url.URL, error) {
	u := c.base.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.5")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("JavLibrary HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxBody {
		return nil, nil, fmt.Errorf("JavLibrary response too large")
	}
	doc, err := parseDocument(body)
	return doc, response.Request.URL, err
}

func (c *Client) Lookup(ctx context.Context, code string) (Film, error) {
	code = codeid.Normalize(code)
	empty := Film{Rating: domain.ExternalRating{Source: "javlibrary", Max: 10}}
	if code == "" {
		return empty, domain.E(domain.KindInvalid, "影片番号不能为空", nil)
	}
	doc, location, err := c.page(ctx, "vl_searchbyid.php", url.Values{"keyword": {code}})
	if err != nil {
		return empty, err
	}
	if byID(doc, "video_id") != nil {
		return parseFilm(doc, location.Query().Get("v"), code)
	}
	id, err := searchFilm(doc, code)
	if err != nil || id == "" {
		return empty, err
	}
	doc, location, err = c.page(ctx, "", url.Values{"v": {id}})
	if err != nil {
		return empty, err
	}
	if location.Query().Get("v") != id {
		return empty, fmt.Errorf("JavLibrary film redirect changed identity")
	}
	return parseFilm(doc, id, code)
}

func (c *Client) Comments(ctx context.Context, id string, page int) (domain.MovieComments, error) {
	result := domain.MovieComments{Source: "javlibrary", Page: page, Items: []domain.MovieComment{}}
	if !filmID.MatchString(id) || page < 1 || page > 1000 {
		return result, domain.E(domain.KindInvalid, "无效的影片或评论页码", nil)
	}
	doc, location, err := c.page(ctx, "videocomments.php", url.Values{"v": {id}, "page": {strconv.Itoa(page)}})
	if err != nil {
		return result, err
	}
	if location.Query().Get("v") != id {
		return result, fmt.Errorf("JavLibrary comments redirect changed identity")
	}
	return parseComments(doc, id, page)
}
