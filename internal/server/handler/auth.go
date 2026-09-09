// auth.go provides HTTP API endpoints related to user authentication, including login, registration, token exchange, logout, password change/reset, workspace switching, etc.
//
// Public endpoints (no authentication required): login, registration, token exchange, password reset request, password reset, accept invitation.
// Authenticated endpoints: logout, current user info, change password, switch workspace.

package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// AuthHandler handles HTTP requests related to user authentication, including login, registration, token exchange, logout, password management, etc.
type AuthHandler struct {
	Svc       *service.Service
	JWTSecret string // JWT signing secret
}

// NewAuthHandler creates an AuthHandler instance.
func NewAuthHandler(svc *service.Service, jwtSecret string) *AuthHandler {
	return &AuthHandler{Svc: svc, JWTSecret: jwtSecret}
}

// Routes returns authentication routes that do not require authentication (login, registration, token exchange).
func (h *AuthHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/login", h.Login)
	r.Post("/register", h.Register)
	r.Post("/token-exchange", h.TokenExchange)

	return r
}

// loginRequest login request body.
type loginRequest struct {
	Email    string `json:"email"`    // user email
	Password string `json:"password"` // user password
}

// Login handles the POST /login endpoint, validates the user email and password, and returns a JWT Token and user info.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - email: string, user email (required)
//   - password: string, user password (required)
//
// Response:
//   - 200: login successful, returns Token and user info
//   - 400: parameter error (missing email or password)
//   - 401: incorrect email or password
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	// parse request body
	var req loginRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// validate required fields
	if req.Email == "" || req.Password == "" {
		response.BadRequest(w, "email and password are required")
		return
	}

	// call auth service to perform login
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	result, err := authSvc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		response.Unauthorized(w, "invalid email or password")
		return
	}

	// return authentication result
	response.JSON(w, r, authResponse{
		Token:       result.Token,
		ExpiresAt:   result.ExpiresAt,
		Member:      result.Member,
		WorkspaceID: result.WorkspaceID.String(),
		Role:        result.Role,
	})
}

// registerRequest registration request body.
type registerRequest struct {
	Name     string `json:"name"`     // user name
	Email    string `json:"email"`    // user email
	Password string `json:"password"` // user password
}

// Register handles the POST /register endpoint, registers a new user and creates a default workspace, returning a JWT Token.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, user name (required)
//   - email: string, user email (required)
//   - password: string, user password (required, 8-128 chars, must include upper/lowercase letters and digits)
//
// Response:
//   - 201: registration successful, returns Token and user info
//   - 400: parameter error or password does not meet strength requirements
//   - 500: internal server error
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	// parse request body
	var req registerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// validate required fields
	if req.Name == "" || req.Email == "" || req.Password == "" {
		response.BadRequest(w, "name, email and password are required")
		return
	}

	// validate password strength (consistent with the service layer, defense in depth)
	if err := service.ValidatePassword(req.Password); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call auth service to perform registration
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	result, err := authSvc.Register(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		if isUniqueViolation(err) {
			response.Conflict(w, "email already registered")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// return registration result
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, authResponse{
		Token:       result.Token,
		ExpiresAt:   result.ExpiresAt,
		Member:      result.Member,
		WorkspaceID: result.WorkspaceID.String(),
		Role:        result.Role,
	})
}

// --- Token exchange ---

// tokenExchangeRequest token exchange request body.
type tokenExchangeRequest struct {
	APIToken string `json:"api_token"` // API Token (tm_ prefix)
}

// tokenExchangeResponse token exchange response body.
type tokenExchangeResponse struct {
	SessionToken string    `json:"session_token"` // session Token (st_ prefix)
	ExpiresAt    time.Time `json:"expires_at"`    // expiration time
}

// TokenExchange handles the POST /token-exchange endpoint, exchanging an API Token for a session Token.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - api_token: string, API Token (required, tm_ prefix)
//
// Response:
//   - 200: exchange successful, returns session Token
//   - 400: parameter error or invalid Token format
//   - 401: Token invalid or expired
func (h *AuthHandler) TokenExchange(w http.ResponseWriter, r *http.Request) {
	// parse request body
	var req tokenExchangeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// validate required fields
	if req.APIToken == "" {
		response.BadRequest(w, "api_token is required")
		return
	}

	// validate Token format
	if !strings.HasPrefix(req.APIToken, "tm_") {
		response.BadRequest(w, "invalid api_token format")
		return
	}

	// call auth service to perform token exchange
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	result, err := authSvc.ExchangeAPITokenForSession(r.Context(), req.APIToken)
	if err != nil {
		response.Unauthorized(w, "invalid or expired api token")
		return
	}

	// return session Token
	response.JSON(w, r, tokenExchangeResponse{
		SessionToken: result.SessionToken,
		ExpiresAt:    result.ExpiresAt,
	})
}

// --- Whoami ---

// Whoami handles the GET /whoami endpoint, returning the current authenticated user's info.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns user info
//   - 401: not authenticated
func (h *AuthHandler) Whoami(w http.ResponseWriter, r *http.Request) {
	// get auth info from context
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "not authenticated")
		return
	}

	// call auth service to get user info
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	info, err := authSvc.Whoami(r.Context(), claims.UserType, claims.UserID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, info)
}

// generateToken internal method that generates a JWT Token.
func (h *AuthHandler) generateToken(userID uuid.UUID, userType string, workspaceID uuid.UUID, role string) (string, time.Time, error) {
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	return authSvc.GenerateToken(userID, userType, workspaceID, role)
}

// --- Switch workspace ---

// switchWorkspaceRequest switch workspace request body.
type switchWorkspaceRequest struct {
	WorkspaceID string `json:"workspace_id"` // target workspace ID
}

// switchWorkspaceResponse switch workspace response body.
type switchWorkspaceResponse struct {
	WorkspaceID string `json:"workspace_id"` // target workspace ID
	Role        string `json:"role"`         // user's role in the target workspace
}

// SwitchWorkspace handles the POST /switch-workspace endpoint, generating a new JWT Token for the target workspace; only human users are allowed to switch.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - workspace_id: string, target workspace ID (required)
//
// Response:
//   - 200: switch successful, returns new Token and role info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: non-human user cannot switch or is not a member of the target workspace
func (h *AuthHandler) SwitchWorkspace(w http.ResponseWriter, r *http.Request) {
	// get auth info from context
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// only human users can switch workspace
	if claims.UserType != "member" {
		response.Forbidden(w, "only members can switch workspace")
		return
	}

	// parse request body
	var req switchWorkspaceRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// validate required fields
	if req.WorkspaceID == "" {
		response.BadRequest(w, "workspace_id is required")
		return
	}

	// parse target workspace ID
	targetWorkspaceID, err := uuid.Parse(req.WorkspaceID)
	if err != nil {
		response.BadRequest(w, "invalid workspace_id format")
		return
	}

	// verify the user is a member of the target workspace
	wsSvc := service.NewWorkspaceService(h.Svc)
	wm, err := wsSvc.GetMembership(r.Context(), targetWorkspaceID, claims.UserID)
	if err != nil {
		response.Forbidden(w, "you are not a member of the target workspace")
		return
	}

	response.JSON(w, r, switchWorkspaceResponse{
		WorkspaceID: targetWorkspaceID.String(),
		Role:        wm.Role,
	})
}

// isUniqueViolation determines whether the error chain contains a PostgreSQL unique constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	// fallback: the error string contains SQLSTATE 23505 (covers cases where the driver error type is wrapped/changed)
	return strings.Contains(err.Error(), "23505")
}
