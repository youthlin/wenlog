package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	gsessions "github.com/gorilla/sessions"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/store"
	"github.com/youthlin/blog/internal/util"
)

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
	value, err := s.st.GetSetting(consts.SettingsSessionSecret)
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
	if err = s.st.SetSetting(consts.SettingsSessionSecret, value); err == nil {
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
	if requestScheme(r) == "https" {
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
