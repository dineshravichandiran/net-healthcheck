package nhc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Target is one thing to check. Empty fields skip the matching check, as in the Bash tool.
type Target struct {
	Host         string `json:"host"`
	Port         string `json:"port"`
	URL          string `json:"url"`
	ExpectStatus int    `json:"expect_status"`
	SkipTLS      bool   `json:"skip_tls"`
}

// DefaultPort picks the port the Bash tool picks when --port is not given.
func DefaultPort(rawURL string) string {
	if strings.HasPrefix(rawURL, "http://") {
		return "80"
	}
	return "443"
}

// ParsePipeConfig reads the Bash tool's format: host|port|url|expected_status|skip_tls.
// Blank lines and lines starting with # are ignored; missing fields take the same defaults.
func ParsePipeConfig(r io.Reader) ([]Target, error) {
	var out []Target
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		f := strings.Split(text, "|")
		for len(f) < 5 {
			f = append(f, "")
		}
		t := Target{Host: f[0], Port: f[1], URL: f[2], ExpectStatus: 200}
		if t.Host == "" {
			continue
		}
		if t.Port == "" {
			t.Port = "443"
		}
		if f[3] != "" {
			n, err := strconv.Atoi(f[3])
			if err != nil {
				return nil, fmt.Errorf("line %d: expected_status %q is not a number", line, f[3])
			}
			t.ExpectStatus = n
		}
		t.SkipTLS = f[4] == "1"
		out = append(out, t)
	}
	return out, sc.Err()
}

// ParseJSONConfig reads {"targets":[...]} or a bare array of targets.
func ParseJSONConfig(r io.Reader) ([]Target, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Targets []Target `json:"targets"`
	}
	var ts []Target
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Targets != nil {
		ts = wrapped.Targets
	} else if err := json.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("config is neither {\"targets\": [...]} nor an array: %w", err)
	}
	for i := range ts {
		if ts[i].Port == "" {
			ts[i].Port = DefaultPort(ts[i].URL)
		}
		if ts[i].ExpectStatus == 0 {
			ts[i].ExpectStatus = 200
		}
	}
	return ts, nil
}
