// Package health runs dependency checks for /health/ready.
package health

import (
	"context"
	"sync"
	"time"
)

// Check probes one dependency. It must honour ctx cancellation.
type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

type Result struct {
	Name    string
	Err     error
	Latency time.Duration
}

// Run executes every check in parallel, each bounded by timeout, preserving input order.
func Run(ctx context.Context, checks []Check, timeout time.Duration) []Result {
	results := make([]Result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			start := time.Now()
			err := c.Probe(cctx)
			results[i] = Result{Name: c.Name, Err: err, Latency: time.Since(start)}
		}()
	}
	wg.Wait()
	return results
}

// Healthy reports whether every result succeeded.
func Healthy(results []Result) bool {
	for _, r := range results {
		if r.Err != nil {
			return false
		}
	}
	return true
}
