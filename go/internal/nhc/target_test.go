package nhc

import (
	"strings"
	"testing"
)

func TestParsePipeConfig(t *testing.T) {
	in := "# comment\n\ngithub.com|443|https://github.com|200|0\nexample.org\n127.0.0.1|8080|http://127.0.0.1:8080/health|204|1\n"
	got, err := ParsePipeConfig(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Target{
		{"github.com", "443", "https://github.com", 200, false},
		{"example.org", "443", "", 200, false}, // missing fields take the Bash defaults
		{"127.0.0.1", "8080", "http://127.0.0.1:8080/health", 204, true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d targets: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestParsePipeConfigRejectsANonNumericStatus(t *testing.T) {
	if _, err := ParsePipeConfig(strings.NewReader("a.com|443|https://a.com|OK|0\n")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseJSONConfigBothShapes(t *testing.T) {
	wrapped := `{"targets":[{"host":"a.com","url":"https://a.com"},{"host":"b.com","port":"8443","expect_status":204,"skip_tls":true}]}`
	bare := `[{"host":"a.com","url":"https://a.com"}]`
	for name, in := range map[string]string{"wrapped": wrapped, "bare": bare} {
		ts, err := ParseJSONConfig(strings.NewReader(in))
		if err != nil || len(ts) == 0 {
			t.Fatalf("%s: %v %+v", name, err, ts)
		}
		if ts[0].Port != "443" || ts[0].ExpectStatus != 200 {
			t.Errorf("%s: defaults not applied: %+v", name, ts[0])
		}
	}
	ts, _ := ParseJSONConfig(strings.NewReader(wrapped))
	if ts[1].Port != "8443" || ts[1].ExpectStatus != 204 || !ts[1].SkipTLS {
		t.Errorf("explicit values lost: %+v", ts[1])
	}
	if _, err := ParseJSONConfig(strings.NewReader("{nope")); err == nil {
		t.Fatal("expected an error for bad JSON")
	}
}

func TestDefaultPort(t *testing.T) {
	for in, want := range map[string]string{"http://x": "80", "https://x": "443", "": "443"} {
		if DefaultPort(in) != want {
			t.Errorf("DefaultPort(%q)=%s want %s", in, DefaultPort(in), want)
		}
	}
}
