package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

const authSessionCookieName = "rpre_session"

type authContextKey string

const authUserContextKey authContextKey = "auth_user"

type AuthUser struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	WorkEmail   string    `json:"work_email"`
}

type AuthStore interface {
	CreateUser(ctx context.Context, user events.UserAccount) (events.UserAccount, error)
	GetUserByEmail(ctx context.Context, workEmail string) (events.UserAccount, bool, error)
	GetUserByID(ctx context.Context, userID uuid.UUID) (events.UserAccount, bool, error)
	CreateSession(ctx context.Context, session events.UserSession) (events.UserSession, error)
	GetSessionByID(ctx context.Context, sessionID uuid.UUID) (events.UserSession, bool, error)
	RevokeSession(ctx context.Context, sessionID uuid.UUID) error
}

type AuthConfig struct {
	CookieSecure bool
	SessionTTL   time.Duration
}

type registerRequest struct {
	DisplayName string `json:"display_name" binding:"required"`
	WorkEmail   string `json:"work_email" binding:"required"`
	Password    string `json:"password" binding:"required"`
}

type loginRequest struct {
	WorkEmail string `json:"work_email" binding:"required"`
	Password  string `json:"password" binding:"required"`
}

func normalizeEmail(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func setSessionCookie(c *gin.Context, sessionID uuid.UUID, cfg AuthConfig) {
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = 14 * 24 * time.Hour
	}
	c.SetCookie(
		authSessionCookieName,
		sessionID.String(),
		int(ttl.Seconds()),
		"/",
		"",
		cfg.CookieSecure,
		true,
	)
}

func clearSessionCookie(c *gin.Context, cfg AuthConfig) {
	c.SetCookie(
		authSessionCookieName,
		"",
		-1,
		"/",
		"",
		cfg.CookieSecure,
		true,
	)
}

func requireAuth(store AuthStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := c.Cookie(authSessionCookieName)
		if err != nil || strings.TrimSpace(raw) == "" {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", nil)
			c.Abort()
			return
		}
		sessionID, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid session", nil)
			c.Abort()
			return
		}
		sess, ok, err := store.GetSessionByID(c.Request.Context(), sessionID)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			c.Abort()
			return
		}
		if !ok || sess.RevokedAt != nil || time.Now().UTC().After(sess.ExpiresAt) {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "session expired", nil)
			c.Abort()
			return
		}
		user, ok, err := store.GetUserByID(c.Request.Context(), sess.UserID)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			c.Abort()
			return
		}
		if !ok {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid session user", nil)
			c.Abort()
			return
		}
		c.Set(string(authUserContextKey), AuthUser{
			UserID:      user.UserID,
			DisplayName: user.DisplayName,
			WorkEmail:   user.WorkEmail,
		})
		c.Next()
	}
}

func authUserFromContext(c *gin.Context) (AuthUser, bool) {
	v, ok := c.Get(string(authUserContextKey))
	if !ok {
		return AuthUser{}, false
	}
	u, ok := v.(AuthUser)
	return u, ok
}

func registerHandler(store AuthStore, cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}
		displayName := strings.TrimSpace(req.DisplayName)
		email := normalizeEmail(req.WorkEmail)
		password := strings.TrimSpace(req.Password)
		if displayName == "" || email == "" || len(password) < 8 {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "display_name, work_email, and password (min 8 chars) are required", nil)
			return
		}
		if _, found, err := store.GetUserByEmail(c.Request.Context(), email); err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		} else if found {
			respondAPIError(c, http.StatusConflict, ErrCodeIdempotencyConflict, "account already exists", nil)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		user, err := store.CreateUser(c.Request.Context(), events.UserAccount{
			UserID:       uuid.New(),
			DisplayName:  displayName,
			WorkEmail:    email,
			PasswordHash: string(hash),
		})
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		c.JSON(http.StatusCreated, AuthUser{
			UserID:      user.UserID,
			DisplayName: user.DisplayName,
			WorkEmail:   user.WorkEmail,
		})
	}
}

func loginHandler(store AuthStore, cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondAPIError(c, http.StatusBadRequest, ErrCodeValidation, "invalid request body including JSON shape", nil)
			return
		}
		email := normalizeEmail(req.WorkEmail)
		user, found, err := store.GetUserByEmail(c.Request.Context(), email)
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		if !found || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid credentials", nil)
			return
		}
		ttl := cfg.SessionTTL
		if ttl <= 0 {
			ttl = 14 * 24 * time.Hour
		}
		sess, err := store.CreateSession(c.Request.Context(), events.UserSession{
			SessionID: uuid.New(),
			UserID:    user.UserID,
			ExpiresAt: time.Now().UTC().Add(ttl),
		})
		if err != nil {
			respondAPIError(c, http.StatusInternalServerError, ErrCodeInternal, "internal error", nil)
			return
		}
		setSessionCookie(c, sess.SessionID, cfg)
		c.JSON(http.StatusOK, AuthUser{
			UserID:      user.UserID,
			DisplayName: user.DisplayName,
			WorkEmail:   user.WorkEmail,
		})
	}
}

func logoutHandler(store AuthStore, cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := c.Cookie(authSessionCookieName)
		if err == nil && strings.TrimSpace(raw) != "" {
			if sid, err := uuid.Parse(strings.TrimSpace(raw)); err == nil {
				_ = store.RevokeSession(c.Request.Context(), sid)
			}
		}
		clearSessionCookie(c, cfg)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func meHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := authUserFromContext(c)
		if !ok {
			respondAPIError(c, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", nil)
			return
		}
		c.JSON(http.StatusOK, user)
	}
}
