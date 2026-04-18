package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"gorm.io/gorm"
)

func (s *sqliteProxyConfigStore) Get(ctx context.Context, id core.ID) (parser.ProxyConfigInput, error) {
	var model ProxyConfigInputModel
	err := toGorm(ctx, s.db).First(&model, "id = ?", id).Error
	if isRecordNotFound(err) {
		return parser.ProxyConfigInput{}, notFoundError("proxy config", id)
	}
	if err != nil {
		return parser.ProxyConfigInput{}, fmt.Errorf("get proxy config: %w", err)
	}
	return proxyConfigFromModel(model), nil
}

func (s *sqliteProxyConfigStore) List(ctx context.Context, filter core.Filter) ([]parser.ProxyConfigInput, error) {
	var models []ProxyConfigInputModel
	query := toGorm(ctx, s.db).Order("node_name ASC, config_path ASC")
	if filter.NodeName != "" {
		query = query.Where("node_name = ?", filter.NodeName)
	}
	if filter.Since != nil {
		query = query.Where("updated_at >= ?", filter.Since.Time())
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list proxy configs: %w", err)
	}
	return proxyConfigsFromModels(models), nil
}

func (s *sqliteProxyConfigStore) Save(ctx context.Context, config parser.ProxyConfigInput) error {
	model := proxyConfigToModel(config)
	model.ID = string(ensureID(core.ID(model.ID)))
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = timestampToTime(core.NowTimestamp())
	}
	if err := toGorm(ctx, s.db).Save(&model).Error; err != nil {
		return fmt.Errorf("save proxy config: %w", err)
	}
	return nil
}

func (s *sqliteProxyConfigStore) Delete(ctx context.Context, id core.ID) error {
	result := toGorm(ctx, s.db).Delete(&ProxyConfigInputModel{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("delete proxy config: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return notFoundError("proxy config", id)
	}
	return nil
}

func (s *sqliteProxyConfigStore) GetByNodeAndPath(ctx context.Context, nodeName core.NodeName, configPath string) (parser.ProxyConfigInput, error) {
	var model ProxyConfigInputModel
	err := toGorm(ctx, s.db).First(&model, "node_name = ? AND config_path = ?", nodeName, configPath).Error
	if isRecordNotFound(err) {
		return parser.ProxyConfigInput{}, notFoundError("proxy config", fmt.Sprintf("%s:%s", nodeName, configPath))
	}
	if err != nil {
		return parser.ProxyConfigInput{}, fmt.Errorf("get proxy config by node and path: %w", err)
	}
	return proxyConfigFromModel(model), nil
}

func (s *sqliteProxyConfigStore) ListByNode(ctx context.Context, nodeName core.NodeName) ([]parser.ProxyConfigInput, error) {
	return s.List(ctx, core.Filter{NodeName: nodeName})
}

func (s *sqliteProxyConfigStore) ListAll(ctx context.Context) ([]parser.ProxyConfigInput, error) {
	return s.List(ctx, core.Filter{})
}

func proxyConfigToModel(config parser.ProxyConfigInput) ProxyConfigInputModel {
	bundleJSON := ""
	if len(config.BundleFiles) > 0 {
		if data, err := json.Marshal(config.BundleFiles); err == nil {
			bundleJSON = string(data)
		}
	}
	return ProxyConfigInputModel{
		ID:         string(config.ID),
		NodeName:   string(config.NodeName),
		Kind:       config.Kind,
		ConfigPath: config.ConfigPath,
		Content:    config.Content,
		BundleJSON: bundleJSON,
		UpdatedAt:  timestampToTime(config.UpdatedAt),
	}
}

func proxyConfigFromModel(model ProxyConfigInputModel) parser.ProxyConfigInput {
	var bundleFiles map[string]string
	if model.BundleJSON != "" {
		_ = json.Unmarshal([]byte(model.BundleJSON), &bundleFiles)
	}
	return parser.ProxyConfigInput{
		ID:          core.ID(model.ID),
		NodeName:    core.NodeName(model.NodeName),
		Kind:        model.Kind,
		ConfigPath:  model.ConfigPath,
		Content:     model.Content,
		BundleFiles: bundleFiles,
		UpdatedAt:   timeToTimestamp(model.UpdatedAt),
	}
}

func proxyConfigsFromModels(models []ProxyConfigInputModel) []parser.ProxyConfigInput {
	configs := make([]parser.ProxyConfigInput, 0, len(models))
	for _, model := range models {
		configs = append(configs, proxyConfigFromModel(model))
	}
	return configs
}

var _ ProxyConfigStore = (*sqliteProxyConfigStore)(nil)
var _ = gorm.ErrRecordNotFound
