package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/collector"
	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

func TestTriggerIsNonBlocking(t *testing.T) {
	t.Parallel()

	s := newScheduler(SchedulerConfig{}, &fakeCollector{}, nil, nil)
	s.Trigger()
	s.Trigger()

	select {
	case <-s.trigger:
	default:
		t.Fatal("expected trigger signal")
	}

	select {
	case <-s.trigger:
		t.Fatal("expected only one queued trigger")
	default:
	}
}

func TestRunExecutesCollectionAndResolution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := &fakeCollector{
		statuses: []collector.NodeStatus{},
		runOnceFunc: func(context.Context) (store.CollectionRun, error) {
			cancel()
			return store.CollectionRun{ID: "run-1"}, nil
		},
	}
	fs := fakeSnapshotStore{
		snapshots: []store.NodeSnapshot{{ID: "snap-1", RunID: "run-1", NodeName: "node-a"}},
	}
	fr := &fakeResolver{}

	s := newScheduler(SchedulerConfig{CollectInterval: time.Millisecond, CollectJitter: 0}, fc, fr, fs)
	err := s.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if fc.runOnceCalls != 1 {
		t.Fatalf("RunOnce called %d times, want 1", fc.runOnceCalls)
	}
	if fr.resolveCalls != 1 {
		t.Fatalf("ResolveRun called %d times, want 1", fr.resolveCalls)
	}
}

func TestRunStartsWatchersForOnlineNodes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := make(chan core.NodeName, 2)
	fc := &fakeCollector{
		statuses: []collector.NodeStatus{
			{NodeName: "node-a", Online: true},
			{NodeName: "node-b", Online: false},
		},
		runOnceFunc: func(context.Context) (store.CollectionRun, error) { return store.CollectionRun{ID: "run-1"}, nil },
		watchNodeFunc: func(ctx context.Context, nodeName core.NodeName) error {
			calls <- nodeName
			<-ctx.Done()
			return ctx.Err()
		},
	}

	s := newScheduler(SchedulerConfig{CollectInterval: time.Millisecond, CollectJitter: 0}, fc, nil, nil)
	done := make(chan error, 1)
	go func() {
		done <- s.Run(ctx)
	}()

	select {
	case got := <-calls:
		if got != "node-a" {
			t.Fatalf("watcher started for %q, want node-a", got)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("expected watcher for online node")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected scheduler to stop after cancel")
	}
}

func TestRunStartsWatchersAfterInitialCyclePopulatesStatus(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := make(chan core.NodeName, 1)
	fc := &fakeCollector{}
	fc.runOnceFunc = func(context.Context) (store.CollectionRun, error) {
		fc.statuses = []collector.NodeStatus{{NodeName: "node-a", Online: true}}
		return store.CollectionRun{ID: "run-1"}, nil
	}
	fc.watchNodeFunc = func(ctx context.Context, nodeName core.NodeName) error {
		calls <- nodeName
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}

	s := newScheduler(SchedulerConfig{CollectInterval: time.Hour, CollectJitter: 0}, fc, nil, nil)
	done := make(chan error, 1)
	go func() {
		done <- s.Run(ctx)
	}()

	select {
	case got := <-calls:
		if got != "node-a" {
			t.Fatalf("watcher started for %q, want node-a", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected watcher after initial cycle populated status")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected scheduler to stop after cancel")
	}
}

func TestRunDoesNotStartWatchersWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := &fakeCollector{
		statuses: []collector.NodeStatus{{NodeName: "node-a", Online: true}},
		runOnceFunc: func(context.Context) (store.CollectionRun, error) {
			cancel()
			return store.CollectionRun{ID: "run-1"}, nil
		},
		watchNodeFunc: func(context.Context, core.NodeName) error {
			t.Fatal("WatchNode() should not be called when watchers are disabled")
			return nil
		},
	}

	s := newScheduler(SchedulerConfig{
		CollectInterval: time.Millisecond,
		CollectJitter:   0,
		DisableWatchers: true,
	}, fc, nil, nil)

	err := s.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
}

func TestSuperviseWatcherRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	attempts := 0
	fc := &fakeCollector{
		watchNodeFunc: func(ctx context.Context, nodeName core.NodeName) error {
			mu.Lock()
			attempts++
			current := attempts
			mu.Unlock()

			if current == 1 {
				return errors.New("boom")
			}
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	var sleeps []time.Duration
	s := newScheduler(SchedulerConfig{}, fc, nil, nil)
	s.sleep = func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.superviseWatcher(ctx, "node-a")
	}()

	<-done

	if attempts < 2 {
		t.Fatalf("WatchNode attempts = %d, want at least 2", attempts)
	}
	if len(sleeps) != 1 || sleeps[0] != initialBackoff {
		t.Fatalf("sleep backoff = %#v, want [%s]", sleeps, initialBackoff)
	}
}

type fakeCollector struct {
	statuses      []collector.NodeStatus
	runOnceFunc   func(context.Context) (store.CollectionRun, error)
	watchNodeFunc func(context.Context, core.NodeName) error
	getStatusFunc func(context.Context) ([]collector.NodeStatus, error)

	runOnceCalls int
}

func (f *fakeCollector) RunOnce(ctx context.Context) (store.CollectionRun, error) {
	f.runOnceCalls++
	if f.runOnceFunc != nil {
		return f.runOnceFunc(ctx)
	}
	return store.CollectionRun{}, nil
}

func (f *fakeCollector) WatchNode(ctx context.Context, nodeName core.NodeName) error {
	if f.watchNodeFunc != nil {
		return f.watchNodeFunc(ctx, nodeName)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeCollector) GetStatus(ctx context.Context) ([]collector.NodeStatus, error) {
	if f.getStatusFunc != nil {
		return f.getStatusFunc(ctx)
	}
	return f.statuses, nil
}

type fakeResolver struct {
	resolveCalls int
}

func (f *fakeResolver) ResolveRun(context.Context, store.CollectionRun, []store.NodeSnapshot) ([]store.TopologyEdge, error) {
	f.resolveCalls++
	return nil, nil
}

type fakeSnapshotStore struct {
	snapshots []store.NodeSnapshot
}

func (f fakeSnapshotStore) ListByRun(context.Context, core.ID) ([]store.NodeSnapshot, error) {
	return f.snapshots, nil
}
