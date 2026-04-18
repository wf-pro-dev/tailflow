package store

import (
	"context"
	"fmt"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"gorm.io/gorm"
)

func (s *sqliteSnapshotStore) Get(ctx context.Context, id core.ID) (NodeSnapshot, error) {
	return s.getByField(ctx, "id", id)
}

func (s *sqliteSnapshotStore) List(ctx context.Context, filter core.Filter) ([]NodeSnapshot, error) {
	var models []NodeSnapshotModel
	query := toGorm(ctx, s.db).Order("collected_at DESC")
	if filter.NodeName != "" {
		query = query.Where("node_name = ?", filter.NodeName)
	}
	if filter.Since != nil {
		query = query.Where("collected_at >= ?", filter.Since.Time())
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return snapshotsFromModels(models)
}

func (s *sqliteSnapshotStore) Save(ctx context.Context, snapshot NodeSnapshot) error {
	snapshot.ID = ensureID(snapshot.ID)
	rawJSON, err := marshalSnapshotPayload(snapshot)
	if err != nil {
		return err
	}

	model := NodeSnapshotModel{
		ID:          string(snapshot.ID),
		RunID:       string(snapshot.RunID),
		NodeName:    string(snapshot.NodeName),
		TailscaleIP: snapshot.TailscaleIP,
		CollectedAt: timestampToTime(snapshot.CollectedAt),
		RawJSON:     rawJSON,
		Error:       snapshot.Error,
	}

	return toGorm(ctx, s.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&model).Error; err != nil {
			return fmt.Errorf("save snapshot: %w", err)
		}
		if err := tx.Where("snapshot_id = ?", model.ID).Delete(&ListenPortModel{}).Error; err != nil {
			return fmt.Errorf("clear snapshot listen ports: %w", err)
		}
		if err := tx.Where("snapshot_id = ?", model.ID).Delete(&ContainerPortModel{}).Error; err != nil {
			return fmt.Errorf("clear snapshot container ports: %w", err)
		}

		for _, port := range snapshot.Ports {
			if err := tx.Create(&ListenPortModel{
				ID:         string(ensureID(port.ID)),
				SnapshotID: model.ID,
				NodeName:   model.NodeName,
				Addr:       port.Addr,
				Port:       port.Port,
				Proto:      port.Proto,
				PID:        port.PID,
				Process:    port.Process,
			}).Error; err != nil {
				return fmt.Errorf("save listen port: %w", err)
			}
		}

		for _, port := range snapshot.Containers {
			if err := tx.Create(&ContainerPortModel{
				ID:            string(ensureID(port.ID)),
				SnapshotID:    model.ID,
				NodeName:      model.NodeName,
				ContainerID:   port.ContainerID,
				ContainerName: port.ContainerName,
				HostPort:      port.HostPort,
				ContainerPort: port.ContainerPort,
				Proto:         port.Proto,
			}).Error; err != nil {
				return fmt.Errorf("save container port: %w", err)
			}
		}

		return nil
	})
}

func (s *sqliteSnapshotStore) Delete(ctx context.Context, id core.ID) error {
	return toGorm(ctx, s.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("snapshot_id = ?", id).Delete(&ListenPortModel{}).Error; err != nil {
			return fmt.Errorf("delete snapshot listen ports: %w", err)
		}
		if err := tx.Where("snapshot_id = ?", id).Delete(&ContainerPortModel{}).Error; err != nil {
			return fmt.Errorf("delete snapshot container ports: %w", err)
		}
		result := tx.Delete(&NodeSnapshotModel{}, "id = ?", id)
		if result.Error != nil {
			return fmt.Errorf("delete snapshot: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return notFoundError("snapshot", id)
		}
		return nil
	})
}

func (s *sqliteSnapshotStore) ListByRun(ctx context.Context, runID core.ID) ([]NodeSnapshot, error) {
	var models []NodeSnapshotModel
	err := toGorm(ctx, s.db).Where("run_id = ?", runID).Order("collected_at DESC").Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("list snapshots by run: %w", err)
	}
	return snapshotsFromModels(models)
}

func (s *sqliteSnapshotStore) LatestByNode(ctx context.Context, nodeName core.NodeName) (NodeSnapshot, error) {
	return s.getOne(ctx, toGorm(ctx, s.db).Where("node_name = ?", nodeName).Order("collected_at DESC").Limit(1), "latest snapshot", nodeName)
}

func (s *sqliteSnapshotStore) getByField(ctx context.Context, field string, value any) (NodeSnapshot, error) {
	return s.getOne(ctx, toGorm(ctx, s.db).Where(field+" = ?", value).Limit(1), "snapshot", value)
}

func (s *sqliteSnapshotStore) getOne(_ context.Context, query *gorm.DB, entity string, key any) (NodeSnapshot, error) {
	var model NodeSnapshotModel
	err := query.Take(&model).Error
	if isRecordNotFound(err) {
		return NodeSnapshot{}, notFoundError(entity, key)
	}
	if err != nil {
		return NodeSnapshot{}, fmt.Errorf("get %s: %w", entity, err)
	}
	return snapshotFromModel(model)
}

func snapshotFromModel(model NodeSnapshotModel) (NodeSnapshot, error) {
	snapshot := NodeSnapshot{
		ID:          core.ID(model.ID),
		RunID:       core.ID(model.RunID),
		NodeName:    core.NodeName(model.NodeName),
		TailscaleIP: model.TailscaleIP,
		CollectedAt: timeToTimestamp(model.CollectedAt),
		Error:       model.Error,
	}
	if err := unmarshalSnapshotPayload(model.RawJSON, &snapshot); err != nil {
		return NodeSnapshot{}, err
	}
	return snapshot, nil
}

func snapshotsFromModels(models []NodeSnapshotModel) ([]NodeSnapshot, error) {
	snapshots := make([]NodeSnapshot, 0, len(models))
	for _, model := range models {
		snapshot, err := snapshotFromModel(model)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

var _ SnapshotStore = (*sqliteSnapshotStore)(nil)
var _ = gorm.ErrRecordNotFound
