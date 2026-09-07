package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
)

type AuthHandler struct {
	authSvc        *service.AuthService
	teams          *service.TeamService
	autoLoginToken string // JWT for default user (empty = disabled)
	log            *slog.Logger
}

func NewAuthHandler(authSvc *service.AuthService, teams *service.TeamService, autoLoginToken string, log *slog.Logger) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, teams: teams, autoLoginToken: autoLoginToken, log: log}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		// InviteToken lets someone who was handed a link create an account on a
		// server with registration closed. The invitation is the authorization;
		// without this, turning registration off would make it impossible to
		// bring anyone new onto a team, which is what invitations are for.
		InviteToken string `json:"invite_token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	invited := false
	if input.InviteToken != "" && h.teams != nil {
		preview, err := h.teams.PreviewInvite(r.Context(), input.InviteToken)
		if err == nil && preview.Status == service.InvitePending {
			invited = true
		}
	}

	var (
		user  *domain.User
		token string
		err   error
	)
	if invited {
		user, token, err = h.authSvc.RegisterInvited(r.Context(), input.Email, input.Password, input.Name)
	} else {
		user, token, err = h.authSvc.Register(r.Context(), input.Email, input.Password, input.Name)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrRegistrationClosed):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrPasswordTooShort),
			errors.Is(err, service.ErrInvalidEmail):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			h.log.Error("register failed", "error", err)
			writeError(w, http.StatusInternalServerError, "registration failed")
		}
		return
	}

	// Join straight away: the point of following the link was to get in, and
	// making the new account press Join again on a page it has already read is
	// a step for nobody's benefit.
	joined := map[string]any(nil)
	if invited {
		ctx := auth.WithUser(r.Context(), user)
		if result, aerr := h.teams.AcceptInvite(ctx, input.InviteToken); aerr == nil {
			joined = map[string]any{"team_id": result.TeamID, "team_name": result.TeamName, "role": result.Role}
		} else {
			h.log.Warn("registered from an invite but could not join", "error", aerr)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":   user,
		"token":  token,
		"joined": joined,
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	user, token, err := h.authSvc.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
		} else {
			h.log.Error("login failed", "error", err)
			writeError(w, http.StatusInternalServerError, "login failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":  user,
		"token": token,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	plain, key, err := h.authSvc.CreateAPIKey(r.Context(), user.ID, input.Name)
	if err != nil {
		h.log.Error("create api key failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create API key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"key":        plain,
		"id":         key.ID,
		"name":       key.Name,
		"prefix":     key.KeyPrefix,
		"created_at": key.CreatedAt,
	})
}

func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keys, err := h.authSvc.ListAPIKeys(r.Context(), user.ID)
	if err != nil {
		h.log.Error("list api keys failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list API keys")
		return
	}

	if keys == nil {
		keys = []*domain.APIKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *AuthHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keyID := r.PathValue("id")
	if err := h.authSvc.DeleteAPIKey(r.Context(), keyID, user.ID); err != nil {
		writeError(w, http.StatusNotFound, "API key not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *AuthHandler) AuthInfo(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"auth_enabled":       true,
		"allow_registration": h.authSvc.AllowRegistration(),
	}
	// The auto-login token is a 30-day JWT for the `default_user`, and this is a
	// public route. Handing it to anyone who asks made an instance configured
	// for local convenience give a stranger that account — reads, writes, and
	// `/api/auth/api-keys` to mint a credential outliving the JWT — for a month,
	// revocable only by rotating `jwt_secret`.
	//
	// It exists so a browser on the operator's own machine logs in without a
	// password, so that is the only caller it is served to. The predicate is the
	// one the write gate uses; a second reading of "is this local" is how the
	// two drift apart.
	if h.autoLoginToken != "" && auth.LocalRequest(r) {
		resp["auto_login_token"] = h.autoLoginToken
	}
	writeJSON(w, http.StatusOK, resp)
}
