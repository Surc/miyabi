package javlibrary

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// Classify only clear error-page markers outside a recognizable site template.
// Normal titles and comments mentioning these words must never become errors.
func pageProblem(doc *html.Node) string {
	if byID(doc, "video_id") != nil || byClass(doc, "videos") != nil || byID(doc, "video_comments") != nil || byID(doc, "video_review") != nil {
		return ""
	}
	text := func(tag string) string {
		n := find(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == tag })
		return strings.Trim(strings.ToLower(strings.Join(strings.Fields(plain(n)), " ")), " .!…")
	}
	title, heading := text("title"), text("h1")
	for _, value := range []string{title, heading} {
		if value == "site unavailable" || value == "service unavailable" {
			return "site_unavailable"
		}
	}
	if title == "just a moment" && (byID(doc, "challenge-form") != nil || byID(doc, "cf-challenge-running") != nil) {
		return "challenge_page"
	}
	if title == "attention required! | cloudflare" && byID(doc, "cf-error-details") != nil {
		return "challenge_page"
	}
	return ""
}

func pageShape(doc *html.Node) string {
	comments := byID(doc, "video_comments")
	rows, texts := 0, 0
	walk(comments, func(n *html.Node) {
		if n.Data == "table" && hasClass(n, "comment") {
			rows++
			if byClass(n, "t") != nil {
				texts++
			}
		}
	})
	return fmt.Sprintf("videos:%t,id:%t,review:%t,score:%t,comments:%t,rows:%d,text_nodes:%d",
		byClass(doc, "videos") != nil, byID(doc, "video_id") != nil,
		byID(doc, "video_review") != nil, byClass(byID(doc, "video_review"), "score") != nil,
		comments != nil, rows, texts)
}
