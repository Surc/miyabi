package emby

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/nfo"
)

// PlaybackTarget is an app launch intent, never a stream or an API-key URL.
// Android Emby 3.4.26+ handles emby://play/serverId/itemId by starting playback.
type PlaybackTarget struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
}

var playbackID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// PlaybackTarget adds an optional capability without changing EmbyManager or
// notification/export behavior. Paths come exclusively from the configured
// export directory; title searches could silently select a different movie.
func (s *Service) PlaybackTarget(ctx context.Context, code string) (PlaybackTarget, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 200 || strings.ContainsAny(code, "\x00\r\n") {
		return PlaybackTarget{}, domain.E(domain.KindInvalid, "无效的影片番号", nil)
	}
	if err := ctx.Err(); err != nil {
		return PlaybackTarget{}, err
	}
	cfg := s.currentConfig()
	if !cfg.ready() {
		return PlaybackTarget{Status: "disabled"}, nil
	}
	if cfg.LocalDir == "" {
		return PlaybackTarget{}, domain.E(domain.KindInvalid, "请先配置 Emby 导出目录", nil)
	}
	directory := export.EmbyMovieDir(cfg.LocalDir, code)
	file, err := firstPlaybackFile(directory, nfo.FileStem(code))
	if err != nil {
		return PlaybackTarget{}, domain.E(domain.KindUpstream, "读取影片播放文件失败", err)
	}
	if file == "" {
		return PlaybackTarget{Status: "not_exported"}, nil
	}
	mediaPath := translatePath(filepath.Join(directory, file), cfg.LocalDir, cfg.MediaPath)
	if mediaPath == "" {
		return PlaybackTarget{}, domain.E(domain.KindInvalid, "Emby 媒体库路径与导出目录不匹配", nil)
	}

	// Scope this client's redirect policy to the extension. An Emby API key must
	// never follow a redirect to another origin; existing clients are untouched.
	httpClient := *s.client.http
	httpClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || next.URL.Scheme != via[0].URL.Scheme || next.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}
	client := &embyClient{http: &httpClient}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	query := url.Values{"Path": {mediaPath}, "Fields": {"Path"}, "Limit": {"2"}, "Recursive": {"true"}, "IncludeItemTypes": {"Movie"}, "EnableImages": {"false"}, "EnableUserData": {"false"}}
	var result struct {
		Items *[]struct {
			ID       string `json:"Id"`
			Path     string `json:"Path"`
			IsFolder bool   `json:"IsFolder"`
		} `json:"Items"`
	}
	if err := client.request(ctx, cfg, http.MethodGet, "/Items?"+query.Encode(), nil, "", &result); err != nil {
		return PlaybackTarget{}, err
	}
	if result.Items == nil {
		return PlaybackTarget{}, domain.E(domain.KindUpstream, "Emby 返回了无法识别的影片列表", nil)
	}
	itemID := ""
	for _, item := range *result.Items {
		if item.IsFolder || item.Path != mediaPath || !playbackID.MatchString(item.ID) {
			return PlaybackTarget{}, domain.E(domain.KindUpstream, "Emby 影片路径或标识不匹配，无法确认播放目标", nil)
		}
		if itemID != "" && itemID != item.ID {
			return PlaybackTarget{}, domain.E(domain.KindConflict, "Emby 返回多个播放目标，请检查媒体库重复项", nil)
		}
		itemID = item.ID
	}
	if itemID == "" {
		return PlaybackTarget{Status: "not_found"}, nil
	}
	info, err := client.ping(ctx, cfg)
	if err != nil {
		return PlaybackTarget{}, err
	}
	if !playbackID.MatchString(info.ID) {
		return PlaybackTarget{}, domain.E(domain.KindUpstream, "Emby 服务器标识无效", nil)
	}
	return PlaybackTarget{Status: "ready", URL: "emby://play/" + info.ID + "/" + itemID}, nil
}

// Only Miyabi-generated STRM names qualify. Multipart films start with cd1;
// unrelated files, directories and symbolic links are never selected.
func firstPlaybackFile(directory, stem string) (string, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(stem) + `(?:-cd([1-9][0-9]*))?\.strm$`)
	type candidate struct {
		name string
		part int
	}
	var files []candidate
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		match := pattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		part := 0
		if match[1] != "" {
			part, err = strconv.Atoi(match[1])
			if err != nil {
				continue
			}
		}
		files = append(files, candidate{entry.Name(), part})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].part < files[j].part })
	if len(files) == 0 {
		return "", nil
	}
	return files[0].name, nil
}
