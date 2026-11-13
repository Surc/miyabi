package javdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Adapted from the public detail path in ppxb/miyabi@bcf22366.
const (
	webBaseURL   = "https://javdb.com"
	webUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	webDurationPattern = regexp.MustCompile(`(\d+)`)
	webRatingPattern   = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s*分`)
)

func requiresWebFallback(err error) bool {
	if errors.Is(err, ErrLoginRequired) {
		return true
	}
	var response *HTTPError
	return errors.As(err, &response) && response.StatusCode == http.StatusForbidden
}

func (c *Client) movieDetailFromWeb(ctx context.Context, movieID string, apiErr error) (domain.MovieDetail, error) {
	if c.fingerprint == nil {
		return domain.MovieDetail{}, apiErr
	}
	if err := c.waitRequest(ctx, time.Now().Add(maxRateLimitWait)); err != nil {
		return domain.MovieDetail{}, err
	}
	detail, err := fetchWebMovie(ctx, c.fingerprint, movieID)
	if ctx.Err() != nil {
		return domain.MovieDetail{}, ctx.Err()
	}
	if err == nil {
		return detail, nil
	}
	var response *HTTPError
	if errors.As(err, &response) && response.StatusCode == http.StatusTooManyRequests {
		delay := response.RetryAfter
		if delay <= 0 {
			delay = time.Second
		}
		c.deferRequests(delay)
		return domain.MovieDetail{}, err
	}
	return domain.MovieDetail{}, domain.E(domain.KindUpstream, domain.PublicMessage(apiErr), errors.Join(apiErr, err))
}

func fetchWebMovie(ctx context.Context, client netx.FingerprintHTTPClient, movieID string) (domain.MovieDetail, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, webBaseURL+"/v/"+url.PathEscape(movieID), nil)
	if err != nil {
		return domain.MovieDetail{}, err
	}
	request.Header.Set("user-agent", webUserAgent)
	request.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("accept-language", defaultLanguage)

	// This request carries no App API credentials, device fields or signature.
	response, err := client.Do(request)
	if err != nil {
		return domain.MovieDetail{}, &networkError{err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return domain.MovieDetail{}, &HTTPError{
			StatusCode: response.StatusCode,
			RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
		}
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
			return domain.MovieDetail{}, errors.New("JavDB web detail did not return HTML")
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
	if err != nil {
		return domain.MovieDetail{}, &networkError{err: fmt.Errorf("read JavDB web response: %w", err)}
	}
	if len(body) > maxResponseBodyBytes {
		return domain.MovieDetail{}, errors.New("JavDB web response exceeded the size limit")
	}
	return parseWebMovieDetail(body, movieID)
}

func parseWebMovieDetail(raw []byte, movieID string) (domain.MovieDetail, error) {
	document, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return domain.MovieDetail{}, err
	}
	for n := range document.Descendants() {
		if n.DataAtom == atom.Link && strings.EqualFold(htmlAttr(n, "rel"), "canonical") {
			if webEntityID(htmlAttr(n, "href"), "/v/") != movieID {
				return domain.MovieDetail{}, errors.New("JavDB web detail has a different canonical movie ID")
			}
			break
		}
	}
	page := htmlFind(document, atom.Div, "video-detail")
	panel := htmlFind(page, atom.Nav, "movie-panel-info")
	if panel == nil {
		return domain.MovieDetail{}, errors.New("JavDB web page has no movie details")
	}
	detail := domain.MovieDetail{
		Movie: domain.Movie{
			ID: movieID, Sources: []domain.SourceID{{Provider: "javdb", ID: movieID}},
			RatingSource: "javdb", RatingMax: 5,
			Actors: []domain.Actor{}, Tags: []domain.Tag{},
		},
		Zone: domain.ZoneUnknown,
	}

	heading := htmlFind(page, atom.H2, "title")
	detail.Title = htmlText(htmlFind(heading, atom.Strong, "current-title"))
	detail.OriginTitle = htmlText(htmlFind(heading, atom.Span, "origin-title"))
	detail.Cover = webImageURL(htmlAttr(htmlFind(page, atom.Img, "video-cover"), "src"))
	detail.Thumbnail = detail.Cover

	for block := range panel.Descendants() {
		if block.DataAtom != atom.Div || !htmlHasClass(block, "panel-block") {
			continue
		}
		label := strings.TrimSpace(strings.TrimRight(htmlText(htmlFind(block, atom.Strong, "")), ":："))
		value := htmlFind(block, atom.Span, "value")
		text := htmlText(value)
		switch label {
		case "番號", "番号":
			detail.Code = strings.Join(strings.Fields(text), "")
		case "日期":
			if date, err := time.Parse(time.DateOnly, text); err == nil {
				detail.ReleaseDate = date.Format(time.DateOnly)
			}
		case "時長", "时长":
			detail.Duration, _ = strconv.Atoi(webDurationPattern.FindString(text))
		case "評分", "评分":
			detail.Rating = webRating(text)
		case "導演", "导演":
			if id, name := webEntityRef(value, "/directors/"); id != "" {
				detail.Director = &domain.Director{Provider: "javdb", ID: id, Name: name}
			}
		case "片商", "發行商", "发行商":
			if id, name := webEntityRef(value, "/makers/"); id != "" {
				detail.Maker = &domain.Maker{Provider: "javdb", ID: id, Name: name}
			}
		case "系列":
			if id, name := webEntityRef(value, "/series/"); id != "" {
				detail.Series = &domain.Series{Provider: "javdb", ID: id, Name: name}
			}
		case "類別", "类别":
			detail.Tags = webTags(value)
		case "演員", "演员":
			detail.Actors = webActors(value)
		}
	}

	if detail.Title == "" {
		detail.Title = detail.OriginTitle
	}
	if detail.Title == "" {
		detail.Title = strings.TrimSpace(strings.TrimPrefix(htmlText(heading), detail.Code))
	}
	if detail.Code == "" || detail.Title == "" || detail.Code == detail.Title {
		return domain.MovieDetail{}, errors.New("JavDB web page has incomplete movie identity")
	}
	detail.PreviewImages = webPreviewImages(page)
	detail.HasPreview = len(detail.PreviewImages) > 0
	detail.ActorMovies = webRecommendations(page, "出演")
	detail.RelatedMovies = webRecommendations(page, "喜歡", "喜欢")
	return detail, nil
}

func webRating(text string) float64 {
	if match := webRatingPattern.FindStringSubmatch(text); len(match) == 2 {
		score, err := strconv.ParseFloat(match[1], 64)
		if err == nil && score > 0 && score <= 5 {
			return score
		}
	}
	return 0
}

func webEntityRef(node *html.Node, prefix string) (id, name string) {
	if node == nil {
		return "", ""
	}
	for link := range node.Descendants() {
		if link.DataAtom == atom.A {
			if id := webEntityID(htmlAttr(link, "href"), prefix); id != "" {
				return id, htmlText(link)
			}
		}
	}
	return "", ""
}

func webEntityID(href, prefix string) string {
	link, err := url.Parse(href)
	if err != nil || link.User != nil || (link.Host != "" && link.Host != "javdb.com") ||
		(link.Scheme != "" && link.Scheme != "https") {
		return ""
	}
	id, found := strings.CutPrefix(strings.TrimSuffix(link.Path, "/"), prefix)
	if !found || !javdbMovieID.MatchString(id) {
		return ""
	}
	return id
}

func webImageURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	address, err := url.Parse(raw)
	// Public-page image URLs are later fetched by the catalogue and scraper.
	if err != nil || address.Scheme != "https" || address.User != nil ||
		!strings.HasSuffix(strings.ToLower(address.Hostname()), ".jdbstatic.com") || (address.Port() != "" && address.Port() != "443") {
		return ""
	}
	return raw
}

func webTags(node *html.Node) []domain.Tag {
	tags := []domain.Tag{}
	if node == nil {
		return tags
	}
	for link := range node.Descendants() {
		href := htmlAttr(link, "href")
		if link.DataAtom == atom.A && (href == "/tags" || strings.HasPrefix(href, "/tags?")) {
			if name := htmlText(link); name != "" {
				tags = append(tags, domain.Tag{Provider: "javdb", Name: name, NameZHT: name})
			}
		}
	}
	return tags
}

func webActors(node *html.Node) []domain.Actor {
	actors := []domain.Actor{}
	if node == nil {
		return actors
	}
	for link := range node.Descendants() {
		if link.DataAtom != atom.A {
			continue
		}
		id := webEntityID(htmlAttr(link, "href"), "/actors/")
		name := htmlText(link)
		if id == "" || name == "" {
			continue
		}
		gender := "unknown"
		if htmlHasClass(link, "actor-female") {
			gender = "female"
		} else if htmlHasClass(link, "actor-male") {
			gender = "male"
		}
		actors = append(actors, domain.Actor{Provider: "javdb", ID: id, Name: name, NameZHT: name, Gender: gender})
	}
	return actors
}

func webPreviewImages(page *html.Node) []domain.PreviewImage {
	images := []domain.PreviewImage{}
	gallery := htmlFind(page, atom.Div, "preview-images")
	if gallery == nil {
		return images
	}
	for link := range gallery.Descendants() {
		if link.DataAtom != atom.A || !htmlHasClass(link, "tile-item") {
			continue
		}
		if original := webImageURL(htmlAttr(link, "href")); original != "" {
			thumbnail := webImageURL(htmlAttr(htmlFind(link, atom.Img, ""), "src"))
			images = append(images, domain.PreviewImage{Thumbnail: thumbnail, Original: original})
		}
	}
	return images
}

func webRecommendations(page *html.Node, markers ...string) []domain.MovieReference {
	references := []domain.MovieReference{}
	for panel := range page.Descendants() {
		if panel.DataAtom != atom.Article || !htmlHasClass(panel, "video-panel") {
			continue
		}
		header := htmlText(htmlFind(panel, atom.Div, "message-header"))
		if !slices.ContainsFunc(markers, func(marker string) bool { return strings.Contains(header, marker) }) {
			continue
		}
		for link := range panel.Descendants() {
			if link.DataAtom != atom.A || !htmlHasClass(link, "tile-item") {
				continue
			}
			id := webEntityID(htmlAttr(link, "href"), "/v/")
			code := htmlText(htmlFind(link, atom.Div, "video-number"))
			if id != "" && code != "" {
				thumbnail := webImageURL(htmlAttr(htmlFind(link, atom.Img, ""), "src"))
				references = append(references, domain.MovieReference{ID: id, Code: code, Thumbnail: thumbnail})
			}
		}
		break
	}
	return references
}

func htmlFind(root *html.Node, tag atom.Atom, class string) *html.Node {
	if root == nil {
		return nil
	}
	for node := range root.Descendants() {
		if node.DataAtom == tag && (class == "" || htmlHasClass(node, class)) {
			return node
		}
	}
	return nil
}

func htmlHasClass(node *html.Node, class string) bool {
	return slices.Contains(strings.Fields(htmlAttr(node, "class")), class)
}

func htmlAttr(node *html.Node, key string) string {
	if node != nil {
		for _, attr := range node.Attr {
			if attr.Key == key {
				return attr.Val
			}
		}
	}
	return ""
}

func htmlText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var text strings.Builder
	for descendant := range node.Descendants() {
		if descendant.Type == html.TextNode {
			text.WriteString(descendant.Data)
		}
	}
	return strings.TrimSpace(text.String())
}
