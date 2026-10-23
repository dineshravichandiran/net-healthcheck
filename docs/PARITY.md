# Bash and Go versions: do they agree?

The Go tool (`go/`) is a port of the Bash tool (`bin/net-healthcheck`). "Port" is only worth saying if it can be
shown, so `go/scripts/parity.sh` runs **both** against the same targets and compares the exit code and the status of
every individual check.

Targets are real, not mocked: a local HTTP server (200, 404, 302), real `openssl s_server` TLS servers with real
certificates of different ages (365 days, 20 days, 3 days, already expired), a closed port, a name that cannot resolve,
and one real internet site.

Result of the last run (full output in `docs/evidence/parity-run.txt`):

| Scenario | Bash exit | Go exit | Same per-check statuses |
|---|---|---|---|
| HTTP 200 | 0 | 0 | yes |
| HTTP 404 (expected 200) | 2 | 2 | yes |
| HTTP 404 (expected 404) | 0 | 0 | yes |
| HTTP 302 redirect (not followed) | 1 | 1 | yes |
| closed TCP port | 2 | 2 | yes |
| TLS cert valid 365 days | 0 | 0 | yes |
| TLS cert expiring in 20 days | 1 | 1 | yes |
| TLS cert expiring in 3 days | 2 | 2 | yes |
| TLS cert already expired | 2 | 2 | yes |
| TLS on a plain HTTP port | 2 | 2 | yes |
| name that does not resolve | 2 | 2 | yes |
| real site (example.com) | 0 | 0 | yes |

**12 of 12 identical.** Re-run it with `bash go/scripts/parity.sh`.

## What is deliberately the same
- Flags: `--host --port --url --expect-status --skip-tls --config --json --help`; the config file format
  (`host|port|url|expected_status|skip_tls`); the port default derived from the URL scheme.
- Exit codes 0 OK, 1 WARNING, 2 CRITICAL, 3 UNKNOWN, and the "worst result wins" rule.
- That rule's ranking is CRITICAL > WARNING > **UNKNOWN** > OK. UNKNOWN is *not* the worst; the Go version copies this
  (it is covered by a test) instead of "fixing" it.
- A redirect is not followed, so a 301/302 is a WARNING (curl without `-L` behaves the same).
- Certificates are not verified for the HTTP check (`curl -k`) and the TLS check only reads the expiry date.
- An IP literal is not "resolved": it is OK without a DNS lookup (the bug the Bash version once had).

## What differs (and why)
| | Bash | Go |
|---|---|---|
| Message text | `via dig`, `via nc`, `0.000533s` | `via go resolver`, `via go net`, `0.001s` (3 decimals) |
| JSON | `worst_status` and `checks[]` | the same keys, plus `duration_ms` per check (additive) |
| Needs | dig/host, nc, curl, openssl, python3 | a single static binary |
| Timeouts | hand-rolled background-and-sleep loop | `context` deadlines, one per check |
| Concurrency | sequential | worker pool (`--workers`), output order kept |
| Extras | none | `--nagios` single line with perfdata, `--config-json`, `--timeout`, `--warn-days`, `--crit-days`, `--version` |

## What is not covered
- A very slow server is not in the comparison (timeouts are tested inside the Go test-suite instead).
- The parity run proves the two agree on these scenarios; it is not proof they agree on every possible input.
