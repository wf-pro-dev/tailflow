package scheduler

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

const (
	defaultCollectInterval = 30 * time.Second
	defaultCollectJitter   = 5 * time.Second
	defaultNodeTimeout     = 10 * time.Second
	initialBackoff         = 250 * time.Millisecond
	maxBackoff             = 10 * time.Second
)

type collectorRunner interface {
	RunOnce(context.Context) (store.CollectionRun, error)
	WatchNode(context.Context, core.NodeName) error
	GetStatus(context.Context) ([]collector.NodeStatus, error)
}

type resolverRunner interface {
	ResolveRun(context.Context, store.CollectionRun, []store.NodeSnapshot) ([]store.TopologyEdge, error)
}

type snapshotLister interface {
	ListByRun(context.Context, core.ID) ([]store.NodeSnapshot, error)
}

type sleeper func(context.Context, time.Duration) error

// SchedulerConfig controls collection cadence and per-node operation timing.
type SchedulerConfig struct {
	CollectInterval time.Duration
	CollectJitter   time.Duration
	NodeTimeout     time.Duration
	DisableWatchers bool
}

// Scheduler owns the periodic collection loop and watch workers.
type Scheduler struct {
	config    SchedulerConfig
	collector collectorRunner
	resolver  resolverRunner
	snapshots snapshotLister
	trigger   chan struct{}
	watchMu   sync.Mutex
	watching  map[core.NodeName]struct{}

	randMu sync.Mutex
	rnd    *rand.Rand

	sleep sleeper
	now   func() time.Time
}

func NewScheduler(cfg SchedulerConfig, c collectorRunner, r resolverRunner, snapshots snapshotLister) *Scheduler {
	return newScheduler(cfg, c, r, snapshots)
}

func newScheduler(cfg SchedulerConfig, c collectorRunner, r resolverRunner, snapshots snapshotLister) *Scheduler {
	cfg = withDefaults(cfg)
	return &Scheduler{
		config:    cfg,
		collector: c,
		resolver:  r,
		snapshots: snapshots,
		trigger:   make(chan struct{}, 1),
		watching:  make(map[core.NodeName]struct{}),
		rnd:       rand.New(rand.NewSource(time.Now().UnixNano())),
		sleep: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
		now: time.Now,
	}
}

func withDefaults(cfg SchedulerConfig) SchedulerConfig {
	if cfg.CollectInterval <= 0 {
		cfg.CollectInterval = defaultCollectInterval
	}
	if cfg.CollectJitter < 0 {
		cfg.CollectJitter = 0
	}
	if cfg.NodeTimeout <= 0 {
		cfg.NodeTimeout = defaultNodeTimeout
	}
	return cfg
}

// Run starts watcher supervision and the collection loop until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.collector == nil {
		return errors.New("scheduler: collector is required")
	}

	for {
		if err := s.runCycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		if !s.config.DisableWatchers {
			if err := s.ensureWatchers(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}

		wait := s.nextInterval()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.trigger:
			continue
		default:
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-s.trigger:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Scheduler) ensureWatchers(ctx context.Context) error {
	statuses, err := s.collector.GetStatus(ctx)
	if err != nil {
		return err
	}

	for _, status := range statuses {
		if !status.Online {
			continue
		}
		s.startWatcher(ctx, status.NodeName)
	}
	return nil
}

func (s *Scheduler) startWatcher(ctx context.Context, nodeName core.NodeName) {
	s.watchMu.Lock()
	if _, ok := s.watching[nodeName]; ok {
		s.watchMu.Unlock()
		return
	}
	s.watching[nodeName] = struct{}{}
	s.watchMu.Unlock()

	go func() {
		defer func() {
			s.watchMu.Lock()
			delete(s.watching, nodeName)
			s.watchMu.Unlock()
		}()
		s.superviseWatcher(ctx, nodeName)
	}()
}

func (s *Scheduler) runCycle(ctx context.Context) error {
	collectCtx, cancelCollect := context.WithTimeout(ctx, s.config.CollectInterval+s.config.NodeTimeout)
	run, err := s.collector.RunOnce(collectCtx)
	cancelCollect()
	if err != nil && run.ID == "" {
		return err
	}

	if s.resolver != nil && s.snapshots != nil && run.ID != "" {
		resolveCtx, cancelResolve := context.WithTimeout(ctx, s.config.NodeTimeout)
		defer cancelResolve()

		snapshots, listErr := s.snapshots.ListByRun(resolveCtx, run.ID)
		if listErr != nil {
			return listErr
		}
		if _, resolveErr := s.resolver.ResolveRun(resolveCtx, run, snapshots); resolveErr != nil {
			return resolveErr
		}
	}

	if errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// Trigger requests an immediate collection cycle without blocking.
func (s *Scheduler) Trigger() {
	select {
	case s.trigger <- struct{}{}:
		log.Printf("scheduler: collection trigger queued")
	default:
		log.Printf("scheduler: collection trigger already pending")
	}
}

// superviseWatcher runs WatchNode and restarts it with backoff until ctx ends.
func (s *Scheduler) superviseWatcher(ctx context.Context, nodeName core.NodeName) {
	backoff := initialBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		watchCtx, cancel := context.WithCancel(ctx)
		err := s.collector.WatchNode(watchCtx, nodeName)
		cancel()

		if err == nil || errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				return
			}
			backoff = initialBackoff
		} else {
			if s.sleep(ctx, backoff) != nil {
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (s *Scheduler) nextInterval() time.Duration {
	if s.config.CollectJitter == 0 {
		return s.config.CollectInterval
	}

	s.randMu.Lock()
	defer s.randMu.Unlock()
	jitter := time.Duration(s.rnd.Int63n(int64(s.config.CollectJitter)*2+1)) - s.config.CollectJitter
	next := s.config.CollectInterval + jitter
	if next < 0 {
		return 0
	}
	return next
}
