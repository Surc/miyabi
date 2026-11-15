package javdb

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"golang.org/x/net/html/atom"
)

// MovieED2K reads file links from the public film page's resource list.
func (c *Client) MovieED2K(ctx context.Context, movieID string) ([]domain.ED2KResource, error) {
	if !ValidMovieID(movieID) {
		return nil, domain.E(domain.KindInvalid, "无效的 JavDB 影片标识", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := c.waitRequest(ctx, time.Now().Add(maxRateLimitWait)); err != nil {
		return nil, err
	}
	body, err := fetchWebMovieHTML(ctx, c.fingerprint, movieID)
	if err != nil {
		c.deferWebRateLimit(err)
		return nil, err
	}
	return parseWebED2K(body, movieID)
}

func parseWebED2K(body []byte, movieID string) ([]domain.ED2KResource, error) {
	document, err := parseWebMovieDocument(body, movieID)
	if err != nil {
		return nil, err
	}
	list := htmlFind(document, atom.Div, "magnet-links")
	if list == nil || htmlAttr(list, "id") != "magnets-content" {
		return nil, errors.New("JavDB web page did not contain the film resource list")
	}

	resources := []domain.ED2KResource{}
	for row := range list.ChildNodes() {
		if row.DataAtom != atom.Div || !htmlHasClass(row, "item") {
			continue
		}
		link := htmlFind(htmlFind(row, atom.Div, "magnet-name"), atom.A, "")
		resource, ok := domain.ParseED2K(strings.TrimSpace(htmlAttr(link, "href")))
		if !ok {
			continue
		}
		resource.Source = domain.ED2KSourceJavDB
		resource.CreatedAt = htmlText(htmlFind(row, atom.Span, "time"))
		if tags := htmlFind(row, atom.Div, "tags"); tags != nil {
			for badge := range tags.ChildNodes() {
				switch htmlText(badge) {
				case "高清":
					resource.Tags = append(resource.Tags, domain.MagnetTagHD)
				case "字幕":
					resource.Tags = append(resource.Tags, domain.MagnetTagSubtitle)
				}
			}
		}
		resources = append(resources, resource)
	}
	return resources, nil
}
