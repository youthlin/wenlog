package render

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
	"github.com/youthlin/blog/internal/permalink"
	"github.com/youthlin/blog/internal/util"
	"github.com/youthlin/blog/internal/wxr"
)

const pattern = "*.gohtml"

func parseTemplates(fsys fs.FS, pattern string) (*template.Template, error) {
	funcs := CommonFuncMap()
	maps.Copy(funcs, markTplFuncMap())
	tpl, err := template.New("").
		Funcs(funcs).
		ParseFS(fsys, pattern)
	if err != nil {
		return nil, errors.Wrap(err, "模板解析失败")
	}
	return tpl, nil
}

// CommonFuncMap 返回主题和插件模板共用的基础模板函数。
// 插件模板解析时复用此函数集，确保插件模板也能使用 postURL、categoryURL 等函数。
func CommonFuncMap() template.FuncMap {
	return template.FuncMap{
		"postURL":          postURL,
		"categoryURL":      permalink.Category,
		"tagURL":           permalink.Tag,
		"safeHTML":         func(s string) template.HTML { return template.HTML(s) },
		"escapeHTML":       html.EscapeString,
		"detailHTML":       detailHTML,
		"hasMore":          func(content string) bool { _, m := wxr.SplitMore(content); return m },
		"avatarURL":        avatarURL,
		"defaultAvatarURL": defaultAvatarURL,
		"fmtDate":          func(t time.Time) string { return t.Format("2006-01-02") },
		"fmtDateTime":      func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"fmtDateTimeLocal": func(t time.Time) string { return t.Format("2006-01-02T15:04") },
		"fmtFileSize":      fmtFileSize,
		"year":             func(t time.Time) int { return t.Year() },
		"add":              func(a, b int) int { return a + b },
		"sub":              func(a, b int) int { return a - b },
		"seq":              seq,
		"toInt":            func(s string) int { n, _ := strconv.Atoi(s); return n },
		"default": func(def, val any) any {
			if ok, _ := template.IsTrue(val); !ok {
				return def
			}
			return val
		},
	}
}

// postURL 返回固定链接
func postURL(p any) string {
	switch v := p.(type) {
	case *model.Post:
		if v == nil {
			return ""
		}
		return permalink.Post(v)
	case model.Post:
		return permalink.Post(&v)
	default:
		if isNilAny(p) {
			return ""
		}
		if v, ok := p.(interface {
			PostURLFields() struct {
				ID          uint
				Title       string
				Slug        string
				PostType    string
				PublishedAt time.Time
				ModifiedAt  time.Time
			}
		}); ok {
			fields := v.PostURLFields()
			if fields.ID > 0 {
				return permalink.Post(&model.Post{
					ID:          fields.ID,
					Title:       fields.Title,
					Slug:        fields.Slug,
					PostType:    fields.PostType,
					Status:      model.StatusPublished,
					PublishedAt: fields.PublishedAt,
					ModifiedAt:  fields.ModifiedAt,
				})
			}
		}
		// 必须保留反射兜底：主题/插件 functions.goyaegi 返回的 hook.PostView 穿过 yaegi 边界后，
		// 在宿主侧不一定能直接断言为 hook.PostView 或实现 PostURLFields 接口；模板函数又需要继续
		// 接受这些脚本值生成永久链接。因此这里仅按固定字段白名单读取生成 URL 所需的最小数据。
		rv := reflect.ValueOf(p)
		if rv.Kind() == reflect.Struct {
			id := reflectGetUint(rv, "ID")
			title := reflectGetString(rv, "Title")
			slug := reflectGetString(rv, "Slug")
			postType := reflectGetString(rv, "PostType")
			publishedAt, _ := reflectGetTime(rv, "PublishedAt")
			modifiedAt, _ := reflectGetTime(rv, "ModifiedAt")
			if id > 0 {
				mp := &model.Post{
					ID:          id,
					Title:       title,
					Slug:        slug,
					PostType:    postType,
					Status:      model.StatusPublished,
					PublishedAt: publishedAt,
					ModifiedAt:  modifiedAt,
				}
				return permalink.Post(mp)
			}
		}
		return ""
	}
}

func reflectGetUint(rv reflect.Value, field string) uint {
	f := rv.FieldByName(field)
	if f.IsValid() {
		switch f.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return uint(f.Uint())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v := f.Int()
			if v >= 0 {
				return uint(v)
			}
		}
	}
	return 0
}

func reflectGetString(rv reflect.Value, field string) string {
	f := rv.FieldByName(field)
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

func reflectGetTime(rv reflect.Value, field string) (time.Time, bool) {
	f := rv.FieldByName(field)
	if f.IsValid() && f.Type() == reflect.TypeOf(time.Time{}) {
		return f.Interface().(time.Time), true
	}
	return time.Time{}, false
}

// postExcerptHTML 返回列表页应展示的文章摘要 HTML: 有 more 标记则只取之前部分。
func postExcerptHTML(p *model.Post) template.HTML {
	above, hasMore := wxr.SplitMore(p.Content)
	if hasMore {
		return renderContentHTML(above)
	}
	if p.Excerpt != "" {
		return renderContentHTML(p.Excerpt)
	}
	return renderContentHTML(p.Content)
}

// detailHTML 返回详情页完整正文,把 <!--more--> 替换为锚点。
func detailHTML(p *model.Post) template.HTML {
	return renderContentHTML(wxr.RenderDetail(p.Content, p.ID))
}

// avatarURL 由邮箱生成 cravatar(国内镜像)头像 URL。
func avatarURL(email, defaultAvatar string) string {
	return "https://cn.cravatar.com/avatar/" + avatarHash(email) + "?s=" + strconv.Itoa(consts.AvatarSizeSmall) + "&d=" + util.NormalizeDefaultAvatar(defaultAvatar)
}

// defaultAvatarURL 获取强制展示指定默认头像的链接。
func defaultAvatarURL(defaultAvatar string) string {
	url := avatarURL("", defaultAvatar)
	return url + "&f=y"
}

func avatarHash(email string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

// fmtFileSize 将字节数格式化为可读字符串。
func fmtFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// seq 生成 [1..n] 整数切片,供分页模板迭代。
func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i + 1
	}
	return s
}
