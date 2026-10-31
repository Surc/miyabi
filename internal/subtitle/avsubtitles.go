package subtitle

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/ppxb/miyabi/internal/codeid"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/net/html"
)

const defaultAVSubtitlesOrigin = "https://www.avsubtitles.com"

type AVSubtitlesProvider struct {
	origin string
	client *resty.Client
}

func NewAVSubtitlesProvider(proxyManager *netx.ProxyManager) *AVSubtitlesProvider {
	return &AVSubtitlesProvider{
		origin: defaultAVSubtitlesOrigin,
		client: netx.NewSafeRestyClient(proxyManager, 15*time.Second),
	}
}

func (p *AVSubtitlesProvider) Search(ctx context.Context, code string) ([]Candidate, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	query := url.Values{"search": {code}, "language": {"zh"}}
	body, err := p.page(ctx, p.origin+"/search_results.php?"+query.Encode())
	if err != nil {
		return nil, err
	}
	var candidates []Candidate
	var errs []error
	var details int
	for _, movie := range parseAVSubtitlesSearch(body, p.origin) {
		if !identifies(movie.name, code) {
			continue
		}
		body, err := p.page(ctx, movie.url)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, detailURL := range parseAVSubtitlesChineseLinks(body, movie.url) {
			// Bound requests even when one work has many subtitle revisions.
			if details == 5 {
				return candidates, errors.Join(errs...)
			}
			details++
			body, err := p.page(ctx, detailURL)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if candidate, ok := parseAVSubtitlesDetail(body, movie.name, p.origin); ok {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates, errors.Join(append(errs, ctx.Err())...)
}

func (p *AVSubtitlesProvider) page(ctx context.Context, target string) ([]byte, error) {
	resp, err := p.client.R().SetContext(ctx).Get(target)
	if err != nil {
		return nil, fmt.Errorf("avsubtitles request: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("avsubtitles status %d", resp.StatusCode())
	}
	return resp.Body(), nil
}

type avsubtitlesMovie struct {
	name string
	url  string
}

func parseAVSubtitlesSearch(body []byte, origin string) []avsubtitlesMovie {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var movies []avsubtitlesMovie
	seen := make(map[string]bool)
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode || node.Data != "h5" {
			continue
		}
		for link := range node.Descendants() {
			if link.Type != html.ElementNode || link.Data != "a" {
				continue
			}
			target := avsubtitlesURL(origin, getAttr(link, "href"))
			if target != "" && strings.HasPrefix(getAttr(link, "href"), "/movie") && !seen[target] {
				seen[target] = true
				movies = append(movies, avsubtitlesMovie{name: extractText(link), url: target})
			}
		}
	}
	return movies
}

func parseAVSubtitlesChineseLinks(body []byte, movieURL string) []string {
	movie, err := url.Parse(movieURL)
	if err != nil {
		return nil
	}
	// Slugs may change on a canonical redirect; the movie ID must stay the same.
	prefix := "/" + strings.Split(strings.Trim(movie.Path, "/"), "/")[0] + "/"
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var links []string
	seen := make(map[string]bool)
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode || node.Data != "table" || getAttr(node, "id") != "subtitles_list" {
			continue
		}
		for link := range node.Descendants() {
			if link.Type != html.ElementNode || link.Data != "a" {
				continue
			}
			target := avsubtitlesURL(movieURL, getAttr(link, "href"))
			parsed, err := url.Parse(target)
			if err == nil && target != "" && strings.HasPrefix(parsed.Path, prefix) &&
				strings.Contains(parsed.Path, "/subtitles/zh/") && !seen[target] {
				seen[target] = true
				links = append(links, target)
			}
		}
	}
	return links
}

func parseAVSubtitlesDetail(body []byte, movieName, origin string) (Candidate, bool) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return Candidate{}, false
	}
	var downloadURL, filename string
	movieCode, _ := codeid.Parse(movieName)
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode {
			continue
		}
		if node.Data == "form" && getAttr(node, "action") == "/download_page.php" {
			params := make(url.Values)
			for input := range node.Descendants() {
				if input.Type == html.ElementNode && input.Data == "input" {
					if name := getAttr(input, "name"); name == "subid" || name == "revid" {
						params.Set(name, getAttr(input, "value"))
					}
				}
			}
			if params.Get("subid") != "" && params.Get("revid") != "" {
				downloadURL = origin + "/download_page.php?" + params.Encode()
			}
		}
		if hasClass(node, "subtitles_content_label") && extractText(node) == "Archive content:" {
			for file := range node.Parent.Descendants() {
				if file.Type == html.ElementNode && file.Data == "span" && hasClass(file, "text-mono") {
					name := extractText(file)
					if fileCode, ok := codeid.Parse(path.Base(name)); ok && !codeid.IsEquivalent(fileCode, movieCode) {
						continue
					}
					if subtitlemeta.Format(path.Ext(name)) != "" {
						filename = name
						break
					}
				}
			}
		}
	}
	if downloadURL == "" || filename == "" {
		return Candidate{}, false
	}
	name := movieName + " " + filename
	return Candidate{
		Provider: "AVSubtitles", Name: name, URL: downloadURL,
		Format: subtitlemeta.Format(path.Ext(filename)), Language: subtitlemeta.LanguageHint(filename),
		Version:  subtitlemeta.DetectVersion(name),
		download: &avsubtitlesDownload{filename: filename, delay: 7 * time.Second},
	}, true
}

// Only follow the site's own links; adverts and external mirrors are not sources.
func avsubtitlesURL(base, href string) string {
	origin, err := url.Parse(base)
	if err != nil || href == "" {
		return ""
	}
	target, err := origin.Parse(href)
	if err != nil || target.Scheme != origin.Scheme || target.Host != origin.Host {
		return ""
	}
	return target.String()
}

type avsubtitlesDownload struct {
	filename string
	delay    time.Duration
}

func (d *avsubtitlesDownload) fetch(ctx context.Context, client *http.Client, candidate Candidate) ([]byte, error) {
	// A separate anonymous session per download keeps concurrent revisions apart.
	session := *client
	session.Jar, _ = cookiejar.New(nil)
	body, err := netx.SafeDownload(ctx, &session, candidate.URL)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("avsubtitles download page: %w", err)
	}
	var target string
	for node := range doc.Descendants() {
		if node.Type == html.ElementNode && node.Data == "a" && getAttr(node, "id") == "bigbutton" {
			target = avsubtitlesURL(candidate.URL, getAttr(node, "href"))
			break
		}
	}
	if target == "" {
		return nil, fmt.Errorf("avsubtitles download link missing")
	}
	// Respect the site's download countdown without running its scripts.
	timer := time.NewTimer(d.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	body, err = netx.SafeDownload(ctx, &session, target)
	if err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("avsubtitles archive: %w", err)
	}
	for _, file := range archive.File {
		if file.Name != d.filename {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		body, err := io.ReadAll(io.LimitReader(stream, maxSize+1))
		if err != nil {
			return nil, err
		}
		if len(body) > maxSize {
			return nil, fmt.Errorf("avsubtitles extracted subtitle exceeds size limit")
		}
		return body, nil
	}
	return nil, fmt.Errorf("avsubtitles archive missing %q", d.filename)
}
