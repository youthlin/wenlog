package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gsessions "github.com/gorilla/sessions"
	"github.com/youthlin/wenlog/internal/consts"
	"github.com/youthlin/wenlog/internal/store"
	"github.com/youthlin/wenlog/internal/util"
)

func Session(st *store.Store) func() gin.HandlerFunc {
	sessionStore, err := NewDynamicCookieStore(st)
	if err != nil {
		slog.Error("init session store", slog.Any("error", err))
		os.Exit(1)
	}
	sessionStore.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		MaxAge:   consts.SessionMaxAge,
		SameSite: http.SameSiteLaxMode,
	})
	return func() gin.HandlerFunc {
		return sessions.Sessions("wenlog_session", sessionStore)
	}
}

// DynamicCookieStore 是按当前设置项动态读取 session secret 的 cookie store。
// 修改设置中的 session_secret 后,后续请求会立即使用新密钥验签/签名。
type DynamicCookieStore struct {
	st             *store.Store
	sessionOption  sessions.Options
	fallbackSecret string
}

// NewDynamicCookieStore 创建动态 session store。
func NewDynamicCookieStore(st *store.Store) (*DynamicCookieStore, error) {
	var (
		fallbackSecret = util.GenerateRandomString(32)
		s              = &DynamicCookieStore{st: st, fallbackSecret: fallbackSecret}
	)
	if _, err := s.currentSecret(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DynamicCookieStore) currentSecret() (string, error) {
	// 先查db设置
	value, err := s.st.GetSetting(context.Background(), consts.SettingsSessionSecret)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value != "" {
		// db里有 直接返回
		return value, nil
	}

	// db里没有 生成一个并保存
	value = util.GenerateRandomString(32)
	if err = s.st.SetSetting(context.Background(), consts.SettingsSessionSecret, value); err == nil {
		s.fallbackSecret = value
		return value, nil
	}

	// 保存失败 使用兜底的
	value = strings.TrimSpace(s.fallbackSecret)
	return value, nil
}

func (s *DynamicCookieStore) currentStore(r *http.Request) (*gsessions.CookieStore, error) {
	value, err := s.currentSecret()
	if err != nil {
		return nil, err
	}
	// 每次请求复制一份 options,避免并发修改共享的 sessionOption。
	opts := s.sessionOption
	if RequestScheme(r) == "https" {
		opts.Secure = true
	}
	cs := gsessions.NewCookieStore([]byte(value))
	cs.Options = opts.ToGorillaOptions()
	return cs, nil
}

func (s *DynamicCookieStore) Get(r *http.Request, name string) (*gsessions.Session, error) {
	cs, err := s.currentStore(r)
	if err != nil {
		return nil, err
	}
	return cs.Get(r, name)
}

func (s *DynamicCookieStore) New(r *http.Request, name string) (*gsessions.Session, error) {
	cs, err := s.currentStore(r)
	if err != nil {
		return nil, err
	}
	return cs.New(r, name)
}

func (s *DynamicCookieStore) Save(r *http.Request, w http.ResponseWriter, session *gsessions.Session) error {
	cs, err := s.currentStore(r)
	if err != nil {
		return err
	}
	return cs.Save(r, w, session)
}

func (s *DynamicCookieStore) Options(options sessions.Options) {
	s.sessionOption = options
}
