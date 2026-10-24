package javlibrary

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"golang.org/x/net/html"
)

func parseDocument(body []byte) (*html.Node, error) { return html.Parse(bytes.NewReader(body)) }
func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}
func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if match(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if hit := find(child, match); hit != nil {
			return hit
		}
	}
	return nil
}
func byID(n *html.Node, id string) *html.Node {
	return find(n, func(n *html.Node) bool { return attr(n, "id") == id })
}
func byClass(n *html.Node, class string) *html.Node {
	return find(n, func(n *html.Node) bool { return hasClass(n, class) })
}
func walk(n *html.Node, visit func(*html.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

// Drop active/hidden markup and preserve line breaks; clients render this as text.
func plain(n *html.Node) string {
	if n == nil || n.Data == "script" || n.Data == "style" {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Data == "br" {
		return "\n"
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(plain(c))
		if c.Data == "p" || c.Data == "div" {
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func searchFilm(doc *html.Node, code string) (string, error) {
	list := byClass(doc, "videos")
	if list == nil {
		return "", failure("unexpected_page", "search_container_missing")
	}
	ids := map[string]bool{}
	identified := map[string]bool{}
	var invalid error
	walk(list, func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		candidate := codeid.Normalize(plain(byClass(n, "id")))
		u, err := url.Parse(attr(n, "href"))
		if err != nil {
			if candidate == code {
				invalid = failure("unexpected_page", "search_link_invalid")
			}
			return
		}
		if u.IsAbs() || u.Host != "" {
			return
		}
		id := filmIDFromQuery(u.Query())
		if id != "" {
			// A poster link may omit the code when another link for the same
			// film supplies it; only genuinely unidentified candidates fail.
			identified[id] = identified[id] || candidate != ""
		}
		if candidate != code {
			return
		}
		if id == "" {
			invalid = failure("unexpected_page", "search_link_invalid")
			return
		}
		ids[id] = true
	})
	if invalid != nil {
		return "", invalid
	}
	for _, known := range identified {
		if !known {
			return "", failure("unexpected_page", "search_identity_missing")
		}
	}
	if len(ids) > 1 {
		return "", failure("identity", "ambiguous_film")
	}
	for id := range ids {
		return id, nil
	}
	return "", nil
}

// Repeated v parameters cannot establish a unique film identity.
func filmIDFromQuery(query url.Values) string {
	ids := query["v"]
	if len(ids) != 1 || !filmID.MatchString(ids[0]) {
		return ""
	}
	return ids[0]
}

func commentPageFromQuery(query url.Values) (int, bool) {
	pages, present := query["page"]
	if !present {
		return 1, true
	}
	if len(pages) != 1 {
		return 0, false
	}
	page, err := strconv.Atoi(pages[0])
	return page, err == nil && page >= 1 && page <= 1000
}

var ratingNumber = regexp.MustCompile(`^(\d+(?:\.\d+)?)$`)

func parseFilm(doc *html.Node, id, code string) (Film, error) {
	film := Film{ID: id, Rating: domain.ExternalRating{Source: "javlibrary", Max: 10, Found: true}}
	if !filmID.MatchString(id) || codeid.Normalize(plain(byClass(byID(doc, "video_id"), "text"))) != code {
		return Film{}, failure("identity", "film_identity_mismatch")
	}
	if byID(doc, "video_review") == nil {
		return Film{}, failure("unexpected_page", "review_container_missing")
	}
	score := plain(byClass(byID(doc, "video_review"), "score"))
	if score == "" {
		return film, nil
	}
	if strings.HasPrefix(score, "(") && strings.HasSuffix(score, ")") {
		score = strings.TrimSpace(score[1 : len(score)-1])
	}
	match := ratingNumber.FindStringSubmatch(score)
	if match == nil {
		return Film{}, failure("parse_score", "invalid_score")
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value < 0 || value > 10 {
		return Film{}, failure("parse_score", "invalid_score")
	}
	film.Rating.Score = &value
	return film, nil
}

var commentDate = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2}(?::\d{2})?)?`)

func parseComments(doc *html.Node, id string, page int) (domain.MovieComments, error) {
	result := domain.MovieComments{Source: "javlibrary", Page: page, Items: []domain.MovieComment{}}
	container := byID(doc, "video_comments")
	if container == nil {
		return result, failure("unexpected_page", "comments_container_missing")
	}
	var invalid error
	walk(container, func(n *html.Node) {
		if invalid != nil || n.Data != "table" || !hasClass(n, "comment") {
			return
		}
		body := byClass(n, "t")
		if body == nil {
			invalid = failure("unexpected_page", "comment_body_missing")
			return
		}
		content := plain(body)
		if content == "" {
			return
		}
		authorNode := find(byClass(n, "info"), func(n *html.Node) bool { return n.Data == "a" && strings.Contains(attr(n, "href"), "user.php?") })
		author := plain(authorNode)
		date := commentDate.FindString(plain(byClass(n, "date")))
		key := attr(n, "id")
		if key == "" {
			key = fmt.Sprintf("%x", sha256.Sum256([]byte(author+"\x00"+date+"\x00"+content)))
		}
		// Helpful votes on JavLibrary are not per-user film ratings.
		result.Items = append(result.Items, domain.MovieComment{ID: key, Author: author, Content: content, CreatedAt: date})
	})
	if invalid != nil {
		return result, invalid
	}
	walk(doc, func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		u, err := url.Parse(attr(n, "href"))
		if err != nil || u.IsAbs() || u.Host != "" {
			return
		}
		query := u.Query()
		next, valid := commentPageFromQuery(query)
		if filmIDFromQuery(query) == id && valid && next == page+1 && (u.Path == "" || path.Base(u.Path) == "videocomments.php") && page < 1000 {
			result.HasMore = true
		}
	})
	return result, nil
}
