package nhc

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fastOpts() Options {
	o := DefaultOptions()
	o.Timeout = 2 * time.Second
	return o
}

func TestCheckTLSExpiry(t *testing.T) {
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		days     int
		want     Status
		contains string
	}{
		{"plenty of time", 90, OK, "valid for 90d"},
		{"at the warning edge is still ok", 30, OK, "valid for 30d"},
		{"inside the warning window", 20, Warning, "expires in 20d"},
		{"at the critical edge is only a warning", 7, Warning, "expires in 7d"},
		{"inside the critical window", 3, Critical, "expires in 3d"},
		{"expired two days ago", -2, Critical, "EXPIRED 2 day(s) ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, port := startTLSServer(t, base.Add(time.Duration(c.days)*24*time.Hour))
			o := fastOpts()
			o.Now = func() time.Time { return base }
			r := CheckTLS(context.Background(), host, port, o)
			if r.Status != c.want || !strings.Contains(r.Message, c.contains) {
				t.Fatalf("got %s %q, want %s containing %q", r.Status, r.Message, c.want, c.contains)
			}
			if len(r.Perf) != 1 || r.Perf[0].Warn == nil || *r.Perf[0].Warn != 30 || *r.Perf[0].Crit != 7 {
				t.Fatalf("perfdata thresholds wrong: %+v", r.Perf)
			}
		})
	}
}

func TestCheckTLSOnAPlainTCPPortIsCritical(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("not tls\n"))
			c.Close()
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	if r := CheckTLS(context.Background(), h, p, fastOpts()); r.Status != Critical {
		t.Fatalf("got %s %q", r.Status, r.Message)
	}
}

func TestCheckTLSOnAClosedPortIsCritical(t *testing.T) {
	if r := CheckTLS(context.Background(), "127.0.0.1", closedPort(t), fastOpts()); r.Status != Critical {
		t.Fatalf("got %s %q", r.Status, r.Message)
	}
}

func newHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/err", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusFound) })
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) { time.Sleep(400 * time.Millisecond) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestCheckHTTP(t *testing.T) {
	s := newHTTPServer(t)
	cases := []struct {
		name     string
		path     string
		expect   int
		timeout  time.Duration
		want     Status
		contains string
	}{
		{"200 as expected", "/ok", 200, 0, OK, "HTTP 200 in"},
		{"500 is critical", "/err", 200, 0, Critical, "HTTP 500 (expected 200)"},
		{"404 is fine when 404 is expected", "/missing", 404, 0, OK, "HTTP 404 in"},
		{"redirect is not followed, so it is a warning", "/redirect", 200, 0, Warning, "HTTP 302 (expected 200)"},
		{"a slow server hits the timeout", "/slow", 200, 100 * time.Millisecond, Critical, "timed out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := fastOpts()
			if c.timeout > 0 {
				o.Timeout = c.timeout
			}
			r := CheckHTTP(context.Background(), s.URL+c.path, c.expect, o)
			if r.Status != c.want || !strings.Contains(r.Message, c.contains) {
				t.Fatalf("got %s %q, want %s containing %q", r.Status, r.Message, c.want, c.contains)
			}
		})
	}
}

func TestCheckHTTPServerGoneAndBadURL(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL
	s.Close()
	if r := CheckHTTP(context.Background(), url, 200, fastOpts()); r.Status != Critical {
		t.Fatalf("closed server: got %s %q", r.Status, r.Message)
	}
	if r := CheckHTTP(context.Background(), "http://bad host/\x7f", 200, fastOpts()); r.Status != Critical {
		t.Fatalf("bad url: got %s %q", r.Status, r.Message)
	}
}

func TestCheckHTTPDefaultsExpectedStatusTo200(t *testing.T) {
	s := newHTTPServer(t)
	if r := CheckHTTP(context.Background(), s.URL+"/ok", 0, fastOpts()); r.Status != OK {
		t.Fatalf("got %s %q", r.Status, r.Message)
	}
}

func TestCheckTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	r := CheckTCP(context.Background(), h, p, fastOpts())
	if r.Status != OK || !strings.Contains(r.Message, "connected in") || len(r.Perf) != 1 || r.Perf[0].Unit != "ms" {
		t.Fatalf("open port: got %s %q %+v", r.Status, r.Message, r.Perf)
	}
	if r := CheckTCP(context.Background(), "127.0.0.1", closedPort(t), fastOpts()); r.Status != Critical {
		t.Fatalf("closed port: got %s %q", r.Status, r.Message)
	}
}

func TestCheckDNS(t *testing.T) {
	cases := []struct {
		host string
		want Status
		msg  string
	}{
		{"127.0.0.1", OK, "already an IP literal"},
		{"::1", OK, "already an IP literal"},
		{"localhost", OK, "resolved to"},
		{"no-such-name.invalid", Critical, "did not resolve"},
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			r := CheckDNS(context.Background(), c.host, fastOpts())
			if r.Status != c.want || !strings.Contains(r.Message, c.msg) {
				t.Fatalf("got %s %q, want %s containing %q", r.Status, r.Message, c.want, c.msg)
			}
		})
	}
}

func TestOptionsDefaults(t *testing.T) {
	if (Options{}).timeout() != 5*time.Second {
		t.Fatal("zero timeout should default to 5s")
	}
	if (Options{}).now().IsZero() {
		t.Fatal("zero Options should fall back to the real clock")
	}
}
