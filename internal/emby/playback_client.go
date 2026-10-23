package emby

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
)

const playbackResponseLimit = 1 << 20 // Items (at most two) and System/Info only.

// A bounded read-only client for playback. The legacy settings, notifications
// and actor client retain their existing request and response contracts.
type playbackClient struct {
	http   *http.Client
	base   *url.URL
	apiKey string
}

func newPlaybackClient(cfg Config, shared *http.Client) (*playbackClient, error) {
	raw := strings.TrimSpace(cfg.ServerURL)
	base, err := url.Parse(raw)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil || strings.ContainsAny(raw, "?#") {
		return nil, domain.E(domain.KindInvalid, "Emby 服务器地址须为 HTTP(S) 基址，可含 /emby 前缀，但不能含账号、查询参数或片段", nil)
	}
	client := *shared
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || next.URL.Scheme != base.Scheme || next.URL.Host != base.Host || next.URL.User != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &playbackClient{http: &client, base: base, apiKey: cfg.APIKey}, nil
}

func (c *playbackClient) get(ctx context.Context, endpoint string, query url.Values, result any) error {
	target := c.base.JoinPath(endpoint)
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return domain.E(domain.KindInvalid, "创建 Emby 播放查询失败", err)
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return domain.E(domain.KindUpstream, "无法连接到 Emby 服务器，请检查地址和网络", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return domain.E(domain.KindUnauthorized, "Emby API Key 无效或权限不足", nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// An upstream error page may echo credentials or private URLs. Never copy
		// its body into the Miyabi API response or diagnostic error.
		return domain.E(domain.KindUpstream, fmt.Sprintf("Emby 服务器返回错误 (HTTP %d)", resp.StatusCode), nil)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, playbackResponseLimit+1))
	if err != nil {
		return domain.E(domain.KindUpstream, "读取 Emby 播放响应失败", err)
	}
	if len(body) > playbackResponseLimit {
		return domain.E(domain.KindUpstream, "Emby 播放响应超过大小限制", nil)
	}
	// Unmarshal rejects a valid JSON object followed by garbage or another value.
	if err := json.Unmarshal(body, result); err != nil {
		return domain.E(domain.KindUpstream, "解析 Emby 播放响应失败", err)
	}
	return nil
}
