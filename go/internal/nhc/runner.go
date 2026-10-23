package nhc

import (
	"context"
	"sync"
)

type job func(ctx context.Context) Result

// jobsFor lists a target's checks in the Bash tool's order: dns, tcp, http, tls.
func jobsFor(t Target, o Options) []job {
	var js []job
	if t.Host != "" {
		host := t.Host
		js = append(js, func(ctx context.Context) Result { return CheckDNS(ctx, host, o) })
	}
	if t.Host != "" && t.Port != "" {
		host, port := t.Host, t.Port
		js = append(js, func(ctx context.Context) Result { return CheckTCP(ctx, host, port, o) })
	}
	if t.URL != "" {
		u, exp := t.URL, t.ExpectStatus
		js = append(js, func(ctx context.Context) Result { return CheckHTTP(ctx, u, exp, o) })
	}
	if !t.SkipTLS && t.Host != "" && t.Port != "" {
		host, port := t.Host, t.Port
		js = append(js, func(ctx context.Context) Result { return CheckTLS(ctx, host, port, o) })
	}
	return js
}

// Run executes every check of every target, at most `workers` at a time, and returns the results
// in the same order the sequential Bash tool would have printed them.
func Run(ctx context.Context, targets []Target, o Options, workers int) []Result {
	if workers < 1 {
		workers = 1
	}
	var jobs []job
	for _, t := range targets {
		jobs = append(jobs, jobsFor(t, o)...)
	}
	return runJobs(ctx, jobs, workers)
}

// runJobs runs the jobs with a bounded worker count and keeps the results in job order.
func runJobs(ctx context.Context, jobs []job, workers int) []Result {
	if workers < 1 {
		workers = 1
	}
	results := make([]Result, len(jobs))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, j job) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = j(ctx)
		}(i, j)
	}
	wg.Wait()
	return results
}
