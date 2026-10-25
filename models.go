package mcpintegration

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// This file owns the MCP integration's persistence models. They previously lived
// in backend/database and were shared with the core; the integration now owns
// them and registers MCPIntegrationConfig for migration through
// Definition.ModelProviders, so it no longer imports backend/... packages.
//
// The structs intentionally mirror the core schema field-for-field (table names,
// column tags and indexes) so existing databases and the core's own MCP helpers
// keep working unchanged.

// Model is the shared gorm base model (UUID primary metadata plus timestamps),
// mirroring the core database.Model so integration tables keep their schema.
type Model struct {
	UUID      string         `gorm:"type:text;" json:"uuid"`
	ID        uint           `gorm:"primarykey" json:"-"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-" gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (b *Model) BeforeCreate(tx *gorm.DB) (err error) {
	b.UUID = uuid.New().String()
	return
}

// User is a read-only projection of the core users table. It is used to resolve
// bootstrap owner references and as the foreign-key target of
// MCPIntegrationConfig.OwnerUser; it is never registered for migration, so the
// core keeps ownership of the users schema.
type User struct {
	ID       uint   `gorm:"primarykey" json:"-"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"-"`
	IsAdmin  bool   `json:"is_admin"`
}

func (User) TableName() string {
	return "users"
}

// MCPIntegrationConfig stores owner-scoped external MCP server configuration.
// The core keeps its own copy for its internal MCP helpers; this integration
// owns the migration/registration of the table.
type MCPIntegrationConfig struct {
	Model
	OwnerUserId uint            `json:"owner_user_id" gorm:"index;uniqueIndex:idx_mcp_integration_owner_name"`
	OwnerUser   User            `json:"-" gorm:"foreignKey:OwnerUserId;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Name        string          `json:"name" gorm:"size:160;uniqueIndex:idx_mcp_integration_owner_name"`
	Config      json.RawMessage `json:"config" gorm:"type:jsonb"`
	AuthData    json.RawMessage `json:"auth_data" gorm:"type:jsonb"`
	AuthSession json.RawMessage `json:"auth_session" gorm:"type:jsonb"`
	Enabled     bool            `json:"enabled" gorm:"default:true;index"`
}

// BotRuntimeConfig is a read-only projection of the core bot_runtime_configs
// table used to refresh owner bot MCP tool snapshots.
type BotRuntimeConfig struct {
	Model
	BotUserId           uint            `json:"bot_user_id" gorm:"index"`
	DefaultSharedConfig json.RawMessage `json:"default_shared_config" gorm:"type:jsonb"`
}

func (BotRuntimeConfig) TableName() string {
	return "bot_runtime_configs"
}

// SharedChatConfig is a read-only projection of the core shared_chat_configs
// table used to refresh owner-shared MCP tool snapshots.
type SharedChatConfig struct {
	Model
	ConfigData json.RawMessage `json:"config_data" gorm:"type:jsonb"`
}

func (SharedChatConfig) TableName() string {
	return "shared_chat_configs"
}
