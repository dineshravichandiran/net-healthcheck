package nhc

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunJobsHonoursTheWorkerLimitAndKeepsOrder(t *testing.T) {
	const jobsN, workers = 40, 5
	var running, peak atomic.Int32
	jobs := make([]job, jobsN)
	for i := range jobs {
		i := i
		jobs[i] = func(context.Context) Result {
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			running.Add(-1)
			return Result{Check: fmt.Sprintf("job-%02d", i)}
		}
	}
	res := runJobs(context.Background(), jobs, workers)
	if peak.Load() > workers {
		t.Fatalf("saw %d jobs at once, limit was %d", peak.Load(), workers)
	}
	if peak.Load() < 2 {
		t.Fatalf("expected real concurrency, peak was %d", peak.Load())
	}
	for i, r := range res {
		if r.Check != fmt.Sprintf("job-%02d", i) {
			t.Fatalf("result %d out of order: %s", i, r.Check)
		}
	}
}

func TestRunJobsWithZeroWorkersStillRuns(t *testing.T) {
	res := runJobs(context.Background(), []job{func(context.Context) Result { return Result{Check: "x"} }}, 0)
	if len(res) != 1 || res[0].Check != "x" {
		t.Fatalf("got %+v", res)
	}
}

func TestRunBuildsChecksInBashOrderPerTarget(t *testing.T) {
	s := newHTTPServer(t)
	h, p := startTLSServer(t, time.Now().Add(90*24*time.Hour))
	targets := []Target{
		{Host: "127.0.0.1", Port: p, URL: s.URL + "/ok", ExpectStatus: 200},
		{Host: h, Port: p, SkipTLS: true},
		{URL: s.URL + "/err", ExpectStatus: 200},
	}
	res := Run(context.Background(), targets, fastOpts(), 4)
	var names []string
	for _, r := range res {
		names = append(names, r.Check[:3])
	}
	want := []string{"dns", "tcp", "htt", "tls", "dns", "tcp", "htt"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("order %v want %v", names, want)
	}
	if WorstOf(res) != Critical {
		t.Fatalf("the 500 should make the batch CRITICAL, got %s", WorstOf(res))
	}
}
