package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// SQLiteStore groups all SQLite-backed repositories behind one DB handle.
type SQLiteStore struct {
	db *gorm.DB
}

// OpenSQLite opens a SQLite database and runs the schema migrations.
func OpenSQLite(path string) (*SQLiteStore, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	return s.db.AutoMigrate(
		&CollectionRunModel{},
		&NodeSnapshotModel{},
		&ListenPortModel{},
		&ContainerPortModel{},
		&TopologyEdgeModel{},
		&ProxyConfigInputModel{},
	)
}

// DB exposes the underlying GORM handle for wiring or transactions.
func (s *SQLiteStore) DB() *gorm.DB {
	return s.db
}

// Runs returns a run repository.
func (s *SQLiteStore) Runs() RunStore {
	return &sqliteRunStore{db: s.db}
}

// Snapshots returns a snapshot repository.
func (s *SQLiteStore) Snapshots() SnapshotStore {
	return &sqliteSnapshotStore{db: s.db}
}

// Edges returns an edge repository.
func (s *SQLiteStore) Edges() EdgeStore {
	return &sqliteEdgeStore{db: s.db}
}

// ProxyConfigs returns a proxy config repository.
func (s *SQLiteStore) ProxyConfigs() ProxyConfigStore {
	return &sqliteProxyConfigStore{db: s.db}
}

type sqliteRunStore struct{ db *gorm.DB }
type sqliteSnapshotStore struct{ db *gorm.DB }
type sqliteEdgeStore struct{ db *gorm.DB }
type sqliteProxyConfigStore struct{ db *gorm.DB }

func toGorm(ctx context.Context, db *gorm.DB) *gorm.DB {
	if ctx == nil {
		return db
	}
	return db.WithContext(ctx)
}

func ensureID(id core.ID) core.ID {
	if id != "" {
		return id
	}
	return core.NewID()
}

func timestampToTime(ts core.Timestamp) time.Time {
	return ts.Time()
}

func timeToTimestamp(t time.Time) core.Timestamp {
	return core.NewTimestamp(t)
}

type snapshotPayload struct {
	Ports      []ListenPort           `json:"ports"`
	Containers []Container            `json:"containers"`
	Services   []SwarmServicePort     `json:"services"`
	Forwards   []parser.ForwardAction `json:"forwards"`
	ProxyRules []parser.ProxyRule     `json:"proxy_rules,omitempty"`
}

func marshalSnapshotPayload(snapshot NodeSnapshot) (string, error) {
	payload := snapshotPayload{
		Ports:      snapshot.Ports,
		Containers: snapshot.Containers,
		Services:   snapshot.Services,
		Forwards:   snapshot.Forwards,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal snapshot payload: %w", err)
	}
	return string(data), nil
}

func unmarshalSnapshotPayload(raw string, snapshot *NodeSnapshot) error {
	if raw == "" {
		return nil
	}
	var payload snapshotPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return fmt.Errorf("unmarshal snapshot payload: %w", err)
	}
	snapshot.Ports = payload.Ports
	snapshot.Containers = payload.Containers
	snapshot.Services = payload.Services
	snapshot.Forwards = payload.Forwards
	if len(snapshot.Forwards) == 0 && len(payload.ProxyRules) > 0 {
		snapshot.Forwards = make([]parser.ForwardAction, 0, len(payload.ProxyRules))
		for _, rule := range payload.ProxyRules {
			snapshot.Forwards = append(snapshot.Forwards, parser.ForwardFromLegacyRule(rule))
		}
		snapshot.Forwards = parser.DedupeForwards(snapshot.Forwards)
	}
	return nil
}

func notFoundError(entity string, key any) error {
	return fmt.Errorf("%s not found: %v", entity, key)
}

func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
