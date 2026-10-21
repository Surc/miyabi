package netx

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// tls-client uses CONNECT even for an HTTP target. This isolated fixture
// answers inside the tunnel, so the proxy-switch test exercises the real wire
// protocol without changing production proxy behavior or weakening assertions.
func fingerprintProxy(t *testing.T, label string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy method=%s, want CONNECT", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Error(err)
			return
		}
		if _, err := io.WriteString(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Error(err)
			return
		}
		if err := buffered.Flush(); err != nil {
			t.Error(err)
			return
		}
		request, err := http.ReadRequest(buffered.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		defer request.Body.Close()
		if request.Method != http.MethodGet {
			t.Errorf("tunnel method=%s, want GET", request.Method)
		}
		if _, err := fmt.Fprintf(buffered, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(label), label); err != nil {
			t.Error(err)
			return
		}
		if err := buffered.Flush(); err != nil {
			t.Error(err)
		}
	}))
}
