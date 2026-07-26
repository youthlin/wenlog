package hook

import gettext "github.com/youthlin/t"

// T 翻译普通消息。
func (api *API) T(msg string, args ...any) string {
	return api.translator().T(msg, args...)
}

// N 按数量翻译单复数消息。
func (api *API) N(singular, plural string, n int, args ...any) string {
	return api.translator().N(singular, plural, n, args...)
}

// X 按上下文翻译消息。
func (api *API) X(ctx, msg string, args ...any) string {
	return api.translator().X(ctx, msg, args...)
}

// XN 按上下文和数量翻译单复数消息。
func (api *API) XN(ctx, singular, plural string, n int, args ...any) string {
	return api.translator().XN(ctx, singular, plural, n, args...)
}

func (api *API) translator() *gettext.Translations {
	if api == nil {
		return gettext.Global()
	}
	tr := gettext.Global()
	if api.ctx != nil {
		tr = gettext.WithContext(api.ctx)
	}
	if api.domain != "" {
		tr = tr.D(api.domain)
	}
	return tr
}
