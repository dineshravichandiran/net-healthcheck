package nhc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sample() []Result {
	w, c := 30.0, 7.0
	return []Result{
		{Check: "dns:a.com", Status: OK, Message: "resolved to 1.2.3.4 (via go resolver)"},
		{Check: "tcp:a.com:443", Status: OK, Message: "connected in 54ms (via go net)", Perf: []Perf{{Label: "tcp_time_a.com_443", Value: 54, Unit: "ms"}}},
		{Check: "tls:a.com:443", Status: Warning, Message: "certificate expires in 20d (Nov  1 00:00:00 2026 GMT)", Duration: 12 * time.Millisecond,
			Perf: []Perf{{Label: "tls days/left", Value: 20, Unit: "d", Warn: &w, Crit: &c}}},
	}
}

func TestFormatTextMatchesTheBashLayout(t *testing.T) {
	out := FormatText(sample())
	if !strings.Contains(out, "[OK      ] dns:a.com") || !strings.Contains(out, "[WARNING ] tls:a.com:443") {
		t.Fatalf("unexpected layout:\n%s", out)
	}
	if !strings.HasSuffix(out, "\nworst status: WARNING\n") {
		t.Fatalf("missing summary:\n%s", out)
	}
}

func TestFormatJSONKeepsTheBashShape(t *testing.T) {
	out, err := FormatJSON(sample())
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Worst  string `json:"worst_status"`
		Checks []struct {
			Check, Status, Message string
			DurationMS             float64 `json:"duration_ms"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if v.Worst != "WARNING" || len(v.Checks) != 3 || v.Checks[2].Status != "WARNING" || v.Checks[2].DurationMS != 12 {
		t.Fatalf("unexpected content: %+v", v)
	}
	empty, _ := FormatJSON(nil)
	if !strings.Contains(empty, `"checks":[]`) {
		t.Fatalf("empty list must render as [], got %s", empty)
	}
}

func TestFormatNagiosLineAndPerfdata(t *testing.T) {
	out := FormatNagios(sample())
	for _, want := range []string{
		"NET-HEALTHCHECK WARNING - 0 critical, 1 warning, 0 unknown, 2 ok: tls:a.com:443 certificate expires in 20d",
		" | tcp_time_a.com_443=54ms tls_days_left=20d;30;7",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("must be a single line:\n%s", out)
	}
	if ok := FormatNagios([]Result{{Check: "x", Status: OK}}); !strings.HasPrefix(ok, "NET-HEALTHCHECK OK - 0 critical, 0 warning, 0 unknown, 1 ok\n") {
		t.Fatalf("all-OK line wrong: %q", ok)
	}
}
