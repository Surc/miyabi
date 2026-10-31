package subtitle

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
)

func TestDownloadUsesSettingsSnapshotWithoutChangingLibraryOrConfig(t *testing.T) {
	service, db, _, target := exportFixture(t, map[string]string{"/bad.srt": "<html>error</html>", "/good.srt": traditionalSRT},
		Candidate{Provider: "坏来源", Name: "ABP-123.cht.srt", URL: "/bad.srt", Format: "srt", Language: subtitlemeta.LangTraditionalChinese, Version: subtitlemeta.VersionStandard},
		Candidate{Provider: "SubtitleCat", Name: "ABP-123.cht.srt", URL: "/good.srt", Format: "srt", Language: subtitlemeta.LangTraditionalChinese, Version: subtitlemeta.VersionStandard})
	saved := DefaultConfig()
	saved.Enabled = false
	if _, err := service.UpdateConfig(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	candidate := saved
	candidate.Language = subtitlemeta.LangTraditionalChinese
	result, err := service.TestDownload(t.Context(), TestRequest{Code: "abp-123", Config: &candidate})
	if err != nil || result.Status != "downloaded" || result.Code != "ABP-123" || result.Source != "SubtitleCat" ||
		result.Language != subtitlemeta.LangTraditionalChinese || result.Format != "srt" || result.Bytes != len(utf8BOM)+len(traditionalSRT) || result.Candidates != 2 {
		t.Fatalf("download test = %+v, %v", result, err)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "no timed cues") {
		t.Fatalf("invalid subtitle was not reported: %+v", result.Errors)
	}
	if db.Subtitle.Query().CountX(t.Context()) != 0 {
		t.Fatal("test download added a library subtitle")
	}
	if _, err := os.Stat(target.Dir); !os.IsNotExist(err) {
		t.Fatal("test download wrote a library directory")
	}
	if got, err := service.Config(t.Context()); err != nil || got != saved || candidate.Enabled {
		t.Fatalf("test changed config: saved=%+v candidate=%+v error=%v", got, candidate, err)
	}
}

type failedSearchProvider struct{}

func (failedSearchProvider) Search(context.Context, string) ([]Candidate, error) {
	return nil, errors.New("source unavailable")
}

func TestDownloadDistinguishesNoMatchesFailuresAndInvalidRequests(t *testing.T) {
	service, _, _, _ := exportFixture(t, nil)
	result, err := service.TestDownload(t.Context(), TestRequest{Code: "ABP-123"})
	if err != nil || result.Status != "not_found" || result.Candidates != 0 {
		t.Fatalf("missing subtitle = %+v, %v", result, err)
	}
	service.finder.providers = []Provider{failedSearchProvider{}}
	result, err = service.TestDownload(t.Context(), TestRequest{Code: "ABP-123"})
	if err != nil || result.Status != "failed" || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "source unavailable") {
		t.Fatalf("failed search = %+v, %v", result, err)
	}
	for _, code := range []string{"", "not a catalogue number"} {
		if _, err := service.TestDownload(t.Context(), TestRequest{Code: code}); !domain.IsKind(err, domain.KindInvalid) {
			t.Fatalf("invalid code %q error = %v", code, err)
		}
	}
	config := DefaultConfig()
	config.Sources = Sources{}
	if _, err := service.TestDownload(t.Context(), TestRequest{Code: "ABP-123", Config: &config}); !domain.IsKind(err, domain.KindInvalid) {
		t.Fatalf("all disabled sources error = %v", err)
	}
	config = DefaultConfig()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = service.TestDownload(ctx, TestRequest{Code: "ABP-123", Config: &config})
	if err != nil || result.Status != "failed" || !strings.Contains(strings.Join(result.Errors, " "), "context canceled") {
		t.Fatalf("canceled probe = %+v, %v", result, err)
	}
}
