package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
)

func TestRunStore(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	repo := db.Runs()
	ctx := context.Background()

	older := CollectionRun{
		ID:         "01JOLD00000000000000000000",
		StartedAt:  core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)),
		FinishedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 5, 0, time.UTC)),
		NodeCount:  1,
		ErrorCount: 1,
	}
	latest := CollectionRun{
		ID:         "01JNEW00000000000000000000",
		StartedAt:  core.NewTimestamp(time.Date(2026, 4, 17, 10, 1, 0, 0, time.UTC)),
		FinishedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 1, 5, 0, time.UTC)),
		NodeCount:  2,
		ErrorCount: 0,
	}

	for _, run := range []CollectionRun{older, latest} {
		if err := repo.Save(ctx, run); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	got, err := repo.Get(ctx, older.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.NodeCount != older.NodeCount {
		t.Fatalf("Get().NodeCount = %d, want %d", got.NodeCount, older.NodeCount)
	}

	list, err := repo.List(ctx, core.Filter{Limit: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != latest.ID {
		t.Fatalf("List() = %#v, want latest run only", list)
	}

	gotLatest, err := repo.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if gotLatest.ID != latest.ID {
		t.Fatalf("Latest().ID = %s, want %s", gotLatest.ID, latest.ID)
	}
}

func TestSnapshotStore(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	ctx := context.Background()

	runRepo := db.Runs()
	if err := runRepo.Save(ctx, CollectionRun{
		ID:        "01JRUN00000000000000000000",
		StartedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)),
	}); err != nil {
		t.Fatalf("Run Save() error = %v", err)
	}

	repo := db.Snapshots()
	first := NodeSnapshot{
		ID:          "01JSNAP0000000000000000000",
		RunID:       "01JRUN00000000000000000000",
		NodeName:    "node-a",
		TailscaleIP: "100.64.0.1",
		CollectedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 1, 0, time.UTC)),
		Ports: []ListenPort{{
			Addr:    "0.0.0.0",
			Port:    80,
			Proto:   "tcp",
			PID:     123,
			Process: "nginx",
		}},
		Containers: []Container{{
			ContainerID:   "abc",
			ContainerName: "web",
			Image:         "nginx:latest",
			State:         "running",
			Status:        "Up 1 hour",
			PublishedPorts: []ContainerPublishedPort{{
				HostPort:   8080,
				TargetPort: 80,
				Proto:      "tcp",
				Source:     "container",
			}},
		}},
		Services: []SwarmServicePort{{
			ServiceID:   "svc-1",
			ServiceName: "unipilot_api",
			HostPort:    3000,
			TargetPort:  3000,
			Proto:       "tcp",
			Mode:        "ingress",
		}},
		Forwards: []parser.ForwardAction{{
			Listener: parser.Listener{Port: 80},
			Target: parser.ForwardTarget{
				Raw:  "localhost:3000",
				Kind: parser.TargetKindAddress,
				Host: "localhost",
				Port: 3000,
			},
		}},
	}
	second := first
	second.ID = "01JSNAP0000000000000000001"
	second.CollectedAt = core.NewTimestamp(time.Date(2026, 4, 17, 10, 0, 2, 0, time.UTC))
	second.Error = "partial failure"

	for _, snapshot := range []NodeSnapshot{first, second} {
		if err := repo.Save(ctx, snapshot); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	got, err := repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Ports) != 1 || got.Ports[0].Process != "nginx" {
		t.Fatalf("Get().Ports = %#v, want nginx listen port", got.Ports)
	}
	if len(got.Containers) != 1 || got.Containers[0].ContainerName != "web" {
		t.Fatalf("Get().Containers = %#v, want web container", got.Containers)
	}
	if len(got.Containers[0].PublishedPorts) != 1 || got.Containers[0].PublishedPorts[0].HostPort != 8080 {
		t.Fatalf("Get().Containers[0].PublishedPorts = %#v, want preserved published ports", got.Containers[0].PublishedPorts)
	}
	if len(got.Services) != 1 || got.Services[0].ServiceName != "unipilot_api" {
		t.Fatalf("Get().Services = %#v, want unipilot_api service", got.Services)
	}
	if len(got.Forwards) != 1 || got.Forwards[0].Target.Raw != "localhost:3000" {
		t.Fatalf("Get().Forwards = %#v, want forward action", got.Forwards)
	}

	latest, err := repo.LatestByNode(ctx, "node-a")
	if err != nil {
		t.Fatalf("LatestByNode() error = %v", err)
	}
	if latest.ID != second.ID {
		t.Fatalf("LatestByNode().ID = %s, want %s", latest.ID, second.ID)
	}

	byRun, err := repo.ListByRun(ctx, first.RunID)
	if err != nil {
		t.Fatalf("ListByRun() error = %v", err)
	}
	if len(byRun) != 2 {
		t.Fatalf("ListByRun() len = %d, want 2", len(byRun))
	}
}

func TestEdgeStore(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	repo := db.Edges()
	ctx := context.Background()

	older := TopologyEdge{
		ID:          "01JEDGE0000000000000000000",
		RunID:       "01JRUN00000000000000000000",
		FromNode:    "node-a",
		FromPort:    80,
		ToNode:      "node-b",
		ToPort:      8080,
		Kind:        EdgeKindProxyPass,
		Resolved:    true,
		RawUpstream: "http://node-b:8080",
	}
	latestResolved := TopologyEdge{
		ID:                 "01JEDGE0000000000000000001",
		RunID:              "01JRUN00000000000000000001",
		FromNode:           "node-c",
		FromPort:           443,
		ToNode:             "node-d",
		ToPort:             8443,
		ToRuntimeNode:      "node-e",
		ToRuntimeContainer: "svc.1.xyz",
		Kind:               EdgeKindDirect,
		Resolved:           true,
		RawUpstream:        "https://node-d:8443",
	}
	latestUnresolved := TopologyEdge{
		ID:          "01JEDGE0000000000000000002",
		RunID:       "01JRUN00000000000000000001",
		FromNode:    "node-e",
		FromPort:    8080,
		Kind:        EdgeKindProxyPass,
		Resolved:    false,
		RawUpstream: "unknown:9000",
	}

	for _, edge := range []TopologyEdge{older, latestResolved, latestUnresolved} {
		if err := repo.Save(ctx, edge); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	got, err := repo.Get(ctx, latestResolved.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ToNode != latestResolved.ToNode {
		t.Fatalf("Get().ToNode = %s, want %s", got.ToNode, latestResolved.ToNode)
	}
	if got.ToRuntimeNode != latestResolved.ToRuntimeNode || got.ToRuntimeContainer != latestResolved.ToRuntimeContainer {
		t.Fatalf("Get() runtime = (%q,%q), want (%q,%q)", got.ToRuntimeNode, got.ToRuntimeContainer, latestResolved.ToRuntimeNode, latestResolved.ToRuntimeContainer)
	}

	latestEdges, err := repo.LatestEdges(ctx)
	if err != nil {
		t.Fatalf("LatestEdges() error = %v", err)
	}
	if len(latestEdges) != 2 {
		t.Fatalf("LatestEdges() len = %d, want 2", len(latestEdges))
	}

	unresolved, err := repo.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved() error = %v", err)
	}
	if len(unresolved) != 1 || unresolved[0].ID != latestUnresolved.ID {
		t.Fatalf("ListUnresolved() = %#v, want unresolved edge", unresolved)
	}
}

func TestProxyConfigStore(t *testing.T) {
	t.Parallel()

	db := openTestStore(t)
	repo := db.ProxyConfigs()
	ctx := context.Background()

	config := parser.ProxyConfigInput{
		ID:         "01JCFG00000000000000000000",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/nginx.conf",
		Content:    "server { listen 80; }",
		BundleFiles: map[string]string{
			"/etc/nginx/nginx.conf": "server { listen 80; }",
			"/etc/nginx/conf.d/app.conf": `
server {
    listen 8080;
}`,
		},
		UpdatedAt: core.NewTimestamp(time.Date(2026, 4, 17, 10, 5, 0, 0, time.UTC)),
	}
	if err := repo.Save(ctx, config); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	gotByID, err := repo.Get(ctx, config.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if gotByID.ConfigPath != config.ConfigPath {
		t.Fatalf("Get().ConfigPath = %s, want %s", gotByID.ConfigPath, config.ConfigPath)
	}
	if gotByID.Content != config.Content {
		t.Fatalf("Get().Content = %q, want %q", gotByID.Content, config.Content)
	}
	if len(gotByID.BundleFiles) != 2 || gotByID.BundleFiles["/etc/nginx/nginx.conf"] == "" {
		t.Fatalf("Get().BundleFiles = %#v", gotByID.BundleFiles)
	}

	gotByNodeAndPath, err := repo.GetByNodeAndPath(ctx, config.NodeName, config.ConfigPath)
	if err != nil {
		t.Fatalf("GetByNodeAndPath() error = %v", err)
	}
	if gotByNodeAndPath.Kind != config.Kind {
		t.Fatalf("GetByNodeAndPath().Kind = %s, want %s", gotByNodeAndPath.Kind, config.Kind)
	}

	second := parser.ProxyConfigInput{
		ID:         "01JCFG00000000000000000001",
		NodeName:   "node-a",
		Kind:       "nginx",
		ConfigPath: "/etc/nginx/conf.d/app.conf",
		Content:    "server { listen 443; }",
		UpdatedAt:  core.NewTimestamp(time.Date(2026, 4, 17, 10, 6, 0, 0, time.UTC)),
	}
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	byNode, err := repo.ListByNode(ctx, config.NodeName)
	if err != nil {
		t.Fatalf("ListByNode() error = %v", err)
	}
	if len(byNode) != 2 {
		t.Fatalf("ListByNode() len = %d, want 2", len(byNode))
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAll() len = %d, want 2", len(all))
	}
}

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tailflow-test.sqlite")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	return store
}
