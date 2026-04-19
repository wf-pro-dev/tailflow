package store

import "time"

type CollectionRunModel struct {
	ID         string    `gorm:"primaryKey"`
	StartedAt  time.Time `gorm:"index"`
	FinishedAt time.Time
	NodeCount  int
	ErrorCount int
}

type NodeSnapshotModel struct {
	ID          string `gorm:"primaryKey"`
	RunID       string `gorm:"index;not null"`
	NodeName    string `gorm:"index;not null"`
	TailscaleIP string
	DNSName     string
	CollectedAt time.Time `gorm:"index"`
	RawJSON     string    `gorm:"column:raw_json;type:text"`
	Error       string
}

type ListenPortModel struct {
	ID         string `gorm:"primaryKey"`
	SnapshotID string `gorm:"index;not null"`
	NodeName   string `gorm:"index;not null"`
	Addr       string
	Port       uint16 `gorm:"index"`
	Proto      string
	PID        int
	Process    string
}

type ContainerPortModel struct {
	ID            string `gorm:"primaryKey"`
	SnapshotID    string `gorm:"index;not null"`
	NodeName      string `gorm:"index;not null"`
	ContainerID   string
	ContainerName string `gorm:"index"`
	HostPort      uint16 `gorm:"index"`
	ContainerPort uint16
	Proto         string
}

type TopologyEdgeModel struct {
	ID                 string `gorm:"primaryKey"`
	RunID              string `gorm:"index;not null"`
	FromNode           string `gorm:"index"`
	FromPort           uint16
	FromProcess        string
	FromContainer      string
	ToNode             string `gorm:"index"`
	ToPort             uint16
	ToProcess          string
	ToContainer        string
	ToService          string
	ToRuntimeNode      string `gorm:"index"`
	ToRuntimeContainer string
	Kind               string
	Resolved           bool `gorm:"index"`
	RawUpstream        string
}

type ProxyConfigInputModel struct {
	ID         string `gorm:"primaryKey"`
	NodeName   string `gorm:"uniqueIndex:idx_proxy_config_node_path;not null"`
	Kind       string
	ConfigPath string `gorm:"uniqueIndex:idx_proxy_config_node_path;not null"`
	Content    string `gorm:"type:text"`
	BundleJSON string `gorm:"column:bundle_json;type:text"`
	UpdatedAt  time.Time
}
