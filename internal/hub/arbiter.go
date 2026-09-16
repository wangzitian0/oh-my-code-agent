package hub

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// StorageArbiter coordinates exclusive access to local databases (SQLite WAL).
type StorageArbiter struct {
	writeLock    sync.Mutex
	lockWaitNano int64
	dbPath       string
}

// NewStorageArbiter creates a storage arbiter.
func NewStorageArbiter(dbPath string) *StorageArbiter {
	return &StorageArbiter{
		dbPath: dbPath,
	}
}

// StorageStats reports storage metrics.
type StorageStats struct {
	DBPath       string        `json:"db_path"`
	SizeBytes    int64         `json:"size_bytes"`
	LockWaitTime time.Duration `json:"lock_wait_time"`
	Status       string        `json:"status"`
}

// Stats returns the storage status.
func (a *StorageArbiter) Stats() StorageStats {
	size := int64(0)
	status := "READY"
	if a.dbPath != "" {
		if fi, err := os.Stat(a.dbPath); err == nil {
			size = fi.Size()
		} else {
			status = "NOT_FOUND"
		}
	}
	return StorageStats{
		DBPath:       a.dbPath,
		SizeBytes:    size,
		LockWaitTime: time.Duration(atomic.LoadInt64(&a.lockWaitNano)),
		Status:       status,
	}
}

// WithWriteLock executes a write operation under single-writer lock protection.
func (a *StorageArbiter) WithWriteLock(ctx context.Context, fn func() error) error {
	start := time.Now()
	a.writeLock.Lock()
	defer a.writeLock.Unlock()

	waitDuration := time.Since(start)
	atomic.AddInt64(&a.lockWaitNano, waitDuration.Nanoseconds())

	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}
