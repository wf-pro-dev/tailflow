package store

import (
	"context"
	"fmt"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"gorm.io/gorm"
)

func (s *sqliteEdgeStore) Get(ctx context.Context, id core.ID) (TopologyEdge, error) {
	var model TopologyEdgeModel
	err := toGorm(ctx, s.db).First(&model, "id = ?", id).Error
	if isRecordNotFound(err) {
		return TopologyEdge{}, notFoundError("topology edge", id)
	}
	if err != nil {
		return TopologyEdge{}, fmt.Errorf("get topology edge: %w", err)
	}
	return edgeFromModel(model), nil
}

func (s *sqliteEdgeStore) List(ctx context.Context, filter core.Filter) ([]TopologyEdge, error) {
	var models []TopologyEdgeModel
	query := toGorm(ctx, s.db).Order("from_node ASC, from_port ASC")
	if filter.NodeName != "" {
		query = query.Where("from_node = ? OR to_node = ?", filter.NodeName, filter.NodeName)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list topology edges: %w", err)
	}
	return edgesFromModels(models), nil
}

func (s *sqliteEdgeStore) Save(ctx context.Context, edge TopologyEdge) error {
	model := edgeToModel(edge)
	model.ID = string(ensureID(core.ID(model.ID)))
	if err := toGorm(ctx, s.db).Save(&model).Error; err != nil {
		return fmt.Errorf("save topology edge: %w", err)
	}
	return nil
}

func (s *sqliteEdgeStore) Delete(ctx context.Context, id core.ID) error {
	result := toGorm(ctx, s.db).Delete(&TopologyEdgeModel{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("delete topology edge: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return notFoundError("topology edge", id)
	}
	return nil
}

func (s *sqliteEdgeStore) ListByRun(ctx context.Context, runID core.ID) ([]TopologyEdge, error) {
	var models []TopologyEdgeModel
	err := toGorm(ctx, s.db).Where("run_id = ?", runID).Order("from_node ASC, from_port ASC").Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("list topology edges by run: %w", err)
	}
	return edgesFromModels(models), nil
}

func (s *sqliteEdgeStore) LatestEdges(ctx context.Context) ([]TopologyEdge, error) {
	var latestRunID string
	err := toGorm(ctx, s.db).Model(&TopologyEdgeModel{}).Select("run_id").Order("run_id DESC").Limit(1).Take(&latestRunID).Error
	if isRecordNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest edge run id: %w", err)
	}
	return s.ListByRun(ctx, core.ID(latestRunID))
}

func (s *sqliteEdgeStore) ListUnresolved(ctx context.Context) ([]TopologyEdge, error) {
	var models []TopologyEdgeModel
	err := toGorm(ctx, s.db).Where("resolved = ?", false).Order("from_node ASC, from_port ASC").Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("list unresolved topology edges: %w", err)
	}
	return edgesFromModels(models), nil
}

func edgeToModel(edge TopologyEdge) TopologyEdgeModel {
	return TopologyEdgeModel{
		ID:                 string(edge.ID),
		RunID:              string(edge.RunID),
		FromNode:           string(edge.FromNode),
		FromPort:           edge.FromPort,
		FromProcess:        edge.FromProcess,
		FromContainer:      edge.FromContainer,
		ToNode:             string(edge.ToNode),
		ToPort:             edge.ToPort,
		ToProcess:          edge.ToProcess,
		ToContainer:        edge.ToContainer,
		ToService:          edge.ToService,
		ToRuntimeNode:      string(edge.ToRuntimeNode),
		ToRuntimeContainer: edge.ToRuntimeContainer,
		Kind:               string(edge.Kind),
		Resolved:           edge.Resolved,
		RawUpstream:        edge.RawUpstream,
	}
}

func edgeFromModel(model TopologyEdgeModel) TopologyEdge {
	return TopologyEdge{
		ID:                 core.ID(model.ID),
		RunID:              core.ID(model.RunID),
		FromNode:           core.NodeName(model.FromNode),
		FromPort:           model.FromPort,
		FromProcess:        model.FromProcess,
		FromContainer:      model.FromContainer,
		ToNode:             core.NodeName(model.ToNode),
		ToPort:             model.ToPort,
		ToProcess:          model.ToProcess,
		ToContainer:        model.ToContainer,
		ToService:          model.ToService,
		ToRuntimeNode:      core.NodeName(model.ToRuntimeNode),
		ToRuntimeContainer: model.ToRuntimeContainer,
		Kind:               EdgeKind(model.Kind),
		Resolved:           model.Resolved,
		RawUpstream:        model.RawUpstream,
	}
}

func edgesFromModels(models []TopologyEdgeModel) []TopologyEdge {
	edges := make([]TopologyEdge, 0, len(models))
	for _, model := range models {
		edges = append(edges, edgeFromModel(model))
	}
	return edges
}

var _ EdgeStore = (*sqliteEdgeStore)(nil)
var _ = gorm.ErrRecordNotFound
