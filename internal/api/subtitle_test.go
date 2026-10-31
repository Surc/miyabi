package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/subtitle"
)

type subtitleSettingsStub struct {
	config subtitle.Config
	tested subtitle.TestRequest
	probes int
}

func (s *subtitleSettingsStub) Config(context.Context) (subtitle.Config, error) { return s.config, nil }

func (s *subtitleSettingsStub) UpdateConfig(_ context.Context, config subtitle.Config) (subtitle.Config, error) {
	if err := config.Validate(); err != nil {
		return subtitle.Config{}, err
	}
	s.config = config
	return config, nil
}

func (s *subtitleSettingsStub) TestDownload(_ context.Context, request subtitle.TestRequest) (subtitle.TestResult, error) {
	s.probes++
	s.tested = request
	return subtitle.TestResult{Code: request.Code, Status: "downloaded", Source: "AVSubtitles", Language: subtitlemeta.LangTraditionalChinese, Format: "srt", Bytes: 1024}, nil
}

func TestSubtitleSettingsEndpoints(t *testing.T) {
	service := &subtitleSettingsStub{config: subtitle.DefaultConfig()}
	router := NewRouter(Dependencies{Access: NewAccessGateService("", ""), Subtitles: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	get := send(http.MethodGet, "/api/settings/subtitles", "")
	var config subtitle.Config
	if get.Code != http.StatusOK || get.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(get.Body.Bytes(), &config) != nil || config != subtitle.DefaultConfig() {
		t.Fatalf("GET settings: %d %s", get.Code, get.Body)
	}
	put := send(http.MethodPut, "/api/settings/subtitles", `{"enabled":false,"sources":{"xunlei":false,"subtitlecat":true,"avsubtitles":false},"language":"zh-TW"}`)
	if put.Code != http.StatusOK || service.config.Enabled || service.config.Sources.Xunlei || service.config.Sources.AVSubtitles || service.config.Language != subtitlemeta.LangTraditionalChinese {
		t.Fatalf("PUT settings: %d %s config=%+v", put.Code, put.Body, service.config)
	}
	saved := service.config
	nullConfig := send(http.MethodPut, "/api/settings/subtitles", `null`)
	if nullConfig.Code != http.StatusBadRequest || service.config != saved {
		t.Fatalf("null settings reset saved configuration: %d %+v", nullConfig.Code, service.config)
	}
	invalid := send(http.MethodPut, "/api/settings/subtitles", `{"language":"en"}`)
	if invalid.Code != http.StatusBadRequest || service.config != saved {
		t.Fatalf("invalid settings: %d %s", invalid.Code, invalid.Body)
	}
	probe := send(http.MethodPost, "/api/settings/subtitles/test", `{"code":"ABP-123","config":{"enabled":false,"language":"zh-CN","sources":{"xunlei":true,"subtitlecat":false,"avsubtitles":false}}}`)
	var result subtitle.TestResult
	if probe.Code != http.StatusOK || probe.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(probe.Body.Bytes(), &result) != nil || result.Status != "downloaded" || result.Bytes != 1024 {
		t.Fatalf("download test: %d %s", probe.Code, probe.Body)
	}
	if service.probes != 1 || service.tested.Code != "ABP-123" || service.tested.Config == nil || service.tested.Config.Sources.SubtitleCat || service.tested.Config.Enabled || service.config != saved {
		t.Fatalf("test altered settings or snapshot: %+v", service)
	}
	for _, body := range []string{`{`, `{}`, `{"code":""}`, `{"code":123}`, `{"code":"` + strings.Repeat("A", 129) + `"}`} {
		response := send(http.MethodPost, "/api/settings/subtitles/test", body)
		if response.Code != http.StatusBadRequest || service.probes != 1 {
			t.Fatalf("invalid test input %q: %d %s", body, response.Code, response.Body)
		}
	}
}

func TestSubtitleSettingsRequireAuthentication(t *testing.T) {
	service := &subtitleSettingsStub{config: subtitle.DefaultConfig()}
	router := NewRouter(Dependencies{Access: NewAccessGateService("password", "secret"), Subtitles: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
		path := "/api/settings/subtitles"
		if method == http.MethodPost {
			path += "/test"
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(`{"code":"ABP-123"}`)))
		if response.Code != http.StatusUnauthorized || service.probes != 0 {
			t.Fatalf("unauthenticated %s: %d %s", method, response.Code, response.Body)
		}
	}
}
