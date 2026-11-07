// themes.go handles runtime-uploaded custom themes: list / upload / delete.
// Themes are deployment-shared UI resources; the payload is validated here (entry
// layer, once): id format, icon enum, exact THEME_VARS coverage, and color-value
// whitelist — the frontend injects vars verbatim, so anything that is not a plain
// color is rejected.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// themeVarsContract is the exact variable set a custom theme must define,
// mirroring web/lib/themes/types.ts THEME_VARS (keep both lists in sync).
var themeVarsContract = []string{
	"--bg", "--surface", "--surface-2", "--surface-3", "--line", "--line-strong",
	"--fg", "--fg-muted", "--fg-subtle", "--fg-faint",
	"--primary", "--primary-strong", "--primary-fg",
	"--accent", "--accent-strong", "--accent-fg",
	"--success", "--success-strong", "--success-fg",
	"--warning", "--warning-strong", "--warning-fg",
	"--danger", "--danger-strong", "--danger-fg",
	"--shadow",
}

var (
	themeIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}[a-z0-9]$`)
	themeHexPattern    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	themeShadowPattern = regexp.MustCompile(`^rgba\(\s*(25[0-5]|2[0-4]\d|1?\d?\d)\s*,\s*(25[0-5]|2[0-4]\d|1?\d?\d)\s*,\s*(25[0-5]|2[0-4]\d|1?\d?\d)\s*,\s*(0|1|0\.\d+)\s*\)$`)
)

var themeIconAllowed = map[string]bool{"moon": true, "sun": true, "sparkles": true, "zap": true, "contrast": true}

type createCustomThemeRequest struct {
	ID     string            `json:"id"`
	NameZh string            `json:"name_zh"`
	NameEn string            `json:"name_en"`
	Icon   string            `json:"icon"`
	Vars   map[string]string `json:"vars"`
}

type customThemeResponse struct {
	ID        string            `json:"id"`
	NameZh    string            `json:"name_zh"`
	NameEn    string            `json:"name_en"`
	Icon      string            `json:"icon"`
	Vars      map[string]string `json:"vars"`
	CreatedBy *string           `json:"created_by"`
	CreatedAt string            `json:"created_at"`
}

// validateThemePayload checks the upload body against the theme contract.
// Returns a user-facing error message when invalid.
func validateThemePayload(req *createCustomThemeRequest) error {
	if !themeIDPattern.MatchString(req.ID) {
		return fmt.Errorf("id must be kebab-case (lowercase letters, digits, hyphens), got %q", req.ID)
	}
	for _, reserved := range []string{"dark", "light", "oled"} {
		if req.ID == reserved {
			return fmt.Errorf("id %q conflicts with a built-in theme", req.ID)
		}
	}
	if strings.TrimSpace(req.NameZh) == "" || strings.TrimSpace(req.NameEn) == "" {
		return fmt.Errorf("name_zh and name_en are required")
	}
	if len(req.NameZh) > 100 || len(req.NameEn) > 100 {
		return fmt.Errorf("theme names must be at most 100 characters")
	}
	icon := req.Icon
	if icon == "" {
		icon = "sparkles"
	}
	if !themeIconAllowed[icon] {
		return fmt.Errorf("icon must be one of moon/sun/sparkles/zap/contrast, got %q", icon)
	}
	req.Icon = icon
	if req.Vars == nil {
		return fmt.Errorf("vars is required")
	}
	contract := make(map[string]bool, len(themeVarsContract))
	for _, v := range themeVarsContract {
		contract[v] = true
	}
	seen := make(map[string]bool, len(themeVarsContract))
	for k, v := range req.Vars {
		// 键必须属于契约集合:注入面在键维度同样封死(值会被拼入全局样式)
		if !contract[k] {
			return fmt.Errorf("vars contains unknown variable %q", k)
		}
		if k == "--shadow" {
			if !themeShadowPattern.MatchString(v) {
				return fmt.Errorf("vars[%s] must be an rgba() color, got %q", k, v)
			}
		} else if !themeHexPattern.MatchString(v) {
			return fmt.Errorf("vars[%s] must be a 6-digit hex color like #09090b, got %q", k, v)
		}
		seen[k] = true
	}
	var missing []string
	for _, want := range themeVarsContract {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("vars is missing required variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func toCustomThemeResponse(t types.CustomTheme) customThemeResponse {
	vars := make(map[string]string, len(themeVarsContract))
	_ = json.Unmarshal(t.Vars, &vars)
	created := ""
	if !t.CreatedAt.IsZero() {
		created = t.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return customThemeResponse{
		ID:        t.ID,
		NameZh:    t.NameZh,
		NameEn:    t.NameEn,
		Icon:      t.Icon,
		Vars:      vars,
		CreatedBy: t.CreatedBy,
		CreatedAt: created,
	}
}

// ListCustomThemesHandler handles GET /themes/custom — every authenticated principal
// can read the shared theme list.
func ListCustomThemesHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		themes, err := service.NewThemeService(svc).List(r.Context())
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		out := make([]customThemeResponse, 0, len(themes))
		for _, t := range themes {
			out = append(out, toCustomThemeResponse(t))
		}
		response.JSON(w, r, map[string]interface{}{"themes": out})
	}
}

// CreateCustomThemeHandler handles POST /themes/custom — human member or higher only.
func CreateCustomThemeHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}
		var req createCustomThemeRequest
		if err := render.Decode(r, &req); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
		if err := validateThemePayload(&req); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
		varsJSON, err := json.Marshal(req.Vars)
		if err != nil {
			response.BadRequest(w, "vars is not serializable")
			return
		}
		createdBy := claims.UserID.String()
		created, err := service.NewThemeService(svc).Create(r.Context(), types.CreateCustomThemeParams{
			ID:        req.ID,
			NameZh:    strings.TrimSpace(req.NameZh),
			NameEn:    strings.TrimSpace(req.NameEn),
			Icon:      req.Icon,
			Vars:      varsJSON,
			CreatedBy: &createdBy,
		})
		if err != nil {
			if isUniqueViolation(err) {
				response.Conflict(w, "a theme with this id already exists")
				return
			}
			response.InternalServerError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		response.JSON(w, r, toCustomThemeResponse(created))
	}
}

// DeleteCustomThemeHandler handles DELETE /themes/custom/{themeId} — human member or higher.
func DeleteCustomThemeHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := svcmw.GetAuthFromContext(r.Context()); !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}
		id := chi.URLParam(r, "themeId")
		if !themeIDPattern.MatchString(id) {
			response.BadRequest(w, "invalid theme id")
			return
		}
		deleted, err := service.NewThemeService(svc).Delete(r.Context(), id)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		if !deleted {
			response.NotFound(w, "theme not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
