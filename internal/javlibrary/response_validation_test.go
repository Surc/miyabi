package javlibrary

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCommentsRejectPageChangesInsteadOfRepeatingEarlierComments(t *testing.T) {
	for _, test := range []struct {
		name, query, reason string
		page                int
	}{
		{"page rollback", "v=film&page=1", "comments_redirect_changed_page", 2},
		{"page advanced", "v=film&page=3", "comments_redirect_changed_page", 2},
		{"page omitted", "v=film", "comments_redirect_changed_page", 2},
		{"invalid page", "v=film&page=invalid", "comments_redirect_changed_page", 2},
		{"ambiguous page", "v=film&page=2&page=1", "comments_redirect_changed_page", 2},
		{"ambiguous film", "v=film&v=other&page=2", "comments_redirect_changed_identity", 2},
		{"first page canonicalized", "v=film", "", 1},
		{"same page", "v=film&page=2", "", 2},
		{"same numeric page", "v=film&page=02", "", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !r.URL.Query().Has("final") {
					http.Redirect(w, r, "/cn/videocomments.php?"+test.query+"&final=1", http.StatusFound)
					return
				}
				fmt.Fprint(w, `<div id="video_comments"><table class="comment" id="one"><tr><td class="t">Comment</td></tr></table></div>`)
			})
			page, err := c.Comments(t.Context(), "film", test.page)
			if test.reason != "" {
				requireDiagnostic(t, err, "comments", "identity", test.reason)
				return
			}
			if err != nil || page.Page != test.page || len(page.Items) != 1 {
				t.Fatalf("canonical same-page response rejected: %+v %v", page, err)
			}
		})
	}
}

func TestMalformedSearchCandidatesDoNotBecomeNotFound(t *testing.T) {
	for _, test := range []struct {
		name, body, reason string
	}{
		{"missing code node", `<div class="videos"><a href="?v=film"><span class="new-id">ABP-123</span></a></div>`, "search_identity_missing"},
		{"empty code", `<div class="videos"><a href="?v=film"><span class="id"></span></a></div>`, "search_identity_missing"},
		{"missing film target", `<div class="videos"><a href="?v="><span class="id">ABP-123</span></a></div>`, "search_link_invalid"},
		{"ambiguous film target", `<div class="videos"><a href="?v=film&v=other"><span class="id">ABP-123</span></a></div>`, "search_link_invalid"},
		{"partial list", `<div class="videos"><a href="?v=valid"><span class="id">ABP-123</span></a><a href="?v=unidentified"><span>Unknown code</span></a></div>`, "search_identity_missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := localClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, test.body) })
			_, err := c.Lookup(t.Context(), "ABP-123")
			requireDiagnostic(t, err, "search", "unexpected_page", test.reason)
		})
	}
}

func TestSearchPosterAndTitleCanIdentifyTheSameFilm(t *testing.T) {
	body := `<div class="videos"><a href="?v=film"><img src="poster.jpg"></a><a href="?v=film"><span class="id">ABP-123</span></a></div>`
	doc, _ := parseDocument([]byte(body))
	id, err := searchFilm(doc, "ABP-123")
	if err != nil || id != "film" {
		t.Fatalf("poster link prevented an exact match: %s %v", id, err)
	}
}

func TestCommentsNextPageRequiresUniqueFilmAndPage(t *testing.T) {
	for _, test := range []struct {
		name, link string
		more       bool
	}{
		{"query only", "?v=film&page=2", true},
		{"numeric page", "videocomments.php?v=film&page=02", true},
		{"ambiguous page", "videocomments.php?v=film&page=2&page=3", false},
		{"ambiguous film", "videocomments.php?v=film&v=other&page=2", false},
		{"different endpoint", "notvideocomments.php?v=film&page=2", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, _ := parseDocument([]byte(`<div id="video_comments"></div><a href="` + test.link + `">Next</a>`))
			page, err := parseComments(doc, "film", 1)
			if err != nil || page.HasMore != test.more {
				t.Fatalf("page=%+v error=%v", page, err)
			}
		})
	}
}

func TestCommentRowsWithoutBodyDoNotBecomeEmptyOrPartialSuccess(t *testing.T) {
	for _, prefix := range []string{"", `<table class="comment" id="valid"><tr><td class="t">Valid comment</td></tr></table>`} {
		c := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `<div id="video_comments">`+prefix+`<table class="comment" id="broken"><tr><td class="new-body">Comment</td></tr></table></div>`)
		})
		_, err := c.Comments(t.Context(), "film", 1)
		requireDiagnostic(t, err, "comments", "unexpected_page", "comment_body_missing")
	}
}

func TestUnbalancedRatingIsNotAcceptedAsNativeScore(t *testing.T) {
	for _, score := range []string{"(8.25", "8.25)"} {
		c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/cn/vl_searchbyid.php" {
				http.Redirect(w, r, "/cn/?v=film", http.StatusFound)
				return
			}
			fmt.Fprint(w, strings.Replace(filmHTML, "(8.25)", score, 1))
		})
		_, err := c.Lookup(t.Context(), "ABP-123")
		requireDiagnostic(t, err, "search", "parse_score", "invalid_score")
	}
}
