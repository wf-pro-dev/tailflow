package store

import (
	"context"
	"fmt"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"gorm.io/gorm"
)

func (s *sqliteRunStore) Get(ctx context.Context, id core.ID) (CollectionRun, error) {
	var model CollectionRunModel
	err := toGorm(ctx, s.db).First(&model, "id = ?", id).Error
	if isRecordNotFound(err) {
		return CollectionRun{}, notFoundError("collection run", id)
	}
	if err != nil {
		return CollectionRun{}, fmt.Errorf("get collection run: %w", err)
	}
	return runFromModel(model), nil
}

func (s *sqliteRunStore) List(ctx context.Context, filter core.Filter) ([]CollectionRun, error) {
	var models []CollectionRunModel
	query := toGorm(ctx, s.db).Order("started_at DESC")
	if filter.Since != nil {
		query = query.Where("started_at >= ?", filter.Since.Time())
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list collection runs: %w", err)
	}
	return runsFromModels(models), nil
}

func (s *sqliteRunStore) Save(ctx context.Context, run CollectionRun) error {
	model := runToModel(run)
	model.ID = string(ensureID(core.ID(model.ID)))
	return toGorm(ctx, s.db).Save(&model).Error
}

func (s *sqliteRunStore) Delete(ctx context.Context, id core.ID) error {
	result := toGorm(ctx, s.db).Delete(&CollectionRunModel{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("delete collection run: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return notFoundError("collection run", id)
	}
	return nil
}

func (s *sqliteRunStore) Latest(ctx context.Context) (CollectionRun, error) {
	var model CollectionRunModel
	err := toGorm(ctx, s.db).Order("started_at DESC").Limit(1).Take(&model).Error
	if isRecordNotFound(err) {
		return CollectionRun{}, notFoundError("collection run", "latest")
	}
	if err != nil {
		return CollectionRun{}, fmt.Errorf("get latest collection run: %w", err)
	}
	return runFromModel(model), nil
}

func runToModel(run CollectionRun) CollectionRunModel {
	return CollectionRunModel{
		ID:         string(run.ID),
		StartedAt:  timestampToTime(run.StartedAt),
		FinishedAt: timestampToTime(run.FinishedAt),
		NodeCount:  run.NodeCount,
		ErrorCount: run.ErrorCount,
	}
}

func runFromModel(model CollectionRunModel) CollectionRun {
	return CollectionRun{
		ID:         core.ID(model.ID),
		StartedAt:  timeToTimestamp(model.StartedAt),
		FinishedAt: timeToTimestamp(model.FinishedAt),
		NodeCount:  model.NodeCount,
		ErrorCount: model.ErrorCount,
	}
}

func runsFromModels(models []CollectionRunModel) []CollectionRun {
	runs := make([]CollectionRun, 0, len(models))
	for _, model := range models {
		runs = append(runs, runFromModel(model))
	}
	return runs
}

var _ RunStore = (*sqliteRunStore)(nil)
var _ = gorm.ErrRecordNotFound
