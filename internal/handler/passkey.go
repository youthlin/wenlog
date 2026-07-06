package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"

	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/middleware"
	"github.com/youthlin/wenlog/internal/model"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
)

const (
	passkeyRegisterSessionKey = "passkey_register_session"
	passkeyLoginSessionKey    = "passkey_login_session"
	passkeyLoginUserIDKey     = "passkey_login_user_id"
)

type passkeyUser struct {
	user        *model.User
	credentials []webauthnlib.Credential
}

func (u passkeyUser) WebAuthnID() []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(u.user.ID))
	return b[:]
}

func (u passkeyUser) WebAuthnName() string {
	return u.user.Username
}

func (u passkeyUser) WebAuthnDisplayName() string {
	if strings.TrimSpace(u.user.DisplayName) != "" {
		return u.user.DisplayName
	}
	return u.user.Username
}

func (u passkeyUser) WebAuthnCredentials() []webauthnlib.Credential {
	return u.credentials
}

func passkeyUserHandle(userID uint) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(userID))
	return b[:]
}

func passkeyUserIDFromHandle(handle []byte) uint {
	if len(handle) != 8 {
		return 0
	}
	return uint(binary.BigEndian.Uint64(handle))
}

func (h *Admin) passkeyUser(c *gin.Context, u *model.User) (passkeyUser, error) {
	passkeys, err := h.st.ListPasskeysByUserID(c, u.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	credentials, err := credentialsFromPasskeys(passkeys)
	if err != nil {
		return passkeyUser{}, err
	}
	return passkeyUser{user: u, credentials: credentials}, nil
}

func (h *Auth) passkeyUser(c *gin.Context, u *model.User) (passkeyUser, error) {
	passkeys, err := h.st.ListPasskeysByUserID(c, u.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	credentials, err := credentialsFromPasskeys(passkeys)
	if err != nil {
		return passkeyUser{}, err
	}
	return passkeyUser{user: u, credentials: credentials}, nil
}

func credentialsFromPasskeys(passkeys []model.PasskeyCredential) ([]webauthnlib.Credential, error) {
	credentials := make([]webauthnlib.Credential, 0, len(passkeys))
	for _, passkey := range passkeys {
		var credential webauthnlib.Credential
		if err := json.Unmarshal([]byte(passkey.CredentialJSON), &credential); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

func webAuthnForRequest(st *store.Store, c *gin.Context) (*webauthnlib.WebAuthn, error) {
	siteName := siteNameFromStore(c, st)
	origin := passkeyOrigin(c)
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	rpID := u.Hostname()
	return webauthnlib.New(&webauthnlib.Config{
		RPID:          rpID,
		RPDisplayName: siteName,
		RPOrigins:     []string{origin},
	})
}

func passkeyOrigin(c *gin.Context) string {
	scheme := middleware.RequestScheme(c.Request)
	host := middleware.RequestHost(c.Request)
	return scheme + "://" + host
}

func writeJSON(c *gin.Context, status int, v any) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(status, v)
}

func passkeyError(c *gin.Context, status int, message string) {
	writeJSON(c, status, gin.H{"ok": false, "message": message})
}

func saveWebAuthnSession(c *gin.Context, key string, session *webauthnlib.SessionData) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	s := sessions.Default(c)
	s.Set(key, string(data))
	return s.Save()
}

func loadWebAuthnSession(c *gin.Context, key string) (webauthnlib.SessionData, error) {
	s := sessions.Default(c)
	var session webauthnlib.SessionData
	raw, _ := s.Get(key).(string)
	if strings.TrimSpace(raw) == "" {
		return session, gorm.ErrRecordNotFound
	}
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return session, err
	}
	s.Delete(key)
	_ = s.Save()
	return session, nil
}

func jsonRequestWithBody(c *gin.Context) *http.Request {
	c.Request.Header.Set("Content-Type", "application/json")
	return c.Request
}

// BeginPasskeyRegistration 开始为当前用户注册 Passkey。
func (h *Admin) BeginPasskeyRegistration(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		passkeyError(c, http.StatusUnauthorized, "未登录。")
		return
	}
	w, err := webAuthnForRequest(h.st, c)
	if err != nil {
		passkeyError(c, http.StatusInternalServerError, "Passkey 初始化失败。")
		return
	}
	pu, err := h.passkeyUser(c, u)
	if err != nil {
		h.serverError(c, err)
		return
	}
	creation, session, err := w.BeginRegistration(pu,
		webauthnlib.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthnlib.WithExclusions(webauthnlib.Credentials(pu.WebAuthnCredentials()).CredentialDescriptors()),
		webauthnlib.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, RequireResidentKey: protocol.ResidentKeyRequired(), UserVerification: protocol.VerificationRequired}),
		webauthnlib.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		passkeyError(c, http.StatusBadRequest, "无法开始 Passkey 注册。")
		return
	}
	if err := saveWebAuthnSession(c, passkeyRegisterSessionKey, session); err != nil {
		h.serverError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "options": creation})
}

// FinishPasskeyRegistration 完成当前用户 Passkey 注册。
func (h *Admin) FinishPasskeyRegistration(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		passkeyError(c, http.StatusUnauthorized, "未登录。")
		return
	}
	w, err := webAuthnForRequest(h.st, c)
	if err != nil {
		passkeyError(c, http.StatusInternalServerError, "Passkey 初始化失败。")
		return
	}
	pu, err := h.passkeyUser(c, u)
	if err != nil {
		h.serverError(c, err)
		return
	}
	session, err := loadWebAuthnSession(c, passkeyRegisterSessionKey)
	if err != nil {
		passkeyError(c, http.StatusBadRequest, "Passkey 注册会话已过期，请重试。")
		return
	}
	credential, err := w.FinishRegistration(pu, session, jsonRequestWithBody(c))
	if err != nil {
		passkeyError(c, http.StatusBadRequest, "Passkey 注册失败，请重试。")
		return
	}
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		name = "Passkey"
	}
	if err := h.st.CreatePasskey(c, u.ID, name, credential); err != nil {
		h.serverError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "message": "Passkey 已添加。"})
}

// DeletePasskey 删除当前用户的一个 Passkey。
func (h *Admin) DeletePasskey(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		h.notFound(c)
		return
	}
	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		h.notFound(c)
		return
	}
	if err := h.st.DeletePasskey(c, u.ID, uint(id64)); err != nil {
		h.serverError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, profileRedirectURL("passkey-deleted"))
}

// BeginPasskeyLogin 开始 Passkey 登录。
func (h *Auth) BeginPasskeyLogin(c *gin.Context) {
	w, err := webAuthnForRequest(h.st, c)
	if err != nil {
		passkeyError(c, http.StatusInternalServerError, "Passkey 初始化失败。")
		return
	}
	username := strings.TrimSpace(c.Query("username"))
	var assertion *protocol.CredentialAssertion
	var session *webauthnlib.SessionData
	var userID uint
	if username != "" {
		u, err := h.st.GetUserByUsername(c, username)
		if err != nil || u == nil {
			passkeyError(c, http.StatusBadRequest, "用户不存在或尚未添加 Passkey。")
			return
		}
		pu, err := h.passkeyUser(c, u)
		if err != nil {
			h.serverError(c, err)
			return
		}
		if len(pu.credentials) == 0 {
			passkeyError(c, http.StatusBadRequest, "用户不存在或尚未添加 Passkey。")
			return
		}
		assertion, session, err = w.BeginLogin(pu, webauthnlib.WithUserVerification(protocol.VerificationRequired))
		userID = u.ID
	} else {
		assertion, session, err = w.BeginDiscoverableLogin(webauthnlib.WithUserVerification(protocol.VerificationRequired))
	}
	if err != nil {
		passkeyError(c, http.StatusBadRequest, "无法开始 Passkey 登录。")
		return
	}
	if err := saveWebAuthnSession(c, passkeyLoginSessionKey, session); err != nil {
		h.serverError(c, err)
		return
	}
	s := sessions.Default(c)
	if userID > 0 {
		s.Set(passkeyLoginUserIDKey, int(userID))
	} else {
		s.Delete(passkeyLoginUserIDKey)
	}
	_ = s.Save()
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "options": assertion})
}

// FinishPasskeyLogin 完成 Passkey 登录。
func (h *Auth) FinishPasskeyLogin(c *gin.Context) {
	w, err := webAuthnForRequest(h.st, c)
	if err != nil {
		passkeyError(c, http.StatusInternalServerError, "Passkey 初始化失败。")
		return
	}
	session, err := loadWebAuthnSession(c, passkeyLoginSessionKey)
	if err != nil {
		passkeyError(c, http.StatusBadRequest, "Passkey 登录会话已过期，请重试。")
		return
	}
	s := sessions.Default(c)
	defer func() {
		s.Delete(passkeyLoginUserIDKey)
		_ = s.Save()
	}()
	var u *model.User
	var credential *webauthnlib.Credential
	if userID := sessionUserIDValue(s.Get(passkeyLoginUserIDKey)); userID > 0 {
		u, err = h.st.GetUserByID(c, userID)
		if err != nil {
			passkeyError(c, http.StatusUnauthorized, "Passkey 登录失败。")
			return
		}
		pu, err := h.passkeyUser(c, u)
		if err != nil {
			h.serverError(c, err)
			return
		}
		credential, err = w.FinishLogin(pu, session, jsonRequestWithBody(c))
	} else {
		var webUser webauthnlib.User
		webUser, credential, err = w.FinishPasskeyLogin(func(rawID, userHandle []byte) (webauthnlib.User, error) {
			uid := passkeyUserIDFromHandle(userHandle)
			if uid == 0 {
				owner, err := h.st.GetUserByPasskeyCredentialID(c, rawID)
				if err != nil {
					return nil, err
				}
				uid = owner.ID
			}
			owner, err := h.st.GetUserByID(c, uid)
			if err != nil {
				return nil, err
			}
			pu, err := h.passkeyUser(c, owner)
			if err != nil {
				return nil, err
			}
			return pu, nil
		}, session, jsonRequestWithBody(c))
		if pu, ok := webUser.(passkeyUser); ok {
			u = pu.user
		}
	}
	if err != nil || u == nil || credential == nil {
		passkeyError(c, http.StatusUnauthorized, "Passkey 登录失败。")
		return
	}
	_ = h.st.TouchPasskeyUsed(c, credential.ID)
	middleware.SetSessionUser(c, u.ID, u.Role, u.SessionVersion)
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "redirect": "/admin/"})
}

func sessionUserIDValue(v any) uint {
	switch n := v.(type) {
	case uint:
		return n
	case int:
		return uint(n)
	case int64:
		return uint(n)
	case float64:
		return uint(n)
	default:
		return 0
	}
}

func passkeyRandomName() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "Passkey"
	}
	return "Passkey-" + base64.RawURLEncoding.EncodeToString(b[:])
}

func (h *Admin) profilePasskeyData(c *gin.Context, data gin.H, u *model.User) {
	passkeys, err := h.st.ListPasskeysByUserID(c, u.ID)
	if err != nil {
		if h.log != nil {
			h.log.ErrorContext(c, "list passkeys for profile",
				slog.Any("error", err),
				slog.Uint64("user_id", uint64(u.ID)),
			)
		}
		return
	}
	data["Passkeys"] = passkeys
	data["PasskeyDefaultName"] = passkeyRandomName()
	settings, _ := h.st.GetSettings(c, consts.SettingsSiteName)
	data["PasskeyRPName"] = util.FirstNonEmptyOr(consts.SettingsSiteNameDefault, settings[consts.SettingsSiteName])
}
