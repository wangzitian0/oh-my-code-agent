package hub

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ActiveWorker represents a live subagent task or swarm batch currently executing.
type ActiveWorker struct {
	ID            string             `json:"id"`
	Type          string             `json:"type"`           // "task" (GLM-5.3 execution) or "batch" (GLM-5.3-Flash swarm)
	ToolName      string             `json:"tool_name"`      // "subagent_task", "subagent_batch", etc.
	HostName      string             `json:"host_name"`      // Host client, e.g. "antigravity", "codex", "claude"
	Model         string             `json:"model"`          // Model name
	PromptPreview string             `json:"prompt_preview"` // Preview of prompt/task
	StartTime     time.Time          `json:"start_time"`
	LastHeartbeat time.Time          `json:"last_heartbeat"`
	Phase         string             `json:"phase"`          // "running", "thinking", "calling_tool", "compiling"
	DeclaredLease time.Duration      `json:"declared_lease,omitempty"`
	cancel        context.CancelFunc `json:"-"`
}

// WorkerPool manages concurrency limits and queuing for subagents.
type WorkerPool struct {
	mu            sync.Mutex
	taskSem       chan struct{} // Concurrency semaphore for execution workers (max 5)
	batchSem      chan struct{} // Concurrency semaphore for swarm workers (max 50)
	activeTask    int64
	queuedTask    int64
	activeBatch   int64
	queuedBatch   int64
	totalExec     int64
	rateLimited   int64
	tbTokens      float64
	tbCapacity    float64
	tbRefillRate  float64 // tokens per second
	tbLastRefill  time.Time
	workersMu     sync.RWMutex
	activeWorkers map[string]*ActiveWorker
}

// NewWorkerPool creates a worker pool with standard limits.
func NewWorkerPool() *WorkerPool {
	return &WorkerPool{
		taskSem:       make(chan struct{}, 5),  // Limit: 5 concurrent execution workers
		batchSem:      make(chan struct{}, 50), // Limit: 50 concurrent swarm workers
		tbTokens:      50.0,
		tbCapacity:    50.0,
		tbRefillRate:  50.0, // 50 requests / sec refill
		tbLastRefill:  time.Now(),
		activeWorkers: make(map[string]*ActiveWorker),
	}
}

// PoolStats reports live status of the worker pool.
type PoolStats struct {
	ActiveTasks   int64          `json:"active_tasks"`
	QueuedTasks   int64          `json:"queued_tasks"`
	MaxTasks      int            `json:"max_tasks"`
	ActiveBatch   int64          `json:"active_batch"`
	QueuedBatch   int64          `json:"queued_batch"`
	MaxBatch      int            `json:"max_batch"`
	TotalExecuted int64          `json:"total_executed"`
	Prevented429  int64          `json:"prevented_429"`
	Workers       []ActiveWorker `json:"workers,omitempty"`
}

// Stats returns current pool metrics.
func (p *WorkerPool) Stats() PoolStats {
	return PoolStats{
		ActiveTasks:   atomic.LoadInt64(&p.activeTask),
		QueuedTasks:   atomic.LoadInt64(&p.queuedTask),
		MaxTasks:      5,
		ActiveBatch:   atomic.LoadInt64(&p.activeBatch),
		QueuedBatch:   atomic.LoadInt64(&p.queuedBatch),
		MaxBatch:      50,
		TotalExecuted: atomic.LoadInt64(&p.totalExec),
		Prevented429:  atomic.LoadInt64(&p.rateLimited),
		Workers:       p.ListActiveWorkers(),
	}
}

// RegisterWorker registers an active worker with its cancellation handle.
func (p *WorkerPool) RegisterWorker(w *ActiveWorker, cancel context.CancelFunc) {
	if w == nil || w.ID == "" {
		return
	}
	w.cancel = cancel
	if w.StartTime.IsZero() {
		w.StartTime = time.Now()
	}
	if w.LastHeartbeat.IsZero() {
		w.LastHeartbeat = time.Now()
	}
	if w.Phase == "" {
		w.Phase = "running"
	}

	p.workersMu.Lock()
	defer p.workersMu.Unlock()
	p.activeWorkers[w.ID] = w
}

// TouchWorker updates the heartbeat timestamp and optionally the phase and lease.
func (p *WorkerPool) TouchWorker(id string, phase string, lease time.Duration) {
	p.workersMu.Lock()
	defer p.workersMu.Unlock()
	if w, ok := p.activeWorkers[id]; ok {
		w.LastHeartbeat = time.Now()
		if phase != "" {
			w.Phase = phase
		}
		if lease > 0 {
			w.DeclaredLease = lease
		}
	}
}

// UnregisterWorker removes a worker from the active registry upon completion.
func (p *WorkerPool) UnregisterWorker(id string) {
	p.workersMu.Lock()
	defer p.workersMu.Unlock()
	delete(p.activeWorkers, id)
}

// KillWorker terminates an active worker by invoking its cancellation function.
func (p *WorkerPool) KillWorker(id string) error {
	p.workersMu.RLock()
	w, ok := p.activeWorkers[id]
	p.workersMu.RUnlock()
	if !ok {
		return fmt.Errorf("worker %q not found or already finished", id)
	}
	if w.cancel != nil {
		w.cancel()
	}
	p.UnregisterWorker(id)
	return nil
}

// ListActiveWorkers returns a point-in-time snapshot of all active workers.
func (p *WorkerPool) ListActiveWorkers() []ActiveWorker {
	p.workersMu.RLock()
	defer p.workersMu.RUnlock()
	res := make([]ActiveWorker, 0, len(p.activeWorkers))
	for _, w := range p.activeWorkers {
		res = append(res, *w)
	}
	return res
}

// GetWorker retrieves an active worker by ID.
func (p *WorkerPool) GetWorker(id string) (ActiveWorker, bool) {
	p.workersMu.RLock()
	defer p.workersMu.RUnlock()
	w, ok := p.activeWorkers[id]
	if !ok {
		return ActiveWorker{}, false
	}
	return *w, true
}

// AcquireTaskSlot acquires an execution worker slot (GLM-5.3, max 5).
func (p *WorkerPool) AcquireTaskSlot(ctx context.Context) (func(), error) {
	p.throttleRateLimit()

	atomic.AddInt64(&p.queuedTask, 1)
	defer atomic.AddInt64(&p.queuedTask, -1)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.taskSem <- struct{}{}:
		atomic.AddInt64(&p.activeTask, 1)
		atomic.AddInt64(&p.totalExec, 1)
		return func() {
			<-p.taskSem
			atomic.AddInt64(&p.activeTask, -1)
		}, nil
	}
}

// AcquireBatchSlot acquires a swarm worker slot (GLM-5.3-Flash, max 50).
func (p *WorkerPool) AcquireBatchSlot(ctx context.Context) (func(), error) {
	p.throttleRateLimit()

	atomic.AddInt64(&p.queuedBatch, 1)
	defer atomic.AddInt64(&p.queuedBatch, -1)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.batchSem <- struct{}{}:
		atomic.AddInt64(&p.activeBatch, 1)
		atomic.AddInt64(&p.totalExec, 1)
		return func() {
			<-p.batchSem
			atomic.AddInt64(&p.activeBatch, -1)
		}, nil
	}
}

func (p *WorkerPool) throttleRateLimit() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(p.tbLastRefill).Seconds()
	p.tbLastRefill = now

	p.tbTokens += elapsed * p.tbRefillRate
	if p.tbTokens > p.tbCapacity {
		p.tbTokens = p.tbCapacity
	}

	if p.tbTokens < 1.0 {
		atomic.AddInt64(&p.rateLimited, 1)
		waitTime := time.Duration((1.0-p.tbTokens)/p.tbRefillRate*float64(time.Second))
		if waitTime > 0 && waitTime < 2*time.Second {
			time.Sleep(waitTime)
			p.tbTokens = 0.0
			return
		}
	}
	p.tbTokens -= 1.0
}
