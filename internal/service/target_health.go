package service

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// targetHealthCheckTimeout is the per-target timeout when running a
// background health check. Kept short so the loop never stalls on a
// dead host.
const targetHealthCheckTimeout = 15 * time.Second

// TargetHealthChecker periodically runs TestConnection against every
// configured target and updates target.status + last_connected_at.
//
// The interval is read from app_settings.target_check_interval_minutes
// on each cycle. 0 (or unset) disables the loop entirely. The setting
// is re-read every cycle, so changes take effect on the next tick
// without a restart.
type TargetHealthChecker struct {
	targets *TargetService
	stop    chan struct{}
	wg      sync.WaitGroup
}

func NewTargetHealthChecker(targets *TargetService) *TargetHealthChecker {
	return &TargetHealthChecker{
		targets: targets,
		stop:    make(chan struct{}),
	}
}

// Start spawns the background loop. Safe to call once.
func (c *TargetHealthChecker) Start() {
	if c == nil || c.targets == nil {
		return
	}
	c.wg.Add(1)
	go c.run()
}

// Stop signals the loop to exit and waits for it to drain.
func (c *TargetHealthChecker) Stop() {
	if c == nil {
		return
	}
	close(c.stop)
	c.wg.Wait()
}

func (c *TargetHealthChecker) run() {
	defer c.wg.Done()

	// Use a short outer ticker so interval changes pick up within ~1 minute
	// rather than waiting for the full configured interval to elapse.
	const pollSettings = 60 * time.Second
	tick := time.NewTicker(pollSettings)
	defer tick.Stop()

	var lastRun time.Time

	for {
		select {
		case <-c.stop:
			return
		case <-tick.C:
			interval := c.readIntervalMinutes()
			if interval <= 0 {
				lastRun = time.Time{} // reset so re-enabling kicks off promptly
				continue
			}
			if !lastRun.IsZero() && time.Since(lastRun) < time.Duration(interval)*time.Minute {
				continue
			}
			c.runOnce()
			lastRun = time.Now()
		}
	}
}

// readIntervalMinutes returns the configured interval in minutes, or 0
// to mean disabled.
func (c *TargetHealthChecker) readIntervalMinutes() int {
	settings, err := c.targets.db.GetAllSettings()
	if err != nil {
		return 0
	}
	v, ok := settings["target_check_interval_minutes"]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// runOnce iterates all targets and runs TestConnection on each. Errors
// are logged and recorded on the target row but do not abort the loop.
func (c *TargetHealthChecker) runOnce() {
	targets, err := c.targets.db.ListTargets()
	if err != nil {
		slog.Warn("target health: list targets failed", "error", err)
		return
	}
	if len(targets) == 0 {
		return
	}
	slog.Info("target health: running periodic connection checks", "count", len(targets))

	for _, t := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), targetHealthCheckTimeout)
		err := c.targets.TestConnection(ctx, t.ID)
		cancel()
		if err != nil {
			slog.Info("target health: target unreachable", "target_id", t.ID, "name", t.Name, "error", err)
		}
	}
}
