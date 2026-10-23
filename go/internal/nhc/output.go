package nhc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// FormatText prints the same lines as the Bash tool, then the worst status.
func FormatText(rs []Result) string {
	var b strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&b, "[%-8s] %-22s %s\n", r.Status, r.Check, r.Message)
	}
	fmt.Fprintf(&b, "\nworst status: %s\n", WorstOf(rs))
	return b.String()
}

type jsonCheck struct {
	Check      string  `json:"check"`
	Status     string  `json:"status"`
	Message    string  `json:"message"`
	DurationMS float64 `json:"duration_ms"`
}

// FormatJSON matches the Bash tool's {"worst_status", "checks":[{check,status,message}]} and adds duration_ms.
func FormatJSON(rs []Result) (string, error) {
	cs := make([]jsonCheck, 0, len(rs))
	for _, r := range rs {
		cs = append(cs, jsonCheck{r.Check, r.Status.String(), r.Message, float64(r.Duration.Microseconds()) / 1000})
	}
	b, err := json.Marshal(struct {
		Worst  string      `json:"worst_status"`
		Checks []jsonCheck `json:"checks"`
	}{WorstOf(rs).String(), cs})
	return string(b) + "\n", err
}

var badLabel = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func perfString(p Perf) string {
	v := fmt.Sprintf("%g", p.Value)
	s := fmt.Sprintf("%s=%s%s", badLabel.ReplaceAllString(p.Label, "_"), v, p.Unit)
	if p.Warn != nil || p.Crit != nil {
		w, c := "", ""
		if p.Warn != nil {
			w = fmt.Sprintf("%g", *p.Warn)
		}
		if p.Crit != nil {
			c = fmt.Sprintf("%g", *p.Crit)
		}
		s += ";" + w + ";" + c
	}
	return s
}

// FormatNagios returns one plugin-style line: "NET-HEALTHCHECK STATUS - summary | perfdata".
func FormatNagios(rs []Result) string {
	var counts [4]int
	var worstMsg string
	w := WorstOf(rs)
	for _, r := range rs {
		counts[r.Status]++
		if r.Status == w && worstMsg == "" && w != OK {
			worstMsg = r.Check + " " + r.Message
		}
	}
	summary := fmt.Sprintf("%d critical, %d warning, %d unknown, %d ok", counts[Critical], counts[Warning], counts[Unknown], counts[OK])
	if worstMsg != "" {
		summary += ": " + worstMsg
	}
	var perf []string
	for _, r := range rs {
		for _, p := range r.Perf {
			perf = append(perf, perfString(p))
		}
	}
	line := fmt.Sprintf("NET-HEALTHCHECK %s - %s", w, summary)
	if len(perf) > 0 {
		line += " | " + strings.Join(perf, " ")
	}
	return line + "\n"
}
