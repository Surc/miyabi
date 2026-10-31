package javlibrary

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestLookupCurrentRatedAndUnratedPages(t *testing.T) {
	for _, test := range []struct {
		code, id string
		score    float64
	}{
		{"FSDSS-437", "javme4ivte", 8.4},
		{"IPZZ-982", "javme33a5a", 0},
	} {
		for _, route := range []string{"direct", "search", "legacy redirect"} {
			t.Run(test.code+"/"+route, func(t *testing.T) {
				body, err := os.ReadFile("testdata/" + strings.ToLower(test.code) + ".html")
				if err != nil {
					t.Fatal(err)
				}
				client := nativeRatingClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/cn/vl_searchbyid.php":
						if r.URL.Query().Get("keyword") != test.code {
							t.Errorf("unexpected search: %s", r.URL)
						}
						switch route {
						case "direct":
							http.Redirect(w, r, "/cn/"+test.id+".html", http.StatusFound)
						case "search":
							fmt.Fprintf(w, `<div class="videos"><a href="/cn/%s.html"><span class="id">%s</span></a></div>`, test.id, test.code)
						case "legacy redirect":
							fmt.Fprintf(w, `<div class="videos"><a href="?v=%s"><span class="id">%s</span></a></div>`, test.id, test.code)
						}
					case "/cn/":
						if route != "legacy redirect" || r.URL.Query().Get("v") != test.id {
							t.Errorf("unexpected legacy request: %s", r.URL)
						}
						http.Redirect(w, r, "/cn/"+test.id+".html", http.StatusFound)
					case "/cn/" + test.id + ".html":
						_, _ = w.Write(body)
					default:
						t.Errorf("unexpected request: %s", r.URL)
						w.WriteHeader(http.StatusNotFound)
					}
				})
				rating, err := client.Rating(t.Context(), test.code)
				if err != nil || !rating.Found || rating.Source != "javlibrary" || rating.Max != 10 {
					t.Fatalf("rating=%+v err=%v", rating, err)
				}
				if test.score == 0 {
					if rating.Score != nil {
						t.Fatalf("unrated release received a score: %v", *rating.Score)
					}
				} else if rating.Score == nil || *rating.Score != test.score {
					t.Fatalf("wrong native score: %+v", rating)
				}
			})
		}
	}
}

func TestCanonicalFilmIdentityRejectsConflictingQueries(t *testing.T) {
	for _, test := range []struct{ address, id string }{
		{"?v=javme4ivte", "javme4ivte"},
		{"javme4ivte.html", "javme4ivte"},
		{"/cn/javme4ivte.html", "javme4ivte"},
		{"/ja/javme4ivte.html?lang=ja", "javme4ivte"},
		{"/cn/javme4ivte.html?v=javme4ivte", "javme4ivte"},
		{"/cn/javme4ivte.html?v=other", ""},
		{"/cn/javme4ivte.html?v=javme4ivte&v=javme4ivte", ""},
		{"/cn/javme4ivte.html?v=", ""},
		{"/cn/javme4ivte.html?unused=%zz", ""},
		{"/cn/javme4ivte.html?v=javme4ivte;v=other", ""},
		{"/other/javme4ivte.html", ""},
		{"/cn/javme4ivte.html/extra", ""},
		{"/cn/redirect.html", ""},
	} {
		t.Run(test.address, func(t *testing.T) {
			u, err := url.Parse(test.address)
			if err != nil {
				t.Fatal(err)
			}
			if got := filmIDFromURL(u); got != test.id {
				t.Fatalf("got %q, want %q", got, test.id)
			}
		})
	}
}

func TestSearchCanonicalAliasesPreserveExactIdentity(t *testing.T) {
	for _, test := range []struct {
		links string
		bad   bool
	}{
		{`<a href="?v=javme4ivte"><span class="id">FSDSS-437</span></a><a href="/cn/javme4ivte.html"><span class="id">FSDSS-437</span></a>`, false},
		{`<a href="/cn/javme4ivte.html"><img src="poster.jpg"></a><a href="/cn/javme4ivte.html"><span class="id">FSDSS-437</span></a>`, false},
		{`<a href="/cn/javme4ivte.html?v=other"><span class="id">FSDSS-437</span></a>`, true},
		{`<a href="/cn/javme4ivte.html"><span class="id">FSDSS-437</span></a><a href="/cn/javother.html"><span class="id">FSDSS-437</span></a>`, true},
	} {
		doc, _ := parseDocument([]byte(`<div class="videos">` + test.links + `</div>`))
		target, err := searchFilm(doc, "FSDSS-437")
		if (err != nil) != test.bad || (!test.bad && (target.id != "javme4ivte" || !target.html)) {
			t.Fatalf("target=%+v err=%v", target, err)
		}
	}
}

func TestUnratedPageStillRequiresCompleteTemplateAndMatchingCode(t *testing.T) {
	body, err := os.ReadFile("testdata/ipzz-982.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		strings.ReplaceAll(string(body), "IPZZ-982", "IPZZ-9820"),
		strings.ReplaceAll(string(body), `id="video_info"`, `id="missing"`),
		strings.ReplaceAll(string(body), `id="video_reviews"`, `id="missing"`),
	} {
		doc, _ := parseDocument([]byte(invalid))
		if film, err := parseFilm(doc, "javme33a5a", "IPZZ-982"); err == nil || film.Rating.Score != nil {
			t.Fatalf("invalid detail accepted: %+v %v", film, err)
		}
	}
}

func TestCanonicalDetailRedirectCannotChangeFilm(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cn/vl_searchbyid.php":
			fmt.Fprint(w, `<div class="videos"><a href="/cn/javme4ivte.html"><span class="id">FSDSS-437</span></a></div>`)
		case "/cn/javme4ivte.html":
			http.Redirect(w, r, "/cn/javother.html", http.StatusFound)
		default:
			fmt.Fprint(w, strings.ReplaceAll(filmHTML, "ABP-123", "FSDSS-437"))
		}
	})
	_, err := client.Lookup(t.Context(), "FSDSS-437")
	requireDiagnostic(t, err, "detail", "identity", "film_redirect_changed_identity")
}
