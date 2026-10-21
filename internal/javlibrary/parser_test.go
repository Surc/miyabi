package javlibrary

import (
	"strings"
	"testing"
)

const filmHTML = `<div id="video_id"><span class="text">ABP-123</span></div><div id="video_review"><span class="score">(8.25)</span></div>`

func TestExactFilmIdentityAndNativeScale(t *testing.T) {
	doc, _ := parseDocument([]byte(filmHTML))
	film, err := parseFilm(doc, "abc123", "ABP-123")
	if err != nil || !film.Rating.Found || film.Rating.Max != 10 || film.Rating.Score == nil || *film.Rating.Score != 8.25 {
		t.Fatalf("film=%+v err=%v", film, err)
	}
	for _, test := range []struct {
		name, body, code string
		valid            bool
	}{
		{"mismatch", filmHTML, "ABP-12", false},
		{"challenge", `<h1>Just a moment</h1>`, "ABP-123", false},
		{"invalid score", strings.ReplaceAll(filmHTML, "8.25", "18.5"), "ABP-123", false},
		{"unrated", strings.ReplaceAll(filmHTML, `<span class="score">(8.25)</span>`, ""), "ABP-123", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, _ := parseDocument([]byte(test.body))
			film, err := parseFilm(doc, "abc123", test.code)
			if (err == nil) != test.valid {
				t.Fatalf("film=%+v err=%v", film, err)
			}
			if test.valid && film.Rating.Score != nil {
				t.Fatal("unrated score should be nil")
			}
		})
	}
}

func TestSearchRejectsAmbiguousAndPartialCodes(t *testing.T) {
	for _, test := range []struct {
		body, want string
		bad        bool
	}{
		{`<div class="videos"><a href="?v=one"><span class="id">ABP-1234</span></a><a href="?v=two"><span class="id">ABP-123</span></a></div>`, "two", false},
		{`<div class="videos"><a href="?v=one"><span class="id">ABP-123</span></a><a href="?v=two"><span class="id">ABP-123</span></a></div>`, "", true},
		{`<div class="videos"></div>`, "", false},
		{`<h1>Site Unavailable</h1>`, "", true},
		{`<div class="videos"><a href="https://evil.example/?v=one"><span class="id">ABP-123</span></a></div>`, "", false},
	} {
		doc, _ := parseDocument([]byte(test.body))
		got, err := searchFilm(doc, "ABP-123")
		if got != test.want || (err != nil) != test.bad {
			t.Fatalf("got=%s err=%v", got, err)
		}
	}
}

func TestCommentsArePlainTextAndPaginationStaysOnFilm(t *testing.T) {
	body := `<div id="video_comments"><table class="comment" id="comment-1"><tr><td class="info"><a href="user.php?u=reader">Reader</a></td><td class="date">2026-10-21 08:30:00</td></tr><tr><td class="t">One<br>Two &amp; three<script>alert(1)</script></td></tr><tr><td>Helpful: 20</td></tr></table></div><a href="videocomments.php?v=film&amp;page=2">next</a>`
	doc, _ := parseDocument([]byte(body))
	page, err := parseComments(doc, "film", 1)
	if err != nil || len(page.Items) != 1 || !page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	c := page.Items[0]
	if c.ID != "comment-1" || c.Content != "One\nTwo & three" || c.Author != "Reader" || c.CreatedAt != "2026-10-21 08:30:00" || c.Rating != nil || c.Likes != nil {
		t.Fatalf("comment=%+v", c)
	}
	for _, link := range []string{"videocomments.php?v=other&amp;page=2", "https://evil.example/videocomments.php?v=film&amp;page=2"} {
		doc, _ := parseDocument([]byte(strings.Replace(body, "videocomments.php?v=film&amp;page=2", link, 1)))
		p, _ := parseComments(doc, "film", 1)
		if p.HasMore {
			t.Fatal("foreign next link accepted")
		}
	}
	doc, _ = parseDocument([]byte(`<h1>Challenge</h1>`))
	if _, err := parseComments(doc, "film", 1); err == nil {
		t.Fatal("challenge became empty success")
	}
}
