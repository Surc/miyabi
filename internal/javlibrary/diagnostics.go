package javlibrary

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Diagnostics are per HTTP operation, including its redirects. They contain no
// query values, credentials, cookies or response text.
type requestInfo struct {
	AttemptID                       uint64
	Operation, Phase, Target, Proxy string
	Status, ProxyStatus, Redirects  int
	RedirectTarget, ContentType     string
	Bytes                           int
	Shape                           string
	ElapsedMS                       int64
}

type requestDiagnostics struct {
	mu      sync.Mutex
	started time.Time
	info    requestInfo
}

type diagnosticsKey struct{}

var nextAttempt atomic.Uint64

func diagnosticsFrom(ctx context.Context) *requestDiagnostics {
	d, _ := ctx.Value(diagnosticsKey{}).(*requestDiagnostics)
	return d
}

func (d *requestDiagnostics) update(update func(*requestInfo)) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	update(&d.info)
}

func (d *requestDiagnostics) snapshot() requestInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	info := d.info
	info.ElapsedMS = time.Since(d.started).Milliseconds()
	return info
}

// Keep the original cause available to errors.Is/As without printing arbitrary
// transport text: a malformed Location can include relative URLs or tokens that
// the API's absolute-URL redactor cannot recognize. The public response is unchanged.
type sourceError struct {
	category, reason string
	request          requestInfo
	cause            error
}

func failure(category, reason string) error {
	return &sourceError{category: category, reason: reason}
}

func (e *sourceError) Error() string {
	if e.request.Operation == "" {
		return "JavLibrary " + e.reason
	}
	i := e.request
	message := fmt.Sprintf("JavLibrary attempt=%d operation=%s phase=%s category=%s reason=%s target=%q proxy=%q http_status=%d proxy_status=%d redirects=%d redirect_target=%q content_type=%q bytes=%d elapsed_ms=%d shape=%q",
		i.AttemptID, i.Operation, i.Phase, e.category, e.reason, i.Target, i.Proxy, i.Status, i.ProxyStatus, i.Redirects, i.RedirectTarget, i.ContentType, i.Bytes, i.ElapsedMS, i.Shape)
	if e.cause != nil {
		message += " " + diagnosticCause(e.cause)
	}
	return message
}

func (e *sourceError) Unwrap() error { return e.cause }

func diagnosticCause(err error) string {
	var request *url.Error
	if errors.As(err, &request) {
		err = request.Err
	}
	result := fmt.Sprintf("cause_type=%T", err)
	var dns *net.DNSError
	if errors.As(err, &dns) {
		result += fmt.Sprintf(" dns_host=%q dns_timeout=%t dns_not_found=%t", bounded(dns.Name, 256), dns.IsTimeout, dns.IsNotFound)
	}
	var op *net.OpError
	if errors.As(err, &op) {
		result += fmt.Sprintf(" net_op=%q", bounded(op.Op, 80))
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		result += fmt.Sprintf(" errno=%d errno_text=%q", errno, errno.Error())
	}
	return result
}

func (d *requestDiagnostics) wrap(err error) error {
	if err == nil {
		return nil
	}
	category, reason := classifyFailure(err)
	return &sourceError{category: category, reason: reason, request: d.snapshot(), cause: err}
}

func classifyFailure(err error) (string, string) {
	var known *sourceError
	var dns *net.DNSError
	var op *net.OpError
	var certificate *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var tlsRecord tls.RecordHeaderError
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled", "request_canceled"
	case errors.As(err, &known):
		return known.category, known.reason
	case errors.As(err, &dns):
		return "dns", "name_resolution_failed"
	case errors.As(err, &op) && op.Op == "proxyconnect":
		return "proxy_connect", "proxy_connection_failed"
	case errors.As(err, &certificate), errors.As(err, &unknownAuthority), errors.As(err, &hostname), errors.As(err, &invalidCertificate), errors.As(err, &tlsRecord):
		return "tls", "tls_verification_or_protocol_failed"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return "timeout", "request_timed_out"
	default:
		return "network", "transport_or_body_read_failed"
	}
}

// Observe the proxy selected by Transport, not a configuration snapshot taken
// before Do. CONNECT callbacks may outlive a canceled Do, hence the mutex.
func observeTransport(transport *http.Transport) {
	resolve := transport.Proxy
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		var proxy *url.URL
		var err error
		if resolve != nil {
			proxy, err = resolve(req)
		}
		diagnosticsFrom(req.Context()).update(func(info *requestInfo) {
			info.Target = diagnosticURL(req.URL, true)
			info.Proxy = "direct"
			if proxy != nil {
				info.Proxy = diagnosticURL(proxy, false)
			}
		})
		return proxy, err
	}
	previous := transport.OnProxyConnectResponse
	transport.OnProxyConnectResponse = func(ctx context.Context, proxy *url.URL, req *http.Request, response *http.Response) error {
		diagnosticsFrom(ctx).update(func(info *requestInfo) { info.ProxyStatus = response.StatusCode })
		if previous != nil {
			if err := previous(ctx, proxy, req, response); err != nil {
				return err
			}
		}
		if response.StatusCode != http.StatusOK {
			return failure("proxy_http", "connect_rejected")
		}
		return nil
	}
}

func diagnosticURL(address *url.URL, includePath bool) string {
	if address == nil {
		return ""
	}
	clean := url.URL{Scheme: address.Scheme, Host: address.Host}
	if includePath {
		clean.Path = address.Path
	}
	return bounded(clean.String(), 256)
}

func diagnosticContentType(raw string) string {
	if raw == "" {
		return "missing"
	}
	value, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "invalid"
	}
	return bounded(value, 80)
}

func bounded(value string, max int) string {
	if len(value) > max {
		return value[:max] + "..."
	}
	return value
}

func (d *requestDiagnostics) logPage(ctx context.Context) {
	info := d.snapshot()
	slog.DebugContext(ctx, "JavLibrary response", "source", "javlibrary", "attempt", info.AttemptID,
		"operation", info.Operation, "target", info.Target, "proxy", info.Proxy,
		"http_status", info.Status, "proxy_status", info.ProxyStatus, "redirects", info.Redirects,
		"content_type", info.ContentType, "bytes", info.Bytes, "elapsed_ms", info.ElapsedMS, "shape", info.Shape)
}
