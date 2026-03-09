package gpu

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Poller periodically re-detects GPUs and publishes snapshots.
type Poller struct {
	detector *Detector
	interval time.Duration
	logger   *slog.Logger

	mu     sync.RWMutex
	latest Snapshot
	subMu  sync.Mutex
	subs   []chan Snapshot
}

// NewPoller creates a Poller that re-detects GPUs at the given interval.
func NewPoller(detector *Detector, interval time.Duration, logger *slog.Logger) *Poller {
	return &Poller{
		detector: detector,
		interval: interval,
		logger:   logger,
	}
}

// Start launches the polling goroutine. It performs an initial detection immediately,
// then polls at the configured interval. Cancel the context to stop.
func (p *Poller) Start(ctx context.Context) {
	// Initial detection
	snap := p.detector.Detect(ctx)
	p.setLatest(snap)

	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				p.logger.Debug("gpu poller stopped")
				return
			case <-ticker.C:
				snap := p.detector.Detect(ctx)
				p.setLatest(snap)
				p.publish(snap)
			}
		}
	}()
}

// Latest returns the most recent GPU snapshot.
func (p *Poller) Latest() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.latest
}

// Subscribe returns a channel that receives GPU snapshots on each poll.
// The channel is buffered with size 1; slow consumers drop updates.
func (p *Poller) Subscribe() <-chan Snapshot {
	ch := make(chan Snapshot, 1)
	p.subMu.Lock()
	p.subs = append(p.subs, ch)
	p.subMu.Unlock()
	return ch
}

func (p *Poller) setLatest(snap Snapshot) {
	p.mu.Lock()
	p.latest = snap
	p.mu.Unlock()
}

func (p *Poller) publish(snap Snapshot) {
	p.subMu.Lock()
	defer p.subMu.Unlock()

	for _, ch := range p.subs {
		select {
		case ch <- snap:
		default:
			// Slow consumer, drop update
		}
	}
}
