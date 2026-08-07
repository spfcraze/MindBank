package cron

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/dream"
)

// DreamCron runs neural consolidation cycles on a schedule.
type DreamCron struct {
	engine   *dream.Engine
	interval time.Duration
	stopCh   chan struct{}
}

// NewDreamCron creates a new dream cron job.
func NewDreamCron(pool *pgxpool.Pool) *DreamCron {
	return &DreamCron{
		engine:   dream.NewEngine(pool),
		interval: 24 * time.Hour, // nightly by default
		stopCh:   make(chan struct{}),
	}
}

// Start begins the cron loop.
func (c *DreamCron) Start() {
	go c.loop()
}

// Stop halts the cron loop.
func (c *DreamCron) Stop() {
	close(c.stopCh)
}

// RunOnce triggers a single dream cycle immediately.
func (c *DreamCron) RunOnce(ctx context.Context) (*dream.CycleResult, error) {
	slog.Info("dream cron: running single cycle")
	return c.engine.Cycle(ctx)
}

func (c *DreamCron) loop() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run immediately on start
	c.runCycle()

	for {
		select {
		case <-ticker.C:
			c.runCycle()
		case <-c.stopCh:
			return
		}
	}
}

func (c *DreamCron) runCycle() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	slog.Info("dream cron: starting scheduled consolidation")
	result, err := c.engine.Cycle(ctx)
	if err != nil {
		slog.Error("dream cron: cycle failed", "error", err)
		return
	}

	slog.Info("dream cron: cycle completed",
		"cycle_id", result.CycleID,
		"edges_strengthened", result.NREM.Strengthened,
		"edges_pruned", result.NREM.Pruned,
		"edges_bridged", result.REM.Bridged,
		"clusters_found", result.Insight.ClustersFound,
	)
}
