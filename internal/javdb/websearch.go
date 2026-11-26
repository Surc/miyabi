package javdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

var webSearchBoxRe = regexp.MustCompile(`(?is)<a href="/v/([A-Za-z0-9]+)"[^>]*class="box".*?<div class="video-title"><strong>([^<]*)</strong>`)

type webSearchResult struct {
	id   string
	code string
}

// parseWebSearch extracts (id, code) pairs from an anonymous search page.
func parseWebSearch(body string) []webSearchResult {
	var results []webSearchResult
	seen := make(map[string]bool)
	for _, match := range webSearchBoxRe.FindAllStringSubmatch(body, -1) {
		id, code := strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
		if id == "" || code == "" || seen[id] {
			continue
		}
		seen[id] = true
		results = append(results, webSearchResult{id: id, code: code})
	}
	return results
}

// ResolveMovieID uses the signed search with a session, and the public search
// page for anonymous scraping.
func (c *Client) ResolveMovieID(ctx context.Context, number string) (string, error) {
	layers := codeid.Layers(number)
	if len(layers) == 0 {
		return "", errors.New("catalogue number is required")
	}

	var results []webSearchResult
	queried := make(map[string]bool)
	for level, candidates := range layers {
		for _, candidate := range candidates {
			for _, query := range codeid.Queries(candidate) {
				if id, err := matchWebResults(results, []string{candidate}, false); err != nil {
					return "", err
				} else if id != "" {
					return id, nil
				}
				if queried[query] {
					continue
				}
				queried[query] = true
				if c.HasSession() || c.fingerprint == nil {
					movies, err := c.Search(ctx, query, domain.SearchOptions{Zone: domain.ZoneAll, Page: 1, Limit: 100})
					if err != nil {
						return "", err
					}
					for _, movie := range movies {
						results = append(results, webSearchResult{id: movie.ID, code: movie.Code})
					}
				} else {
					body, err := c.fetchWebHTML(ctx, "/search", url.Values{"q": {query}, "f": {"all"}})
					if err != nil {
						return "", err
					}
					results = append(results, parseWebSearch(string(body))...)
				}
			}
		}
		for _, completed := range layers[:level+1] {
			for _, equivalent := range []bool{false, true} {
				id, err := matchWebResults(results, completed, equivalent)
				if err != nil || id != "" {
					return id, err
				}
			}
		}
	}
	return "", fmt.Errorf("catalogue number %s was not found on JavDB", layers[0][0])
}

func matchWebResults(results []webSearchResult, candidates []string, equivalent bool) (string, error) {
	ids := make(map[string]struct{})
	matched := ""
	for _, result := range results {
		for _, candidate := range candidates {
			var ok bool
			if equivalent {
				ok = codeid.IsFormatEquivalent(result.code, candidate)
			} else {
				ok = codeid.Normalize(result.code) == candidate
			}
			if ok {
				if _, exists := ids[result.id]; !exists {
					ids[result.id] = struct{}{}
					matched = result.id
				}
			}
		}
	}
	if len(ids) > 1 {
		kind := "exact"
		if equivalent {
			kind = "format-equivalent"
		}
		return "", fmt.Errorf("catalogue number %s has multiple %s JavDB matches", candidates[0], kind)
	}
	return matched, nil
}
