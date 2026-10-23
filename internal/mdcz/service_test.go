package mdcz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/metadata"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

func fixture(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Service{directory: t.TempDir(), slots: make(chan struct{}, 1),
		catalogue: func(context.Context, domain.MovieRef) (domain.MovieMetadata, error) {
			return domain.MovieMetadata{}, metadata.ErrNotFound
		},
		settings: Settings{Enabled: true, ServerURL: server.URL, APIKey: "secret", Position: "bottomLeft", Badges: true},
		client:   &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func TestCatalogueSupplementKeepsLinksAndRatingWithoutReplacingMDCzFields(t *testing.T) {
	s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"protocol": Protocol, "revision": EngineRevision, "detail": map[string]string{"code": "TEST-001", "title": "Translated title", "plot": "Introduction"}})
	})
	s.catalogue = func(context.Context, domain.MovieRef) (domain.MovieMetadata, error) {
		return domain.MovieMetadata{Detail: domain.MovieDetail{Movie: domain.Movie{Code: "TEST-001", ID: "catalogue-id", Title: "Native title", Rating: 4.2, RatingSource: "javdb", RatingMax: 5}}}, nil
	}
	result, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001"})
	if err != nil || result.Detail.ID != "catalogue-id" || result.Detail.Rating != 4.2 || result.Detail.RatingMax != 5 || result.Detail.Title != "Translated title" || result.Detail.Plot != "Introduction" || len(result.Detail.Sources) != 2 {
		t.Fatalf("catalogue compatibility lost: %+v %v", result, err)
	}
	if _, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001", JavDBID: "different"}); !domain.IsKind(err, domain.KindConflict) {
		t.Fatalf("identity mismatch accepted: %v", err)
	}
	s.catalogue = func(context.Context, domain.MovieRef) (domain.MovieMetadata, error) {
		return domain.MovieMetadata{}, errors.New("catalogue temporarily unavailable")
	}
	if _, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001", Refresh: true}); err == nil {
		t.Fatal("refresh accepted a failed catalogue supplement")
	}
	if _, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001"}); err != nil {
		t.Fatalf("new metadata cannot tolerate optional catalogue failure: %v", err)
	}
}

func TestPartialRefreshCannotReplaceExistingMetadata(t *testing.T) {
	s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"protocol": Protocol, "revision": EngineRevision, "partial": true, "detail": map[string]string{"code": "TEST-001", "title": "Partial title"}})
	})
	if _, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001", Refresh: true}); err == nil {
		t.Fatal("partial refresh was accepted")
	}
}

func TestMetadataChecksProtocolIdentityAndPreservesKnownCatalogueID(t *testing.T) {
	for _, scenario := range []struct {
		code, revision string
		valid          bool
	}{
		{"TEST-001", EngineRevision, true}, {"TEST-002", EngineRevision, false}, {"TEST-001", "different", false},
	} {
		s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer secret" || r.URL.RawQuery != "" {
				t.Error("credential contract changed")
			}
			json.NewEncoder(w).Encode(map[string]any{"protocol": Protocol, "revision": scenario.revision, "detail": map[string]string{"code": scenario.code, "title": "Fixture", "plot": "Saved synopsis"}, "images": []any{}})
		})
		value, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001", JavDBID: "known"})
		if !scenario.valid {
			if err == nil {
				t.Fatal("accepted incompatible metadata")
			}
			continue
		}
		if err != nil || value.Detail.ID != "known" || value.Detail.Plot != "Saved synopsis" {
			t.Fatalf("%+v %v", value, err)
		}
		cached, err := s.Synopsis(t.Context(), "TEST-001")
		if err != nil || cached.Plot != value.Detail.Plot {
			t.Fatalf("lost cached synopsis: %+v %v", cached, err)
		}
	}
}

func TestBridgeErrorsRedirectsAndCanceledWaitsDoNotLeakSecrets(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusFound, http.StatusServiceUnavailable} {
		s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "http://other.invalid/secret")
			w.WriteHeader(status)
			w.Write([]byte("secret upstream diagnostic"))
		})
		_, err := s.Resolve(t.Context(), domain.MovieRef{Code: "TEST-001"})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%v", err)
		}
		encoded, _ := json.Marshal(s.ExtensionSettings())
		if strings.Contains(string(encoded), "secret") {
			t.Fatal("settings exposed key")
		}
		s.slots <- struct{}{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = s.Resolve(ctx, domain.MovieRef{Code: "TEST-001"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queue ignored cancellation: %v", err)
		}
		<-s.slots
	}
}

func TestPosterAlwaysUsesOriginalAndReplaysOffline(t *testing.T) {
	encode := func(shade uint8) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 4, 6))
		for y := range 6 {
			for x := range 4 {
				img.Set(x, y, color.RGBA{shade, shade, shade, 255})
			}
		}
		var body bytes.Buffer
		if err := jpeg.Encode(&body, img, nil); err != nil {
			t.Fatal(err)
		}
		return body.Bytes()
	}
	original, markedImage := encode(0), encode(255)
	requests := 0
	s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		var input struct {
			Poster    []byte   `json:"poster"`
			Filenames []string `json:"filenames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if !bytes.Equal(input.Poster, original) || len(input.Filenames) != 1 {
			t.Errorf("marked source or missing evidence: %+v", input)
		}
		json.NewEncoder(w).Encode(map[string]any{"protocol": Protocol, "revision": EngineRevision, "poster": markedImage, "labels": []string{"中字"}})
	})
	files := []pan.File{{Name: "TEST-001-C.mp4"}}
	doc, marked, err := s.Decorate(t.Context(), nfo.Movie{Code: "TEST-001", Plot: "Synopsis"}, files, original)
	if err != nil || !bytes.Equal(marked, markedImage) || doc.ArtworkExtension == nil {
		t.Fatalf("%+v %v", doc, err)
	}
	_, again, err := s.Decorate(t.Context(), doc, files, marked)
	if err != nil || !bytes.Equal(again, markedImage) || requests != 1 {
		t.Fatalf("replay reprocessed artwork: %d %v", requests, err)
	}
	s.settings.Position = "topRight"
	doc, _, err = s.Decorate(t.Context(), doc, files, marked)
	if err != nil || requests != 2 {
		t.Fatal("style did not regenerate")
	}
	s.settings.Badges = false
	doc, restored, err := s.Decorate(t.Context(), doc, files, marked)
	if err != nil || !bytes.Equal(restored, original) || len(doc.Tags) != 0 || requests != 2 || doc.Plot != "Synopsis" {
		t.Fatalf("disable failed: %+v %v", doc, err)
	}
	os.Remove(s.directory + "/original-" + doc.ArtworkExtension.Original + ".jpg")
	if _, _, err := s.Decorate(t.Context(), doc, files, marked); err == nil {
		t.Fatal("missing original silently stacked badges")
	}
}
