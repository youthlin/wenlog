package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/middleware"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/store"
)

func requestBaseURL(c *gin.Context) string {
	scheme := middleware.RequestScheme(c.Request)
	host := middleware.RequestHost(c.Request)
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func positiveIntSetting(raw string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return def
	}
	return n
}

func syncPostPermalink(ctx context.Context, st *store.Store) string {
	pattern := consts.SettingsPostPermalinkDefault
	categoryPrefix := consts.SettingsCategoryPrefixDefault
	tagPrefix := consts.SettingsTagPrefixDefault
	if st != nil {
		if v, err := st.GetSetting(ctx, consts.SettingsPostPermalink); err == nil && strings.TrimSpace(v) != "" {
			pattern = v
		}
		if v, err := st.GetSetting(ctx, consts.SettingsCategoryPrefix); err == nil && strings.TrimSpace(v) != "" {
			categoryPrefix = v
		}
		if v, err := st.GetSetting(ctx, consts.SettingsTagPrefix); err == nil && strings.TrimSpace(v) != "" {
			tagPrefix = v
		}
	}
	permalink.SetPostPattern(pattern)
	permalink.SetTaxonomyPrefixes(categoryPrefix, tagPrefix)
	return permalink.CurrentPostPattern()
}

// currentUserID 从 session 读取当前登录用户 ID(未登录为 0)。
func currentUserID(c *gin.Context) uint {
	s := sessions.Default(c)
	if v := s.Get(middleware.SessionUserKey); v != nil {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

// currentUserByStore 返回当前登录用户(未登录为 nil)。
func currentUserByStore(ctx context.Context, st *store.Store, c *gin.Context) *model.User {
	uid := currentUserID(c)
	if uid == 0 {
		return nil
	}
	u, err := st.GetUserByID(ctx, uid)
	if err != nil {
		return nil
	}
	return u
}
