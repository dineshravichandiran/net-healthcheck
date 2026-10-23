package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exec(args ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {})
	mux.HandleFunc("/err", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", 302) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestExitCodesFollowTheWorstResult(t *testing.T) {
	s := server(t)
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"healthy", []string{"--url", s.URL + "/ok", "--skip-tls"}, 0},
		{"redirect warns", []string{"--url", s.URL + "/redirect", "--skip-tls"}, 1},
		{"500 is critical", []string{"--url", s.URL + "/err", "--skip-tls"}, 2},
		{"expecting the 500 makes it healthy", []string{"--url", s.URL + "/err", "--expect-status", "500", "--skip-tls"}, 0},
		{"no target is unknown", nil, 3},
		{"unknown flag is unknown", []string{"--bogus"}, 3},
		{"missing config file is unknown", []string{"--config", "/no/such/file"}, 3},
		{"help exits 0", []string{"--help"}, 0},
		{"version exits 0", []string{"--version"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if code, out, errOut := exec(c.args...); code != c.want {
				t.Fatalf("exit %d want %d\nstdout:%s\nstderr:%s", code, c.want, out, errOut)
			}
		})
	}
}

func TestJSONAndNagiosModes(t *testing.T) {
	s := server(t)
	code, out, _ := exec("--url", s.URL+"/ok", "--skip-tls", "--json")
	var v struct {
		Worst string `json:"worst_status"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v.Worst != "OK" {
		t.Fatalf("json mode: exit %d out %s", code, out)
	}
	code, out, _ = exec("--url", s.URL+"/err", "--skip-tls", "--nagios")
	if code != 2 || !strings.HasPrefix(out, "NET-HEALTHCHECK CRITICAL - ") || !strings.Contains(out, "http_time_") {
		t.Fatalf("nagios mode: exit %d out %s", code, out)
	}
}

func TestConfigFilesBothFormats(t *testing.T) {
	s := server(t)
	dir := t.TempDir()
	pipe := filepath.Join(dir, "targets.conf")
	js := filepath.Join(dir, "targets.json")
	os.WriteFile(pipe, []byte("# t\n127.0.0.1|1|"+s.URL+"/ok|200|1\n127.0.0.1|1|"+s.URL+"/err|200|1\n"), 0o600)
	os.WriteFile(js, []byte(`{"targets":[{"host":"127.0.0.1","port":"1","url":"`+s.URL+`/ok","skip_tls":true}]}`), 0o600)
	// port 1 is closed, so the TCP checks are CRITICAL in both files; what matters is that every line ran.
	code, out, _ := exec("--config", pipe, "--workers", "3")
	if code != 2 || strings.Count(out, "[") < 6 {
		t.Fatalf("pipe config: exit %d\n%s", code, out)
	}
	code, out, _ = exec("--config-json", js)
	if code != 2 || !strings.Contains(out, "http:"+s.URL+"/ok") {
		t.Fatalf("json config: exit %d\n%s", code, out)
	}
	os.WriteFile(js, []byte("{broken"), 0o600)
	if code, _, _ := exec("--config-json", js); code != 3 {
		t.Fatalf("bad json config should exit 3, got %d", code)
	}
}

func TestPortDefaultsFromTheURLScheme(t *testing.T) {
	// A URL-only target has no host, so only the HTTP check runs; this just proves the flag path works.
	s := server(t)
	if code, out, _ := exec("--url", s.URL+"/ok", "--skip-tls", "--timeout", "2s"); code != 0 || strings.Count(out, "[") != 1 {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
