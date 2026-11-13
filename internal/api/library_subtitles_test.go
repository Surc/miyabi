package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/library"
	"github.com/ppxb/miyabi/internal/subtitle"
)

type librarySubtitleProvider struct {
	*subtitle.Service
	body string
}

func (p librarySubtitleProvider) DownloadMissing(_ context.Context, target subtitlemeta.Target, _ map[subtitlemeta.Kind]bool, save func(subtitlemeta.Track, []byte) (bool, error)) (int, error) {
	track := subtitlemeta.Track{Source: "AVSubtitles", Kind: subtitlemeta.Kind{
		Language: subtitlemeta.LangTraditionalChinese, Version: target.Version, Format: "srt",
	}}
	saved, err := save(track, []byte(p.body))
	if saved {
		return 1, err
	}
	return 0, err
}

func TestLibrarySubtitleEndpointsManageOnlyTheSelectedVideo(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	film := store.Client.Movie.Create().SetCode("ABP-123").SaveX(ctx)
	const srt = "1\n00:00:01,000 --> 00:00:02,000\n字幕測試\n"
	for _, name := range []string{"ABP-123.mp4", "ABP-123.4k.mp4", "ABP-123.4k.zh-TW.srt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(srt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	addVideo := func(name, id, account string) int {
		return store.Client.File.Create().SetFileID(id).SetName(name).SetPath(name).SetSize(1024).
			SetAccountID(account).SetMovieID(film.ID).SaveX(ctx).ID
	}
	first := addVideo("ABP-123.mp4", "first", domain.LocalAccountID)
	second := addVideo("ABP-123.4k.mp4", "second", domain.LocalAccountID)
	otherSource := addVideo("other.mp4", "other", "other-account")
	subtitles := subtitle.NewService(store.Client, subtitle.NewFinder(nil, subtitle.WithProviders()))
	lib := library.New(store.Client, nil, nil, nil, library.Options{
		ExportManager: export.NewManager(export.Config{EmbyDir: root}),
		Subtitles:     librarySubtitleProvider{Service: subtitles, body: srt},
	})
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), Library: lib, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	base := "/api/library/movies/" + strconv.Itoa(film.ID) + "/subtitles"
	send := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, base+path, strings.NewReader(body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body)
		}
		return response
	}
	get := func() []library.SubtitleVideo {
		t.Helper()
		var videos []library.SubtitleVideo
		response := send(http.MethodGet, "", "", http.StatusOK)
		if err := json.Unmarshal(response.Body.Bytes(), &videos); err != nil {
			t.Fatal(err)
		}
		return videos
	}
	firstPath, secondPath := "/"+strconv.Itoa(first), "/"+strconv.Itoa(second)
	before := get()
	if len(before) != 2 || before[0].ID != second || len(before[0].Tracks) != 1 || before[0].Tracks[0].Language != "zh-TW" || len(before[1].Tracks) != 0 {
		t.Fatalf("incorrect existing subtitle ownership: %+v", before)
	}
	download := send(http.MethodPost, firstPath+"/download", "", http.StatusOK)
	if !strings.Contains(download.Body.String(), `"downloaded":1`) {
		t.Fatalf("subtitle not downloaded: %s", download.Body)
	}
	if repeated := send(http.MethodPost, firstPath+"/download", "", http.StatusOK); !strings.Contains(repeated.Body.String(), `"downloaded":0`) {
		t.Fatalf("repeated download wrote an existing track: %s", repeated.Body)
	}
	send(http.MethodPost, "/"+strconv.Itoa(otherSource)+"?name=manual.srt", srt, http.StatusNotFound)
	send(http.MethodPost, firstPath+"?name=manual.srt", "not a subtitle", http.StatusBadRequest)
	send(http.MethodPost, firstPath+"?name=manual.srt&language=ja", srt, http.StatusCreated)
	send(http.MethodPost, firstPath+"?name=manual.srt&language=ja", srt, http.StatusConflict)
	after := get()
	if len(after[1].Tracks) != 2 || after[1].Tracks[0].Name != "ABP-123.miyabi.ja.srt" || after[1].Tracks[0].Source != "manual" || after[1].Tracks[1].Source != "AVSubtitles" {
		t.Fatalf("uploaded subtitle missing: %+v", after)
	}
	track := after[1].Tracks[0]
	send(http.MethodDelete, secondPath+"/"+track.ID, "", http.StatusNotFound)
	send(http.MethodDelete, firstPath+"/"+track.ID, "", http.StatusOK)
	if _, err := os.Stat(filepath.Join(root, track.Name)); !os.IsNotExist(err) {
		t.Fatalf("deleted file remains: %v", err)
	}
	if store.Client.Subtitle.Query().CountX(ctx) != 1 || len(get()[0].Tracks) != 1 || len(get()[1].Tracks) != 1 {
		t.Fatal("delete did not clean up its record or affected the other video")
	}
}
