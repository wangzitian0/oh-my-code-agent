package hub

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// IdleReaper scans managed tools in the supervisor and stops tools that have
// been idle (no incoming JSON-RPC traffic) beyond the configured timeout.
type IdleReaper struct {
	mu          sync.Mutex
	supervisor  *Supervisor
	timeout     time.Duration
	interval    time.Duration
	stopCh      chan struct{}
	running     bool
	reapedCount uint64
}

// NewIdleReaper creates an IdleReaper for the supervisor.
func NewIdleReaper(sup *Supervisor, timeout time.Duration) *IdleReaper {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	interval := timeout / 2
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	if interval < 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}

	return &IdleReaper{
		supervisor: sup,
		timeout:    timeout,
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

// SetInterval overrides the ticker interval (useful in unit tests).
func (r *IdleReaper) SetInterval(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interval = d
}

// Start runs the reaper ticker in the background.
func (r *IdleReaper) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	interval := r.interval
	r.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-ticker.C:
				r.ReapOnce()
			}
		}
	}()
}

// ReapOnce performs a single pass over managed tools and terminates any tool
// exceeding the idle timeout threshold.
func (r *IdleReaper) ReapOnce() int {
	tools := r.supervisor.ListManagedTools()
	now := time.Now()
	reaped := 0

	for _, tool := range tools {
		if tool.Status() != "RUNNING" {
			continue
		}
		idle := now.Sub(tool.LastActive())
		if idle >= r.timeout {
			log.Printf("[hub:reaper] tool %q idle for %v >= %v; reaping subprocess", tool.Name(), idle.Round(time.Second), r.timeout)
			if err := tool.Stop(); err == nil {
				atomic.AddUint64(&r.reapedCount, 1)
				reaped++
			}
		}
	}

	return reaped
}

// Stop halts the background reaper ticker.
func (r *IdleReaper) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	close(r.stopCh)
}

// ReapedCount returns total number of tools reaped so far.
func (r *IdleReaper) ReapedCount() uint64 {
	return atomic.LoadUint64(&r.reapedCount)
}
