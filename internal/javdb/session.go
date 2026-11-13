package javdb

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/ppxb/miyabi/internal/domain"
)

var ErrLoginRequired = errors.New("JavDB login required")

const loginRequiredMessage = "JavDB 需要登录或会话已失效，请在设置中登录 JavDB"

type SessionStatus string

const (
	SessionSignedOut  SessionStatus = "signed_out"
	SessionSignedIn   SessionStatus = "signed_in"
	SessionExpired    SessionStatus = "expired"
	SessionUnverified SessionStatus = "unverified"
)

func trustedSessionHost(host string) bool {
	return strings.HasPrefix(host, "https://") && slices.Contains(bootstrapHosts, host)
}

func (c *Client) usableRoute() *routeState {
	state := c.current.Load()
	if state != nil && c.HasSession() && !trustedSessionHost(state.host) {
		return nil
	}
	return state
}

func (c *Client) token() string {
	if token := c.sessionToken.Load(); token != nil {
		return *token
	}
	return ""
}

func (c *Client) HasSession() bool { return c.sessionToken.Load() != nil }

func (c *Client) SetSessionToken(token string) {
	if token == "" {
		c.sessionToken.Store(nil)
		return
	}
	c.sessionToken.Store(&token)
}

func (c *Client) SessionStatus(ctx context.Context) SessionStatus {
	token := c.sessionToken.Load()
	if token == nil {
		return SessionSignedOut
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var data struct {
		User *struct{} `json:"user"`
	}
	err := c.getJSON(ctx, "/api/v1/users", nil, &data)
	current := c.sessionToken.Load()
	switch {
	case current == nil:
		return SessionSignedOut
	case current != token:
		return SessionUnverified
	case errors.Is(err, ErrLoginRequired):
		return SessionExpired
	case err == nil && data.User != nil:
		return SessionSignedIn
	default:
		return SessionUnverified
	}
}

// Login submits credentials once and returns the session token without installing it.
func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 254 || password == "" || len(password) > 1024 {
		return "", domain.E(domain.KindInvalid, "请输入有效的 JavDB 账号和密码", nil)
	}
	var host string
	if current := c.current.Load(); current != nil && trustedSessionHost(current.host) {
		host = current.host
	} else {
		probeCtx, cancel := context.WithTimeout(ctx, 3*c.options.Timeout)
		defer cancel()
		status, err := selectRoute(probeCtx, routeSelection{bootstrapOnly: true}, c.probe)
		if err != nil {
			return "", err
		}
		host = status.Host
	}
	if !trustedSessionHost(host) {
		return "", domain.E(domain.KindUpstream, "没有可用于登录的 JavDB HTTPS 入口", nil)
	}
	// Use an isolated client so login cookies cannot be retained by the catalogue client.
	transport, err := newTransport(host, c.proxyURL(), c.options)
	if err != nil {
		return "", err
	}
	defer transport.closeIdleConnections()
	return transport.login(ctx, username, password)
}

func (t *transport) login(ctx context.Context, username, password string) (string, error) {
	form := url.Values{"username": {username}, "password": {password}}
	request, err := t.newRequest(ctx, http.MethodPost, "/api/v1/sessions", nil, strings.NewReader(form.Encode()), time.Now().Unix())
	if err != nil {
		return "", err
	}
	request.Header.Set("content-type", "application/x-www-form-urlencoded")
	var data struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := t.doJSON(request, &data); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// The response to a credential submission can contain sensitive values.
		return "", domain.E(domain.KindUpstream, "JavDB 登录失败，请检查账号、密码和网络后重试", nil)
	}
	token := data.Token
	if token == "" {
		token = data.AccessToken
	}
	if token == "" || len(token) > 8192 || strings.IndexFunc(token, func(r rune) bool { return r <= ' ' || r >= 127 }) >= 0 {
		return "", domain.E(domain.KindUpstream, "JavDB 登录响应缺少有效会话", nil)
	}
	return token, nil
}
