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
		settings, err := st.GetSettings(ctx,
			consts.SettingsPostPermalink,
			consts.SettingsCategoryPrefix,
			consts.SettingsTagPrefix,
		)
		if err == nil {
			if v := settings[consts.SettingsPostPermalink]; strings.TrimSpace(v) != "" {
				pattern = v
			}
			if v := settings[consts.SettingsCategoryPrefix]; strings.TrimSpace(v) != "" {
				categoryPrefix = v
			}
			if v := settings[consts.SettingsTagPrefix]; strings.TrimSpace(v) != "" {
				tagPrefix = v
			}
		}
	}
	permalink.SetPostPattern(pattern)
	permalink.SetTaxonomyPrefixes(categoryPrefix, tagPrefix)
	return permalink.CurrentPostPattern()
}

// syncPostPermalinkFromLoader 从 DataLoader 内存中同步 permalink 设置，不查 DB。
func syncPostPermalinkFromLoader(loader *store.DataLoader) string {
	pattern := consts.SettingsPostPermalinkDefault
	categoryPrefix := consts.SettingsCategoryPrefixDefault
	tagPrefix := consts.SettingsTagPrefixDefault
	if loader != nil {
		if v := loader.GetSetting(consts.SettingsPostPermalink); strings.TrimSpace(v) != "" {
			pattern = v
		}
		if v := loader.GetSetting(consts.SettingsCategoryPrefix); strings.TrimSpace(v) != "" {
			categoryPrefix = v
		}
		if v := loader.GetSetting(consts.SettingsTagPrefix); strings.TrimSpace(v) != "" {
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

const ctxKeyCurrentUser = "currentUser"

// currentUserByStore 返回当前登录用户(未登录为 nil)。同一请求内缓存结果，避免重复查库。
func currentUserByStore(ctx context.Context, st *store.Store, c *gin.Context) *model.User {
	if cached, ok := c.Get(ctxKeyCurrentUser); ok {
		if u, ok := cached.(*model.User); ok {
			return u
		}
	}
	uid := currentUserID(c)
	if uid == 0 {
		return nil
	}
	u, err := st.GetUserByID(ctx, uid)
	if err != nil {
		return nil
	}
	c.Set(ctxKeyCurrentUser, u)
	return u
}

// currentUserFromLoader 从 DataLoader 内存中获取当前登录用户，避免查 DB。
func currentUserFromLoader(c *gin.Context, loader *store.DataLoader) *model.User {
	if cached, ok := c.Get(ctxKeyCurrentUser); ok {
		if u, ok := cached.(*model.User); ok {
			return u
		}
	}
	uid := currentUserID(c)
	if uid == 0 {
		return nil
	}
	u := loader.Users[uid]
	if u != nil {
		c.Set(ctxKeyCurrentUser, u)
	}
	return u
}
