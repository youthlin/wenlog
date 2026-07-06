package store

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gorm.io/gorm"
)

var (
	sqlTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wenlog_sql_total",
		Help: "SQL 执行总数",
	}, []string{"sql_type", "error"})

	sqlDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "wenlog_sql_duration_seconds",
		Help:    "SQL 执行耗时(秒)",
		Buckets: prometheus.DefBuckets,
	}, []string{"sql_type", "error"})

	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wenlog_dataloader_cache_hits_total",
		Help: "DataLoader 缓存命中次数",
	})

	cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wenlog_dataloader_cache_misses_total",
		Help: "DataLoader 缓存未命中次数（触发全量加载）",
	})
)

// 接口检测
var _ gorm.Plugin = (*GormSQLTracer)(nil)

// GormSQLTracer 统计 SQL 执行次数/时间等的插件。
type GormSQLTracer struct {
	Log    *slog.Logger
	Before func(db *gorm.DB)
	After  func(db *gorm.DB)
}

const (
	tracerPluginID   = "gorm:sql_tracer"
	tracerNameBefore = "gorm:sql_tracer_start"
	tracerNameAfter  = "gorm:sql_tracer_end"
)

// Name 实现 gorm.Plugin。
func (g *GormSQLTracer) Name() string {
	return tracerPluginID
}

// Initialize 插件初始化，实现 gorm.Plugin。
func (g *GormSQLTracer) Initialize(db *gorm.DB) error {
	onBefore := g.Before
	if onBefore == nil {
		onBefore = g.beforeDefault
	}
	onAfter := g.After
	if onAfter == nil {
		onAfter = g.afterDefault
	}

	// 注入执行钩子：before 记录开始时间
	db.Callback().Create().Before("gorm:before_create").Register(tracerNameBefore, g.wrap(onBefore, "CREATE"))
	db.Callback().Query().Before("gorm:query").Register(tracerNameBefore, g.wrap(onBefore, "QUERY"))
	db.Callback().Delete().Before("gorm:before_delete").Register(tracerNameBefore, g.wrap(onBefore, "DELETE"))
	db.Callback().Update().Before("gorm:setup_reflect_value").Register(tracerNameBefore, g.wrap(onBefore, "UPDATE"))
	db.Callback().Row().Before("gorm:row").Register(tracerNameBefore, g.wrap(onBefore, "ROW"))
	db.Callback().Raw().Before("gorm:raw").Register(tracerNameBefore, g.wrap(onBefore, "RAW"))

	// 注入 SQL hint（在 SQL 构建完成后、执行前）
	db.Callback().Create().After("gorm:create").Register("gorm:sql_tracer_hint_create", injectSQLHint)
	db.Callback().Query().After("gorm:query").Register("gorm:sql_tracer_hint_query", injectSQLHint)
	db.Callback().Delete().After("gorm:delete").Register("gorm:sql_tracer_hint_delete", injectSQLHint)
	db.Callback().Update().After("gorm:update").Register("gorm:sql_tracer_hint_update", injectSQLHint)
	db.Callback().Row().After("gorm:row").Register("gorm:sql_tracer_hint_row", injectSQLHint)
	db.Callback().Raw().After("gorm:raw").Register("gorm:sql_tracer_hint_raw", injectSQLHint)

	// after 记录耗时和详情
	db.Callback().Create().After("gorm:after_create").Register(tracerNameAfter, onAfter)
	db.Callback().Query().After("gorm:after_query").Register(tracerNameAfter, onAfter)
	db.Callback().Delete().After("gorm:after_delete").Register(tracerNameAfter, onAfter)
	db.Callback().Update().After("gorm:after_update").Register(tracerNameAfter, onAfter)
	db.Callback().Row().After("gorm:row").Register(tracerNameAfter, onAfter)
	db.Callback().Raw().After("gorm:raw").Register(tracerNameAfter, onAfter)
	return nil
}

// injectSQLHint 在 SQL 构建完成后，将 hint 注入到 SQL 语句前。
// 优先使用 ctx 中手动设置的 hint；否则自动从调用栈提取调用者函数名。
func injectSQLHint(db *gorm.DB) {
	hint := CtxGetSQLHint(db.Statement.Context)
	if hint == "" {
		hint = callerHint()
	}
	if hint == "" {
		return
	}
	oldSQL := db.Statement.SQL.String()
	// 将 hint 注释放在第一个 SQL 关键字之后，避免破坏 getFirstWord 的解析
	firstWordEnd := strings.IndexByte(oldSQL, ' ')
	if firstWordEnd > 0 {
		db.Statement.SQL.Reset()
		db.Statement.SQL.WriteString(oldSQL[:firstWordEnd])
		db.Statement.SQL.WriteString(" /*" + hint + "*/")
		db.Statement.SQL.WriteString(oldSQL[firstWordEnd:])
	} else {
		db.Statement.SQL.Reset()
		db.Statement.SQL.WriteString("/*" + hint + "*/ ")
		db.Statement.SQL.WriteString(oldSQL)
	}
}

// callerHint 从调用栈中提取调用链作为 hint，格式为 "caller -> callee"。
// 例如 handler 调用 store 方法时，生成 "public.Index -> store.ListPosts"。
func callerHint() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(0, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	// 第一遍：找到 store 层方法（callee）
	callee := ""
	for {
		frame, more := frames.Next()
		fn := frame.Function
		if isInternalFrame(fn) {
			if !more {
				break
			}
			continue
		}
		callee = shortFuncName(fn)
		break
	}
	if callee == "" {
		return ""
	}

	// 第二遍：继续往上找调用者（caller），跳过 store 包自身
	caller := ""
	for {
		frame, more := frames.Next()
		fn := frame.Function
		if isInternalFrame(fn) || strings.Contains(fn, "/store.") || strings.Contains(fn, "/store/") {
			if !more {
				break
			}
			continue
		}
		caller = shortFuncName(fn)
		break
	}
	if caller == "" {
		return callee
	}
	return caller + " -> " + callee
}

// isInternalFrame 判断是否为框架/运行时内部帧，应跳过。
func isInternalFrame(fn string) bool {
	return strings.HasSuffix(fn, ".callerHint") ||
		strings.HasSuffix(fn, ".injectSQLHint") ||
		strings.HasSuffix(fn, ".wrap") ||
		strings.HasSuffix(fn, ".beforeDefault") ||
		strings.HasSuffix(fn, ".afterDefault") ||
		strings.Contains(fn, "gorm.io/gorm") ||
		strings.Contains(fn, "runtime.") ||
		// SQL 调用栈有时会经过反射分发（例如 GORM callback 或测试替身），
		// 这些帧不是业务调用点，跳过后才能在 SQL Debug 中看到更有用的上层函数名。
		strings.Contains(fn, "reflect.")
}

// shortFuncName 取完整函数名的简短形式（去掉包路径前缀）。
func shortFuncName(fn string) string {
	if idx := strings.LastIndexByte(fn, '/'); idx >= 0 {
		fn = fn[idx+1:]
	}
	return fn
}

func (g *GormSQLTracer) wrap(fn func(*gorm.DB), sqlType string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		if g.Log != nil {
			hasErr := false
			if db.Statement.Error != nil {
				hasErr = true
			}
			g.Log.Debug("before execute sql",
				slog.String("sql_type", sqlType),
				slog.Bool("has_error", hasErr),
			)
		}
		fn(db)
	}
}

// beforeDefault 在执行 SQL 之前注入的默认钩子：记录开始时间。
func (g *GormSQLTracer) beforeDefault(db *gorm.DB) {
	db.InstanceSet(tracerPluginID, time.Now())
}

// afterDefault 执行 SQL 之后运行的默认钩子：计算耗时、记录详情。
func (g *GormSQLTracer) afterDefault(db *gorm.DB) {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if x := recover(); x != nil {
			if g.Log != nil {
				g.Log.WarnContext(ctx, "SQL tracer panic recovered", slog.Any("panic", x))
			}
		}
	}()

	// 取出开始执行时间
	val, ok := db.InstanceGet(tracerPluginID)
	if !ok {
		return
	}
	start, ok := val.(time.Time)
	if !ok {
		return
	}

	end := time.Now()
	cost := end.Sub(start)
	err := db.Statement.Error
	rowsAffected := db.Statement.RowsAffected
	sqlText := db.Statement.SQL.String()
	sqlType := getFirstWord(sqlText)
	hasErr := "false"
	if err != nil {
		hasErr = "true"
	}

	// Prometheus 打点
	sqlTotal.WithLabelValues(sqlType, hasErr).Inc()
	sqlDuration.WithLabelValues(sqlType, hasErr).Observe(cost.Seconds())

	// 记录 SQL 执行日志
	if g.Log != nil {
		attrs := []slog.Attr{
			slog.String("sql_type", sqlType),
			slog.Int64("rows", rowsAffected),
			slog.Duration("cost", cost),
		}
		if err != nil {
			attrs = append(attrs, slog.Any("error", err))
		}
		g.Log.LogAttrs(ctx, slog.LevelDebug, "sql executed", attrs...)
	}

	// 如果 ctx 中注入了 SqlDetails，则追加详情
	if d := CtxGetSQLDetails(ctx); d != nil {
		explainedSQL := db.Dialector.Explain(sqlText, db.Statement.Vars...)
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		d.Append(&SQLDetail{
			Cmd:   sqlType,
			Rows:  rowsAffected,
			SQL:   explainedSQL,
			Cost:  cost,
			Used:  cost.String(),
			Start: start.Format("2006-01-02 15:04:05.000"),
			End:   end.Format("2006-01-02 15:04:05.000"),
			Err:   errStr,
		})
	}
}

func getFirstWord(sql string) string {
	s := strings.Fields(sql)
	if len(s) > 0 {
		return s[0]
	}
	return "UNKNOWN"
}

// ---------- context 中存取 SQL 详情 ----------

type sqlDetailsKey struct{}

// CtxWithSQLDetails 在 ctx 中注入 SQL 详情收集器。如果 details 非 nil 则复用，否则新建。
func CtxWithSQLDetails(ctx context.Context, details *SQLDetails) context.Context {
	if details == nil {
		details = &SQLDetails{}
	}
	return context.WithValue(ctx, sqlDetailsKey{}, details)
}

// CtxGetSQLDetails 从 ctx 中取出记录 SQL 详情的 SQLDetails 结构。
func CtxGetSQLDetails(ctx context.Context) *SQLDetails {
	if d := ctx.Value(sqlDetailsKey{}); d != nil {
		return d.(*SQLDetails)
	}
	return nil
}

// SQLDetails 记录一次请求中所有 SQL 执行的详情。
type SQLDetails struct {
	mu      sync.Mutex
	TraceID string
	Details []*SQLDetail
}

// Append 追加一条 SQL 执行详情。
func (d *SQLDetails) Append(detail *SQLDetail) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Details = append(d.Details, detail)
}

// List 返回所有 SQL 详情的副本。
func (d *SQLDetails) List() []*SQLDetail {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*SQLDetail, len(d.Details))
	copy(out, d.Details)
	return out
}

// SQLDetail 单条 SQL 执行详情。
type SQLDetail struct {
	Cmd   string        // SQL 类型: SELECT, INSERT, UPDATE 等
	Rows  int64         // 影响行数
	SQL   string        // 完整的 SQL 语句(占位符已替换为实际值)
	Cost  time.Duration // 耗时
	Used  string        // 耗时字符串
	Start string        // 开始时间
	End   string        // 结束时间
	Err   string        // 错误信息(空表示无错误)
}

// FormatSQLDetails 将 SQLDetails 格式化为人类可读的字符串，方便调试输出。
func FormatSQLDetails(d *SQLDetails) string {
	if d == nil {
		return ""
	}
	list := d.List()
	if len(list) == 0 {
		return ""
	}
	var cost time.Duration
	for _, detail := range list {
		cost += detail.Cost
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共[%d]条 SQL, 总计耗时[%s]:\n", len(list), cost)
	for i, detail := range list {
		fmt.Fprintf(&b, "[%02d] rows=%d | cost=%s", i+1, detail.Rows, detail.Used)
		if detail.Err != "" {
			fmt.Fprintf(&b, " | err=%s", detail.Err)
		}
		fmt.Fprintf(&b, "\n%s\n", detail.SQL)
	}
	return b.String()
}

// LazySQLDetails 延迟格式化 SQL 详情，供模板使用。
// 在 base() 中注入到模板数据，模板渲染时从 ctx 中实时读取 SQLDetails。
type LazySQLDetails struct {
	Ctx context.Context
}

// String 实现 fmt.Stringer，模板中 {{.SQLDetails}} 会调用此方法。
func (l *LazySQLDetails) String() string {
	if l == nil || l.Ctx == nil {
		return ""
	}
	return FormatSQLDetails(CtxGetSQLDetails(l.Ctx))
}

// Count 返回已收集的 SQL 条数。 模板中会使用 {{.SQLDetails.Count}}
func (l *LazySQLDetails) Count() int {
	if l == nil || l.Ctx == nil {
		return 0
	}
	d := CtxGetSQLDetails(l.Ctx)
	if d == nil {
		return 0
	}
	return len(d.List())
}

// ---------- context 中存取 SQL hint ----------

type sqlHintKey struct{}

// CtxWithSQLHint 在 ctx 中注入 SQL hint，后续 SQL 语句前会自动添加 /*hint*/ 注释。
func CtxWithSQLHint(ctx context.Context, hint string) context.Context {
	return context.WithValue(ctx, sqlHintKey{}, hint)
}

// CtxGetSQLHint 从 ctx 中取出 SQL hint。
func CtxGetSQLHint(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(sqlHintKey{}); v != nil {
		return v.(string)
	}
	return ""
}
