// Package nhc is the Go port of net-healthcheck: DNS, TCP, HTTP(S) and TLS-expiry checks
// reported with Nagios exit codes. It keeps the Bash tool's behaviour and adds timeouts,
// concurrency, JSON config and perfdata.
package nhc

import "time"

// Status uses the Nagios exit-code numbers directly.
type Status int

const (
	OK       Status = 0
	Warning  Status = 1
	Critical Status = 2
	Unknown  Status = 3
)

func (s Status) String() string {
	switch s {
	case OK:
		return "OK"
	case Warning:
		return "WARNING"
	case Critical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// ExitCode is the process exit code for this status.
func (s Status) ExitCode() int { return int(s) }

// rank orders statuses for "worst wins". It copies the Bash tool exactly:
// CRITICAL > WARNING > UNKNOWN > OK. (UNKNOWN is NOT the worst, unlike some Nagios setups.)
func (s Status) rank() int {
	switch s {
	case OK:
		return 0
	case Unknown:
		return 1
	case Warning:
		return 2
	case Critical:
		return 3
	default:
		return 3
	}
}

// Worst returns the more severe of two statuses.
func Worst(a, b Status) Status {
	if b.rank() > a.rank() {
		return b
	}
	return a
}

// Perf is one Nagios performance-data item.
type Perf struct {
	Label string
	Value float64
	Unit  string
	Warn  *float64
	Crit  *float64
}

// Result is the outcome of one check.
type Result struct {
	Check    string
	Status   Status
	Message  string
	Duration time.Duration
	Perf     []Perf
}

// WorstOf folds a list of results into the single worst status (OK for an empty list).
func WorstOf(rs []Result) Status {
	w := OK
	for _, r := range rs {
		w = Worst(w, r.Status)
	}
	return w
}
