# Interview notes: net-healthcheck in Go

## The 60-second pitch
"At work, the first question in most incidents is: is it DNS, the network, the app, or an expired certificate? I had
a Bash tool that answers that in one command with Nagios exit codes, so any monitoring agent can use it. I then
rewrote it in Go to learn the language on something I already understood end to end. It keeps the same flags and exit
codes, adds per-check timeouts, runs checks concurrently with a worker limit, and ships as one static binary in a
14 MB container. I proved it behaves like the original by running both against 12 real scenarios, including real
certificates of different ages, and the results were identical. It has tests with the race detector on, 94.7% and
97.7% coverage in its two packages."

## How it is built
- `internal/nhc/status.go`: the Status type, the "worst wins" ranking, `Result` and `Perf`.
- `internal/nhc/checks.go`: `CheckDNS`, `CheckTCP`, `CheckHTTP`, `CheckTLS`, each taking a `context` and returning a `Result`.
- `internal/nhc/target.go`: the pipe config (Bash-compatible) and a JSON config.
- `internal/nhc/runner.go`: `runJobs`, a bounded worker pool that keeps results in the original order.
- `internal/nhc/output.go`: text (Bash layout), JSON, and one-line Nagios with perfdata.
- `cmd/net-healthcheck/main.go`: flags, exit-code mapping; `run()` takes writers so it can be tested.

## Ten questions I should expect, answered from the real code
1. **Why Go for this?** One static binary, no dependency on dig/nc/curl/openssl being installed, real concurrency and
   cancellation with `context`. The Bash version works but depends on whatever tools the host has.
2. **How do timeouts work?** Each check wraps the caller's context in `context.WithTimeout`. The dialer, resolver, HTTP
   request and TLS handshake all receive that context, so one slow target cannot hang the run. The Bash version had a
   hand-written background-and-poll loop for the same thing.
3. **How is concurrency limited?** A buffered channel is a semaphore: acquire before starting a goroutine, release when it
   ends. Results go into a pre-sized slice by index, so there is no lock and the output order does not depend on timing.
   A test counts how many jobs run at once with an atomic counter and asserts it never exceeds the limit.
4. **Why does UNKNOWN rank below WARNING?** The Bash tool ranks CRITICAL > WARNING > UNKNOWN > OK and I copied it so the two
   behave the same. It is a deliberate choice that I would revisit before using this in production, and a test pins it.
5. **Why not follow redirects?** `curl` without `-L` doesn't, and the Bash tool treats a 3xx as a WARNING. Go follows them by
   default, so `CheckRedirect` returns `http.ErrUseLastResponse`.
6. **Why `InsecureSkipVerify`?** The HTTP check mirrors `curl -k` and the TLS check only reads the expiry date from whatever
   certificate the server presents. It would be wrong to use that setting for anything that sends data.
7. **How did you test TLS expiry without waiting?** The test generates a self-signed certificate with `crypto/x509` whose
   `NotAfter` is an exact offset, serves it with `tls.Listen`, and injects a fixed clock through `Options.Now`. That makes
   365, 30, 20, 7, 3 and -2 days deterministic, including both threshold edges.
8. **What does the race detector add?** The runner writes to a shared slice from many goroutines. `go test -race` is how I
   know that is safe (each goroutine writes only its own index).
9. **How do you know the Go version matches the Bash one?** `scripts/parity.sh` runs both against the same real targets and
   compares exit codes and every per-check status: 12 of 12 identical. It does not prove they match on every input.
10. **What would you improve?** Retries with backoff, an IPv6 and DNS-record-type option, checking the whole certificate
    chain, exporting results as Prometheus metrics, and releasing binaries from CI.

## Limits (say these out loud, they build trust)
- It checks the leaf certificate only, not the chain or revocation.
- The HTTP check is a GET with a timeout, with no retries and no body check.
- The GitHub Actions workflow for the Go code is written but has not run on GitHub yet.
- It was run on macOS arm64 and in a Linux container; the linux/amd64 and linux/arm64 binaries were built but not run.

## What I did not do
Use Go in production at work. I learned it by porting a tool I already knew, and I can explain every file.

## "Have you used Go at work?"
"Not in my day job. I wrote my first real Go program recently by porting a Bash health-check tool I'd built, and I
verified the port against the original on 12 real scenarios. I can walk you through the code, the tests and what
I would change. I learn tooling fastest by building something small and breaking it, and this is an example."
