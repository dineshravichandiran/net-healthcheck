package nhc

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Options are the knobs shared by every check.
type Options struct {
	Timeout  time.Duration    // per-check timeout, applied through the context
	WarnDays int              // TLS: warn when fewer days than this are left
	CritDays int              // TLS: critical when fewer days than this are left
	Now      func() time.Time // injectable clock for tests
}

// DefaultOptions mirrors the Bash tool: 30-day warning, 7-day critical, 5s timeout.
func DefaultOptions() Options {
	return Options{Timeout: 5 * time.Second, WarnDays: 30, CritDays: 7, Now: time.Now}
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 5 * time.Second
	}
	return o.Timeout
}

func isIPLiteral(s string) bool { return net.ParseIP(s) != nil }

// CheckDNS resolves host. An IP literal is OK without a lookup (the Bash tool had a bug here once:
// querying an IP as a hostname returned nothing and gave a false CRITICAL).
func CheckDNS(ctx context.Context, host string, o Options) Result {
	name := "dns:" + host
	start := time.Now()
	if isIPLiteral(host) {
		return Result{Check: name, Status: OK, Message: "already an IP literal, nothing to resolve", Duration: time.Since(start)}
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return Result{Check: name, Status: Critical, Message: "did not resolve (via go resolver)", Duration: time.Since(start)}
	}
	return Result{Check: name, Status: OK, Message: fmt.Sprintf("resolved to %s (via go resolver)", addrs[0].IP), Duration: time.Since(start)}
}

// CheckTCP opens a TCP connection to host:port.
func CheckTCP(ctx context.Context, host, port string, o Options) Result {
	name := fmt.Sprintf("tcp:%s:%s", host, port)
	ctx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	elapsed := time.Since(start)
	if err != nil {
		return Result{Check: name, Status: Critical, Message: "connection refused or timed out (via go net)", Duration: elapsed}
	}
	_ = conn.Close()
	ms := float64(elapsed.Milliseconds())
	return Result{
		Check: name, Status: OK, Duration: elapsed,
		Message: fmt.Sprintf("connected in %dms (via go net)", elapsed.Milliseconds()),
		Perf:    []Perf{{Label: "tcp_time_" + host + "_" + port, Value: ms, Unit: "ms"}},
	}
}

// CheckHTTP does a GET and compares the status code. Like `curl -k` without -L it does not verify
// certificates and does not follow redirects, so a 301/302 shows up as a WARNING.
func CheckHTTP(ctx context.Context, rawURL string, expected int, o Options) Result {
	name := "http:" + rawURL
	if expected == 0 {
		expected = 200
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{Check: name, Status: Critical, Message: "request failed (bad URL)", Duration: time.Since(start)}
	}
	client := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // matches curl -k
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		var ue *url.Error
		msg := "request failed"
		if e, ok := err.(*url.Error); ok {
			ue = e
			if ue.Timeout() {
				msg = "request failed (timed out)"
			}
		}
		return Result{Check: name, Status: Critical, Message: msg, Duration: elapsed}
	}
	defer resp.Body.Close()
	secs := elapsed.Seconds()
	perf := []Perf{{Label: "http_time_" + rawURL, Value: math.Round(secs*1000) / 1000, Unit: "s"}}
	code := resp.StatusCode
	switch {
	case code == expected:
		return Result{Check: name, Status: OK, Message: fmt.Sprintf("HTTP %d in %.3fs", code, secs), Duration: elapsed, Perf: perf}
	case code/100 == 2 || code/100 == 3:
		return Result{Check: name, Status: Warning, Message: fmt.Sprintf("HTTP %d (expected %d) in %.3fs", code, expected, secs), Duration: elapsed, Perf: perf}
	default:
		return Result{Check: name, Status: Critical, Message: fmt.Sprintf("HTTP %d (expected %d) in %.3fs", code, expected, secs), Duration: elapsed, Perf: perf}
	}
}

// CheckTLS reads the certificate presented by host:port and reports how long it has left.
func CheckTLS(ctx context.Context, host, port string, o Options) Result {
	name := fmt.Sprintf("tls:%s:%s", host, port)
	ctx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	start := time.Now()
	d := tls.Dialer{Config: &tls.Config{ServerName: host, InsecureSkipVerify: true}} //nolint:gosec // we only read the expiry date
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return Result{Check: name, Status: Critical, Message: "could not retrieve certificate (handshake failed or timed out)", Duration: time.Since(start)}
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return Result{Check: name, Status: Critical, Message: "could not retrieve certificate (handshake failed or timed out)", Duration: time.Since(start)}
	}
	notAfter := certs[0].NotAfter.UTC()
	when := notAfter.Format("Jan _2 15:04:05 2006 GMT") // the same shape openssl prints
	daysLeft := int(math.Round(notAfter.Sub(o.now()).Hours() / 24))
	warn, crit := float64(o.WarnDays), float64(o.CritDays)
	perf := []Perf{{Label: "tls_days_left_" + host + "_" + port, Value: float64(daysLeft), Unit: "d", Warn: &warn, Crit: &crit}}
	el := time.Since(start)
	switch {
	case daysLeft < 0:
		return Result{Check: name, Status: Critical, Message: fmt.Sprintf("certificate EXPIRED %d day(s) ago (%s)", -daysLeft, when), Duration: el, Perf: perf}
	case daysLeft < o.CritDays:
		return Result{Check: name, Status: Critical, Message: fmt.Sprintf("certificate expires in %dd (%s)", daysLeft, when), Duration: el, Perf: perf}
	case daysLeft < o.WarnDays:
		return Result{Check: name, Status: Warning, Message: fmt.Sprintf("certificate expires in %dd (%s)", daysLeft, when), Duration: el, Perf: perf}
	default:
		return Result{Check: name, Status: OK, Message: fmt.Sprintf("certificate valid for %dd (%s)", daysLeft, when), Duration: el, Perf: perf}
	}
}
