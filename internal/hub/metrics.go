package hub

import (
	"time"
)

// DashboardSnapshot captures the complete state of the hub for the One-Page TUI.
type DashboardSnapshot struct {
	Uptime       time.Duration        `json:"uptime"`
	Connected    []ConnectedHost      `json:"connected_hosts"`
	Workers      PoolStats            `json:"workers"`
	Tools        map[string]ToolStats `json:"tools"`
	Storage      StorageStats         `json:"storage"`
	ReapedCount  uint64               `json:"reaped_count"`
	Timestamp    time.Time            `json:"timestamp"`
}

// DashboardSnapshot returns a point-in-time state of the entire hub.
func (h *Hub) DashboardSnapshot() DashboardSnapshot {
	uptime := time.Duration(0)
	if !h.startTime.IsZero() {
		uptime = time.Since(h.startTime)
	}

	var reaped uint64
	if h.reaper != nil {
		reaped = h.reaper.ReapedCount()
	}

	return DashboardSnapshot{
		Uptime:      uptime,
		Connected:   h.ActiveHosts(),
		Workers:     h.pool.Stats(),
		Tools:       h.supervisor.ListTools(),
		Storage:     h.arbiter.Stats(),
		ReapedCount: reaped,
		Timestamp:   time.Now(),
	}
}
