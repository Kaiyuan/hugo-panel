package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$`)

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Language string `json:"language"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not check setup state")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "setup_complete", "Setup has already been completed")
		return
	}
	var req setupRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	username := strings.TrimSpace(req.Username)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !usernameRe.MatchString(username) {
		writeError(w, http.StatusBadRequest, "invalid_username", "Use 3-64 letters, numbers, dots, dashes or underscores")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if email != "" && !validEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email", "Enter a valid email address")
		return
	}
	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = "en"
	}
	if !s.languageExists(language) {
		language = "en"
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not secure the password")
		return
	}
	user, err := s.store.CreateAdmin(ctx, username, hash, email, language)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrSetupDone):
			writeError(w, http.StatusConflict, "setup_complete", "Setup has already been completed")
		case errors.Is(err, store.ErrConflict):
			writeError(w, http.StatusConflict, "user_exists", "That username is already taken")
		default:
			s.logger.Error("create admin", "error", err)
			writeError(w, http.StatusInternalServerError, "server_error", "Could not create the administrator")
		}
		return
	}
	_ = s.settings.Update(ctx, map[string]string{
		settings.KeyDefaultLanguage:  language,
		settings.KeySetupCompletedAt: time.Now().UTC().Format(time.RFC3339),
	})
	token, csrf, expires, err := s.store.CreateSession(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Account created, but sign in failed. Please log in.")
		return
	}
	setSessionCookie(w, r, s.cfg.CookieSecure, token, expires)
	s.logger.Info("administrator created", "username", user.Username)
	s.writeStateForSession(w, r, store.Session{User: user, CSRF: csrf})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.Allow("login:"+ip, 12, 15*time.Minute) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts. Try again later.")
		return
	}
	var req loginRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Username and password are required")
		return
	}
	ctx := r.Context()
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		_ = verifyPassword(s.dummyHash, req.Password)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Incorrect username or password")
		return
	}
	if !verifyPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Incorrect username or password")
		return
	}
	token, csrf, expires, err := s.store.CreateSession(ctx, user.ID)
	if err != nil {
		s.logger.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "server_error", "Could not start a session")
		return
	}
	setSessionCookie(w, r, s.cfg.CookieSecure, token, expires)
	s.writeStateForSession(w, r, store.Session{User: user, CSRF: csrf})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, sess store.Session) {
	if token, err := s.sessionToken(r); err == nil {
		_ = s.store.DeleteSession(r.Context(), token)
	}
	clearSessionCookie(w, r, s.cfg.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) writeFreshState(w http.ResponseWriter, r *http.Request) {
	state, err := s.buildState(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load application state")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) writeStateForSession(w http.ResponseWriter, r *http.Request, sess store.Session) {
	state, err := s.buildStateWithSession(r, &sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load application state")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

type forgotRequest struct {
	Login string `json:"login"`
}

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.Allow("forgot:"+ip, 6, time.Hour) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many reset requests. Try again later.")
		return
	}
	var req forgotRequest
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	login := strings.TrimSpace(req.Login)
	if login == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Enter your email address or username")
		return
	}
	ctx := r.Context()
	smtpCfg, err := s.settings.EffectiveSMTP(ctx)
	if err != nil || !smtpCfg.Configured() {
		s.logger.Warn("password reset requested but SMTP is not configured")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	user, err := s.store.GetUserByEmailOrUsername(ctx, login)
	if err == nil && user.Email != "" {
		token, terr := s.store.CreatePasswordReset(ctx, user.ID)
		if terr == nil {
			base := s.externalBaseURL(r)
			link := base + "/#/reset?token=" + url.QueryEscape(token)
			msg := mailer.PasswordResetEmail(user.Username, link, store.ResetLifetime)
			msg.To = user.Email
			go func(to string, msg mailer.Message) {
				sendCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := s.mailer.Send(sendCtx, smtpCfg, msg); err != nil {
					s.logger.Error("send password reset email", "error", err, "to", to)
				}
			}(user.Email, msg)
		} else {
			s.logger.Error("create password reset", "error", terr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "reset_invalid", "This reset link is invalid or has expired")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	ctx := r.Context()
	userID, err := s.store.ConsumePasswordReset(ctx, req.Token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reset_invalid", "This reset link is invalid or has expired")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not secure the password")
		return
	}
	if err := s.store.UpdatePassword(ctx, userID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not update the password")
		return
	}
	_ = s.store.DeleteAllSessions(ctx, userID)
	s.logger.Info("password reset completed", "user_id", userID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, sess store.Session) {
	writeJSON(w, http.StatusOK, map[string]any{"user": sess.User})
}

type updateMeRequest struct {
	Email    *string `json:"email"`
	Language *string `json:"language"`
	Theme    *string `json:"theme"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req updateMeRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	email := sess.User.Email
	language := sess.User.Language
	theme := sess.User.Theme
	if req.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*req.Email))
		if email != "" && !validEmail(email) {
			writeError(w, http.StatusBadRequest, "invalid_email", "Enter a valid email address")
			return
		}
	}
	if req.Language != nil {
		language = strings.TrimSpace(*req.Language)
		if !s.languageExists(language) {
			writeError(w, http.StatusBadRequest, "invalid_locale", "Unsupported language")
			return
		}
	}
	if req.Theme != nil {
		theme = strings.TrimSpace(*req.Theme)
		if !validTheme(theme) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Theme must be light, dark or system")
			return
		}
	}
	user, err := s.store.UpdateUserProfile(r.Context(), sess.User.ID, email, language, theme)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not update profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req changePasswordRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	if !verifyPassword(sess.User.PasswordHash, req.CurrentPassword) {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "Current password is incorrect")
		return
	}
	if err := validatePassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not secure the password")
		return
	}
	if err := s.store.UpdatePassword(r.Context(), sess.User.ID, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not update the password")
		return
	}
	if token, err := s.sessionToken(r); err == nil {
		_ = s.store.DeleteOtherSessions(r.Context(), sess.User.ID, token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) languageExists(code string) bool {
	languages, err := s.i18n.Languages()
	if err != nil {
		return code == "en"
	}
	for _, l := range languages {
		if strings.EqualFold(l.Code, code) {
			return true
		}
	}
	return false
}

func validTheme(theme string) bool {
	return theme == "light" || theme == "dark" || theme == "system"
}

func hashPassword(password string) (string, error) {
	sum := sha256.Sum256([]byte(password))
	hash, err := bcrypt.GenerateFromPassword(sum[:], 12)
	return string(hash), err
}

func verifyPassword(hash, password string) bool {
	sum := sha256.Sum256([]byte(password))
	return bcrypt.CompareHashAndPassword([]byte(hash), sum[:]) == nil
}

func validatePassword(password string) error {
	n := len([]rune(password))
	if n < 8 {
		return errors.New("Password must be at least 8 characters")
	}
	if n > 200 {
		return errors.New("Password must be at most 200 characters")
	}
	return nil
}

func validEmail(email string) bool {
	if email == "" {
		return true
	}
	if strings.ContainsAny(email, "\r\n<>") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(addr.Address, email) && addr.Name == ""
}

func (s *Server) externalBaseURL(r *http.Request) string {
	if pub := s.settings.PublicURL(r.Context()); pub != "" {
		return pub
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = strings.TrimSpace(strings.Split(xfh, ",")[0])
	}
	return scheme + "://" + host
}
