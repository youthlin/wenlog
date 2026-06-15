// Package permalink 集中管理永久链接的生成、解析与运行时配置。
//
// 技术方案说明（记录在包内，便于后续维护）：
//
//  1. 站点允许配置“文章永久链接结构”以及“分类/标签页面前缀”，页面仍然保持单段
//     /{slug}，这样可以继续复用现有页面模型与导航逻辑。
//  2. 文章永久链接结构兼容一组常见 WordPress 占位符：
//     %year% %monthnum% %day% %hour% %minute% %second%
//     %post_id% %postname% %category% %author%
//     同时允许夹杂任意固定文本，比如 /%year%%post_id%.html。
//  3. 包内部把结构串编译成两份产物：
//     - 生成模板：渲染文章对象时按占位符替换；
//     - 解析正则：收到请求路径时反向提取出 post_id、postname、日期、分类、作者等条件。
//  4. 解析出来的结构化条件会交给 store 层查询数据库，真正决定“这条 URL 指向哪篇文章”的
//     不是 nginx/apache，而是应用自己的路由 + 规则解析 + 数据库查询。对于 Go 服务来说，
//     只要请求最终都进了当前进程，就完全可以自行实现 WordPress 风格固定链接。
//  5. 为了兼容模板、feed、评论跳转、导出等到处都会生成文章 URL 的场景，这个包维护一份
//     进程级当前规则。启动时从 settings 载入；后台修改设置后立即刷新。这样整个进程内的
//     链接生成与请求解析始终使用同一份规则。
//  6. 当前 %category% 取“文章主分类”的 slug（按分类 ID 最小值稳定选择），不展开父分类
//     层级路径；%author% 取后台用户的 username；%postname% 取文章 slug。
package permalink

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/youthlin/blog/internal/consts"
	"github.com/youthlin/blog/internal/model"
)

type tokenSpec struct {
	name        string
	pattern     string
	variableLen bool
	render      func(*model.Post) string
}

var tokenSpecs = map[string]tokenSpec{
	"year": {
		name:    "year",
		pattern: `\d{4}`,
		render:  func(p *model.Post) string { return fmt.Sprintf("%04d", p.PublishedAt.Year()) },
	},
	"monthnum": {
		name:    "monthnum",
		pattern: `\d{2}`,
		render:  func(p *model.Post) string { return fmt.Sprintf("%02d", int(p.PublishedAt.Month())) },
	},
	"day": {
		name:    "day",
		pattern: `\d{2}`,
		render:  func(p *model.Post) string { return fmt.Sprintf("%02d", p.PublishedAt.Day()) },
	},
	"hour": {
		name:    "hour",
		pattern: `\d{2}`,
		render:  func(p *model.Post) string { return fmt.Sprintf("%02d", p.PublishedAt.Hour()) },
	},
	"minute": {
		name:    "minute",
		pattern: `\d{2}`,
		render:  func(p *model.Post) string { return fmt.Sprintf("%02d", p.PublishedAt.Minute()) },
	},
	"second": {
		name:    "second",
		pattern: `\d{2}`,
		render:  func(p *model.Post) string { return fmt.Sprintf("%02d", p.PublishedAt.Second()) },
	},
	"post_id": {
		name:        "post_id",
		pattern:     `\d+`,
		variableLen: true,
		render:      func(p *model.Post) string { return strconv.FormatUint(uint64(p.ID), 10) },
	},
	"postname": {
		name:        "postname",
		pattern:     `[^/?#]+?`,
		variableLen: true,
		render:      func(p *model.Post) string { return strings.TrimSpace(p.Slug) },
	},
	"category": {
		name:        "category",
		pattern:     `[^?#]+?`,
		variableLen: true,
		render:      func(p *model.Post) string { return primaryCategorySlug(p) },
	},
	"author": {
		name:        "author",
		pattern:     `[^/?#]+?`,
		variableLen: true,
		render:      func(p *model.Post) string { return strings.TrimSpace(p.Author.Username) },
	},
}

type part struct {
	literal string
	token   string
}

type compiledPattern struct {
	normalized string
	parts      []part
	regex      *regexp.Regexp
	usedTokens map[string]bool
}

// PostPathMatch 是把请求路径按当前固定链接规则解析后的结果。
type PostPathMatch struct {
	Year      int
	Month     int
	Day       int
	Hour      int
	Minute    int
	Second    int
	PostID    uint
	PostName  string
	Category  string
	Author    string
	HasYear   bool
	HasMonth  bool
	HasDay    bool
	HasHour   bool
	HasMinute bool
	HasSecond bool
	HasPostID bool
	HasName   bool
	HasCat    bool
	HasAuthor bool
}

var currentPattern = struct {
	mu sync.RWMutex
	cp *compiledPattern
}{cp: mustCompile(consts.SettingsPostPermalinkDefault)}

var currentTaxonomy = struct {
	mu             sync.RWMutex
	categoryPrefix string
	tagPrefix      string
}{
	categoryPrefix: consts.SettingsCategoryPrefixDefault,
	tagPrefix:      consts.SettingsTagPrefixDefault,
}

// NormalizePostPattern 归一化固定链接结构。
func NormalizePostPattern(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return consts.SettingsPostPermalinkDefault
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return raw
}

// ValidatePostPattern 校验固定链接结构是否合法。
func ValidatePostPattern(raw string) error {
	_, err := compile(raw)
	return err
}

// SetPostPattern 更新当前进程使用的文章固定链接结构。
// 如果传入值非法，则回退为默认结构。
func SetPostPattern(raw string) {
	cp, err := compile(raw)
	if err != nil {
		cp = mustCompile(consts.SettingsPostPermalinkDefault)
	}
	currentPattern.mu.Lock()
	currentPattern.cp = cp
	currentPattern.mu.Unlock()
}

// CurrentPostPattern 返回当前正在使用的文章固定链接结构。
func CurrentPostPattern() string {
	currentPattern.mu.RLock()
	defer currentPattern.mu.RUnlock()
	return currentPattern.cp.normalized
}

// CurrentPatternUsesToken 判断当前规则是否使用某个占位符。
func CurrentPatternUsesToken(token string) bool {
	currentPattern.mu.RLock()
	defer currentPattern.mu.RUnlock()
	return currentPattern.cp.usedTokens[token]
}

// Post 返回文章或页面的永久链接路径。
func Post(p *model.Post) string {
	if p.PostType == "page" {
		return Page(p)
	}
	currentPattern.mu.RLock()
	cp := currentPattern.cp
	currentPattern.mu.RUnlock()
	return cp.render(p)
}

// Page 返回页面的永久链接路径,如 /about。
func Page(p *model.Post) string {
	return "/" + p.Slug
}

// ParsePostPath 按当前固定链接规则解析文章路径。
func ParsePostPath(path string) (*PostPathMatch, bool) {
	currentPattern.mu.RLock()
	cp := currentPattern.cp
	currentPattern.mu.RUnlock()
	return cp.match(path)
}

// Category 返回分类页路径。
func Category(slug string) string {
	currentTaxonomy.mu.RLock()
	prefix := currentTaxonomy.categoryPrefix
	currentTaxonomy.mu.RUnlock()
	return "/" + prefix + "/" + slug
}

// Tag 返回标签页路径。
func Tag(slug string) string {
	currentTaxonomy.mu.RLock()
	prefix := currentTaxonomy.tagPrefix
	currentTaxonomy.mu.RUnlock()
	return "/" + prefix + "/" + slug
}

func NormalizeTaxonomyPrefix(raw, def string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return def
	}
	return strings.ToLower(raw)
}

func ValidateTaxonomyPrefix(raw string) error {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return fmt.Errorf("前缀不能为空")
	}
	if strings.ContainsRune(raw, '/') || strings.ContainsAny(raw, "?# \t\r\n") {
		return fmt.Errorf("前缀不能包含 /、空白、? 或 #")
	}
	if raw == "." || raw == ".." {
		return fmt.Errorf("前缀非法")
	}
	matched, err := regexp.MatchString(`^[A-Za-z0-9][A-Za-z0-9._-]*$`, raw)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("前缀仅支持字母、数字、点、下划线和连字符，且需以字母或数字开头")
	}
	return nil
}

func SetTaxonomyPrefixes(categoryPrefix, tagPrefix string) {
	currentTaxonomy.mu.Lock()
	currentTaxonomy.categoryPrefix = NormalizeTaxonomyPrefix(categoryPrefix, consts.SettingsCategoryPrefixDefault)
	currentTaxonomy.tagPrefix = NormalizeTaxonomyPrefix(tagPrefix, consts.SettingsTagPrefixDefault)
	currentTaxonomy.mu.Unlock()
}

func CurrentCategoryPrefix() string {
	currentTaxonomy.mu.RLock()
	defer currentTaxonomy.mu.RUnlock()
	return currentTaxonomy.categoryPrefix
}

func CurrentTagPrefix() string {
	currentTaxonomy.mu.RLock()
	defer currentTaxonomy.mu.RUnlock()
	return currentTaxonomy.tagPrefix
}

func ParseCategoryPath(path string) (slug string, ok bool) {
	currentTaxonomy.mu.RLock()
	prefix := currentTaxonomy.categoryPrefix
	currentTaxonomy.mu.RUnlock()
	return parseTaxonomyPath(path, prefix)
}

func ParseTagPath(path string) (slug string, ok bool) {
	currentTaxonomy.mu.RLock()
	prefix := currentTaxonomy.tagPrefix
	currentTaxonomy.mu.RUnlock()
	return parseTaxonomyPath(path, prefix)
}

func parseTaxonomyPath(path, prefix string) (slug string, ok bool) {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return "", false
	}
	marker := "/" + prefix + "/"
	if !strings.HasPrefix(path, marker) {
		return "", false
	}
	slug = strings.TrimPrefix(path, marker)
	if slug == "" || strings.ContainsRune(slug, '/') {
		return "", false
	}
	if decoded, err := url.PathUnescape(slug); err == nil {
		slug = decoded
	}
	return slug, true
}

func compile(raw string) (*compiledPattern, error) {
	normalized := NormalizePostPattern(raw)
	if strings.ContainsAny(normalized, "?#") {
		return nil, fmt.Errorf("固定链接结构不能包含 ? 或 #")
	}
	parts, err := parseParts(normalized)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, p := range parts {
		if p.token != "" {
			used[p.token] = true
		}
	}
	if len(used) == 0 {
		return nil, fmt.Errorf("固定链接结构至少需要一个占位符")
	}
	for i := 0; i < len(parts)-1; i++ {
		left, right := parts[i], parts[i+1]
		if left.token == "" || right.token == "" {
			continue
		}
		if tokenSpecs[left.token].variableLen && tokenSpecs[right.token].variableLen {
			return nil, fmt.Errorf("相邻的可变长度占位符之间需要插入分隔符，如 /、-、.html")
		}
	}
	var b strings.Builder
	b.WriteByte('^')
	for _, p := range parts {
		if p.token == "" {
			b.WriteString(regexp.QuoteMeta(p.literal))
			continue
		}
		spec := tokenSpecs[p.token]
		b.WriteString("(?P<")
		b.WriteString(spec.name)
		b.WriteString(">")
		b.WriteString(spec.pattern)
		b.WriteByte(')')
	}
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("编译固定链接规则失败: %w", err)
	}
	return &compiledPattern{normalized: normalized, parts: parts, regex: re, usedTokens: used}, nil
}

func mustCompile(raw string) *compiledPattern {
	cp, err := compile(raw)
	if err != nil {
		panic(err)
	}
	return cp
}

func parseParts(pattern string) ([]part, error) {
	var parts []part
	for len(pattern) > 0 {
		start := strings.IndexByte(pattern, '%')
		if start < 0 {
			parts = appendLiteral(parts, pattern)
			break
		}
		if start > 0 {
			parts = appendLiteral(parts, pattern[:start])
			pattern = pattern[start:]
		}
		end := strings.IndexByte(pattern[1:], '%')
		if end < 0 {
			return nil, fmt.Errorf("固定链接结构中的 %% 未闭合")
		}
		token := pattern[1 : end+1]
		if _, ok := tokenSpecs[token]; !ok {
			return nil, fmt.Errorf("不支持的占位符 %%%s%%", token)
		}
		parts = append(parts, part{token: token})
		pattern = pattern[end+2:]
	}
	return parts, nil
}

func appendLiteral(parts []part, literal string) []part {
	if literal == "" {
		return parts
	}
	if len(parts) > 0 && parts[len(parts)-1].token == "" {
		parts[len(parts)-1].literal += literal
		return parts
	}
	return append(parts, part{literal: literal})
}

func (cp *compiledPattern) render(p *model.Post) string {
	var b strings.Builder
	for _, part := range cp.parts {
		if part.token == "" {
			b.WriteString(part.literal)
			continue
		}
		b.WriteString(tokenSpecs[part.token].render(p))
	}
	return b.String()
}

func (cp *compiledPattern) match(path string) (*PostPathMatch, bool) {
	matches := cp.regex.FindStringSubmatch(path)
	if matches == nil {
		return nil, false
	}
	out := &PostPathMatch{}
	for i, name := range cp.regex.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		value := matches[i]
		switch name {
		case "year":
			out.Year, _ = strconv.Atoi(value)
			out.HasYear = true
		case "monthnum":
			out.Month, _ = strconv.Atoi(value)
			out.HasMonth = true
		case "day":
			out.Day, _ = strconv.Atoi(value)
			out.HasDay = true
		case "hour":
			out.Hour, _ = strconv.Atoi(value)
			out.HasHour = true
		case "minute":
			out.Minute, _ = strconv.Atoi(value)
			out.HasMinute = true
		case "second":
			out.Second, _ = strconv.Atoi(value)
			out.HasSecond = true
		case "post_id":
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, false
			}
			out.PostID = uint(n)
			out.HasPostID = true
		case "postname":
			out.PostName = value
			out.HasName = true
		case "category":
			out.Category = value
			out.HasCat = true
		case "author":
			out.Author = value
			out.HasAuthor = true
		}
	}
	return out, true
}

func primaryCategorySlug(p *model.Post) string {
	if len(p.Categories) == 0 {
		return ""
	}
	cs := append([]model.Category(nil), p.Categories...)
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].ID == cs[j].ID {
			return cs[i].Slug < cs[j].Slug
		}
		return cs[i].ID < cs[j].ID
	})
	return strings.TrimSpace(cs[0].Slug)
}
