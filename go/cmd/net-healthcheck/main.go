// net-healthcheck (Go): DNS, TCP, HTTP(S) and TLS-expiry diagnostics with Nagios exit codes.
// It is a port of the Bash tool in ../../bin and keeps its flags and exit codes.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/dineshravichandiran/net-healthcheck/go/internal/nhc"
)

var version = "dev" // set with -ldflags "-X main.version=..."

const usage = `net-healthcheck (go) - OSI-layer network diagnostics (DNS, TCP, HTTP, TLS expiry)

Usage:
  net-healthcheck --host HOST [--port PORT] [--url URL] [options]
  net-healthcheck --config FILE [options]          (host|port|url|expected_status|skip_tls per line)
  net-healthcheck --config-json FILE [options]     ({"targets":[{"host":...,"port":...,"url":...}]})

Options:
  --host HOST          Hostname to check DNS resolution, TCP, and TLS expiry against
  --port PORT          TCP port to check (default: 443 if --url is https, else 80)
  --url URL            URL to check via HTTP(S) request (redirects are not followed)
  --expect-status CODE Expected HTTP status code (default: 200)
  --skip-tls           Skip the TLS certificate expiry check
  --config FILE        Run checks for every target line in FILE
  --config-json FILE   Run checks for every target in a JSON file
  --json               Emit a single JSON summary instead of human-readable lines
  --nagios             Emit one Nagios plugin line with performance data
  --timeout DURATION   Per-check timeout (default 5s)
  --workers N          Run at most N checks at once (default 8)
  --warn-days N        TLS warning threshold in days (default 30)
  --crit-days N        TLS critical threshold in days (default 7)
  --version            Print the version and exit
  --help               Show this help and exit

Exit codes: 0 OK, 1 WARNING, 2 CRITICAL, 3 UNKNOWN
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("net-healthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		host, port, rawURL, config, configJSON string
		expect                                 = fs.Int("expect-status", 200, "")
		skipTLS, asJSON, asNagios, showVer     bool
		help                                   bool
		timeout                                = fs.Duration("timeout", 5*time.Second, "")
		workers                                = fs.Int("workers", 8, "")
		warnDays                               = fs.Int("warn-days", 30, "")
		critDays                               = fs.Int("crit-days", 7, "")
	)
	fs.StringVar(&host, "host", "", "")
	fs.StringVar(&port, "port", "", "")
	fs.StringVar(&rawURL, "url", "", "")
	fs.StringVar(&config, "config", "", "")
	fs.StringVar(&configJSON, "config-json", "", "")
	fs.BoolVar(&skipTLS, "skip-tls", false, "")
	fs.BoolVar(&asJSON, "json", false, "")
	fs.BoolVar(&asNagios, "nagios", false, "")
	fs.BoolVar(&showVer, "version", false, "")
	fs.BoolVar(&help, "help", false, "")
	fs.BoolVar(&help, "h", false, "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "Unknown argument: %v\n%s", err, usage)
		return nhc.Unknown.ExitCode()
	}
	if help {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if showVer {
		fmt.Fprintf(stdout, "net-healthcheck-go %s\n", version)
		return 0
	}

	var targets []nhc.Target
	switch {
	case config != "" || configJSON != "":
		path, parse := config, nhc.ParsePipeConfig
		if configJSON != "" {
			path, parse = configJSON, nhc.ParseJSONConfig
		}
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(stderr, "Config file not found: %s\n", path)
			return nhc.Unknown.ExitCode()
		}
		defer f.Close()
		targets, err = parse(f)
		if err != nil {
			fmt.Fprintf(stderr, "Config error: %v\n", err)
			return nhc.Unknown.ExitCode()
		}
	default:
		if host == "" && rawURL == "" {
			fmt.Fprintln(stderr, "Need at least --host or --url (or --config). See --help.")
			return nhc.Unknown.ExitCode()
		}
		if port == "" {
			port = nhc.DefaultPort(rawURL)
		}
		targets = []nhc.Target{{Host: host, Port: port, URL: rawURL, ExpectStatus: *expect, SkipTLS: skipTLS}}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opts := nhc.Options{Timeout: *timeout, WarnDays: *warnDays, CritDays: *critDays, Now: time.Now}
	results := nhc.Run(ctx, targets, opts, *workers)

	switch {
	case asJSON:
		out, err := nhc.FormatJSON(results)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return nhc.Unknown.ExitCode()
		}
		fmt.Fprint(stdout, out)
	case asNagios:
		fmt.Fprint(stdout, nhc.FormatNagios(results))
	default:
		fmt.Fprint(stdout, nhc.FormatText(results))
	}
	return nhc.WorstOf(results).ExitCode()
}
