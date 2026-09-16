package hub

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerPool manages concurrency limits and queuing for subagents.
type WorkerPool struct {
	mu           sync.Mutex
	taskSem      chan struct{} // Concurrency semaphore for execution workers (max 5)
	batchSem     chan struct{} // Concurrency semaphore for swarm workers (max 50)
	activeTask   int64
	queuedTask   int64
	activeBatch  int64
	queuedBatch  int64
	totalExec    int64
	rateLimited  int64
	tbTokens     float64
	tbCapacity   float64
	tbRefillRate float64 // tokens per second
	tbLastRefill time.Time
}

// NewWorkerPool creates a worker pool with standard limits.
func NewWorkerPool() *WorkerPool {
	return &WorkerPool{
		taskSem:      make(chan struct{}, 5),  // Limit: 5 concurrent execution workers
		batchSem:     make(chan struct{}, 50), // Limit: 50 concurrent swarm workers
		tbTokens:     50.0,
		tbCapacity:   50.0,
		tbRefillRate: 50.0, // 50 requests / sec refill
		tbLastRefill: time.Now(),
	}
}

// PoolStats reports live status of the worker pool.
type PoolStats struct {
	ActiveTasks     int64 `json:"active_tasks"`
	QueuedTasks     int64 `json:"queued_tasks"`
	MaxTasks        int   `json:"max_tasks"`
	ActiveBatch     int64 `json:"active_batch"`
	QueuedBatch     int64 `json:"queued_batch"`
	MaxBatch        int   `json:"max_batch"`
	TotalExecuted   int64 `json:"total_executed"`
	Prevented429    int64 `json:"prevented_429"`
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
	}
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
