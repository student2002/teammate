package types

import (
	"encoding/json"
	"time"
)

// CustomTheme is a runtime-uploaded theme shared across the deployment.
// Vars maps every THEME_VARS contract variable name (e.g. "--bg") to a color value.
type CustomTheme struct {
	ID        string          `json:"id"`
	NameZh    string          `json:"name_zh"`
	NameEn    string          `json:"name_en"`
	Icon      string          `json:"icon"`
	Vars      json.RawMessage `json:"vars"`
	CreatedBy *string         `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// CreateCustomThemeParams is the domain parameter struct for uploading a custom theme.
type CreateCustomThemeParams struct {
	ID        string
	NameZh    string
	NameEn    string
	Icon      string
	Vars      json.RawMessage
	CreatedBy *string
}
