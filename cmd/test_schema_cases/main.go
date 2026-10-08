// Command test_schema_cases 是 dataschema「yml -> 表结构同步」能力的全 case 回归程序。
//
// etc/ 下的每一个目录都是"表结构的一个版本"，程序按顺序把它们跑一遍，
// 覆盖建表、加字段、改类型、改注释、改默认值、删字段、索引增删改、全文索引、
// 主键变更、字符集漂移、DropPolicy、分表、DryRun 导出、编译产物回读、告警与报错。
//
// 每个用例都会做四件事：
//  1. 先 DryRun 预览，把变更清单与期望值逐条比对（含"被跳过"与"高危"标记）；
//  2. 真的执行；
//  3. 再 DryRun 一次，确认结构与配置一致时不会产生任何 SQL（幂等）；
//  4. 去 information_schema 复查数据库的真实状态（列顺序、索引列、注释、字符集……）。
//
// 数据库连接一律从环境变量读取，代码里不写任何账号密码：
//
//	export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
//	go run ./cmd/test_schema_cases
//
// 常用参数：
//
//	-only 关键字    只跑名字里带该关键字的用例（注意用例之间有先后依赖）
//	-from 关键字    从名字里带该关键字的用例开始跑到最后
//	-v             打印每个用例的完整过程输出（默认只在失败时打印）
//	-no-reset      开始前不清理 ds_case_ 开头的表
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	dataschema "github.com/k-kkong/dataschema"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// tablePrefix 所有用例表的统一前缀，清理时只认这个前缀，绝不碰库里其它表
	tablePrefix = "ds_case_"
	// dsnEnv 数据库连接串的环境变量名
	dsnEnv = "APP_MYSQL_DSN"
)

// env 用例执行期间能用到的东西
type env struct {
	db   *gorm.DB
	dsn  string
	base string // 用例根目录，etc/ 与 out/ 都在它下面
}

// outPath 用例产物（导出的 SQL、编译产物）的存放路径
func (e *env) outPath(name string) string {
	return filepath.Join(e.base, "out", name)
}

func main() {
	var (
		only    = flag.String("only", "", "只跑名字里包含该关键字的用例")
		from    = flag.String("from", "", "从名字里包含该关键字的用例开始跑")
		verbose = flag.Bool("v", false, "打印每个用例的完整过程输出")
		noReset = flag.Bool("no-reset", false, "开始前不清理 "+tablePrefix+" 开头的表")
	)
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv(dsnEnv))
	if dsn == "" {
		fmt.Printf("请先设置环境变量 %s，例如：\n", dsnEnv)
		fmt.Printf("  export %s=\"user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local\"\n", dsnEnv)
		fmt.Printf("连接串不要写进代码，这个程序只从环境变量读。\n")
		os.Exit(1)
	}

	e := &env{dsn: dsn, base: resolveBase()}
	e.db = openDB(dsn)

	fmt.Printf("数据库：%s\n", dbVersion(e.db))
	fmt.Printf("用例目录：%s\n", filepath.Join(e.base, "etc"))
	if !*noReset {
		resetTables(e.db)
	}
	os.MkdirAll(filepath.Join(e.base, "out"), os.ModePerm)

	var (
		list    = cases()
		started = *from == ""
		passed  int
		failed  []string
		skipped int
	)
	for _, c := range list {
		if !started {
			if strings.Contains(c.name, *from) {
				started = true
			} else {
				skipped++
				continue
			}
		}
		if *only != "" && !strings.Contains(c.name, *only) {
			skipped++
			continue
		}
		if fails := runCase(c, e, *verbose); len(fails) == 0 {
			passed++
		} else {
			failed = append(failed, c.name)
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 78))
	fmt.Printf("汇总：通过 %d 个，失败 %d 个，跳过 %d 个\n", passed, len(failed), skipped)
	if len(failed) > 0 {
		fmt.Printf("失败用例：%s\n", strings.Join(failed, "、"))
		fmt.Printf("\n提示：%s 开头的表不会自动清理，可用 -no-reset 保留现场排查。\n", tablePrefix)
		os.Exit(1)
	}
	fmt.Printf("全部用例通过。%s 开头的用例表已保留在库里，可自行 SHOW CREATE TABLE 对照。\n", tablePrefix)
}

// resolveBase 定位用例根目录：既支持在仓库根目录跑，也支持在 cmd/test_schema_cases 下跑
func resolveBase() string {
	for _, c := range []string{".", filepath.Join("cmd", "test_schema_cases")} {
		if _, err := os.Stat(filepath.Join(c, "etc", "demo_create")); err == nil {
			return c
		}
	}
	fmt.Println("找不到用例目录 etc/demo_create，请在仓库根目录或 cmd/test_schema_cases 目录下运行")
	os.Exit(1)
	return ""
}

func openDB(dsn string) *gorm.DB {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		// 关掉 gorm 的 SQL 日志，本程序自己会打印变更报告
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Printf("连接数据库失败：%v\n", err)
		os.Exit(1)
	}
	return db
}

func dbVersion(db *gorm.DB) string {
	var v string
	if err := db.Raw("SELECT VERSION()").Scan(&v).Error; err != nil {
		return "未知（" + err.Error() + "）"
	}
	return v
}

// resetTables 清掉上一轮留下的用例表，保证每次都是干净的起点。
// 只删 tablePrefix 开头的表，测试库里的其它表一律不碰。
func resetTables(db *gorm.DB) {
	var names []string
	if err := db.Raw("SHOW TABLES").Scan(&names).Error; err != nil {
		fmt.Printf("读取表清单失败：%v\n", err)
		os.Exit(1)
	}
	var dropped []string
	for _, n := range names {
		if !strings.HasPrefix(n, tablePrefix) {
			continue
		}
		if err := db.Exec("DROP TABLE IF EXISTS " + quote(n)).Error; err != nil {
			fmt.Printf("清理表 %s 失败：%v\n", n, err)
			os.Exit(1)
		}
		dropped = append(dropped, n)
	}
	if len(dropped) > 0 {
		fmt.Printf("已清理上一轮留下的 %d 张用例表：%s\n", len(dropped), strings.Join(dropped, ", "))
	}
}

// quote 用反引号包裹标识符
func quote(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// ---------------------------------------------------------------------------
// 用例执行
// ---------------------------------------------------------------------------

// runCase 跑一个用例，返回失败原因清单（为空即通过）
func runCase(c tcase, e *env, verbose bool) []string {
	var (
		fails []string
		logs  []string
	)
	fail := func(format string, args ...interface{}) {
		fails = append(fails, fmt.Sprintf(format, args...))
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 78))
	fmt.Printf("【%s】%s\n", c.name, c.desc)
	fmt.Printf("配置目录：%s\n", filepath.Join(e.base, "etc", c.dir))

	// ---- 第一步：DryRun 预览，比对期望的变更清单 ----
	var (
		got     []string
		warns   []string
		sqlText string
	)
	out, rec := capture(func() {
		h := newHandler(e, c.dir, true)
		if c.config != nil {
			h = c.config(e, h)
		}
		h.ExecuteSchemaSafeCheck()
		got = summarize(h.GetChangeReport())
		warns = h.GetWarnings()
		sqlText = strings.Join(h.GetSql(), "\n")
	})
	logs = append(logs, "----- 预览阶段（DryRun）-----\n"+out)

	// 期望配置本身就报错的用例，到这里就该终止了
	if c.wantPanic != "" {
		switch {
		case rec == nil:
			fail("期望解析阶段直接终止，但程序跑完了")
		case !strings.Contains(out, c.wantPanic):
			fail("终止原因里没找到 %q", c.wantPanic)
		default:
			fmt.Printf("  通过：解析阶段按预期终止，原因包含 %q\n", c.wantPanic)
		}
		return finish(c, fails, logs, verbose)
	}
	if rec != nil {
		fail("预览阶段异常终止：%v", rec)
		return finish(c, fails, logs, verbose)
	}

	if !sameList(got, c.want) {
		fail("变更清单不符\n    期望：%s\n    实际：%s", joinList(c.want), joinList(got))
	} else {
		fmt.Printf("  通过：变更清单 = [%s]\n", joinList(got))
	}
	for _, w := range c.wantWarn {
		if !containsAny(warns, w) {
			fail("期望出现包含 %q 的告警，实际告警：%s", w, joinList(warns))
		}
	}
	if len(c.wantWarn) > 0 {
		fmt.Printf("  通过：告警 %d 条\n", len(warns))
	}
	for _, s := range c.sqlHas {
		if !strings.Contains(sqlText, s) {
			fail("生成的 SQL 里没找到片段：%s", s)
		}
	}
	for _, s := range c.sqlNot {
		if strings.Contains(sqlText, s) {
			fail("生成的 SQL 里不该出现片段：%s", s)
		}
	}
	if len(c.sqlHas)+len(c.sqlNot) > 0 {
		fmt.Printf("  通过：SQL 内容符合预期\n")
	}

	// ---- 第二步：真的执行 ----
	if !c.dryOnly {
		out, rec = capture(func() {
			h := newHandler(e, c.dir, false)
			if c.config != nil {
				h = c.config(e, h)
			}
			h.ExecuteSchema()
		})
		logs = append(logs, "----- 执行阶段 -----\n"+out)
		if rec != nil {
			fail("执行阶段异常终止：%v", rec)
			return finish(c, fails, logs, verbose)
		}

		// ---- 第三步：幂等复检，再比一次必须是 0 条待执行变更 ----
		if !c.noIdem {
			var pending int
			out, rec = capture(func() {
				h := newHandler(e, c.dir, true)
				if c.config != nil {
					h = c.config(e, h)
				}
				h.ExecuteSchemaSafeCheck()
				pending = pendingCount(h.GetChangeReport())
			})
			logs = append(logs, "----- 幂等复检 -----\n"+out)
			if rec != nil {
				fail("幂等复检异常终止：%v", rec)
			} else if pending != 0 {
				fail("执行完再比对仍有 %d 条待执行变更，说明比对不幂等", pending)
			} else {
				fmt.Printf("  通过：执行完再比对为 0 条变更（幂等）\n")
			}
		}
	} else {
		fmt.Printf("  DryRun 用例，跳过执行\n")
	}

	// ---- 第四步：复查数据库真实状态 ----
	if c.check != nil {
		if err := c.check(e); err != nil {
			fail("数据库状态复查失败：%v", err)
		} else {
			fmt.Printf("  通过：数据库状态复查\n")
		}
	}
	if c.extra != nil {
		if err := c.extra(e); err != nil {
			fail("附加流程失败：%v", err)
		} else {
			fmt.Printf("  通过：附加流程\n")
		}
	}
	return finish(c, fails, logs, verbose)
}

// finish 收口一个用例：失败或 -v 时把过程输出打出来
func finish(c tcase, fails, logs []string, verbose bool) []string {
	if len(fails) == 0 {
		fmt.Printf("结果：通过\n")
		if verbose {
			printLogs(logs)
		}
		return nil
	}
	fmt.Printf("结果：失败\n")
	for _, f := range fails {
		fmt.Printf("  × %s\n", f)
	}
	printLogs(logs)
	return fails
}

func printLogs(logs []string) {
	fmt.Printf("%s\n", strings.Repeat("-", 78))
	for _, l := range logs {
		fmt.Println(strings.TrimRight(l, "\n"))
		fmt.Printf("%s\n", strings.Repeat("-", 78))
	}
}

// newHandler 每个阶段都用一个全新的 handler：
// handler 内部的状态是一次性的，复用会让上一轮的变更清单串到这一轮。
// 连接统一用 SetDB 传进去，避免每个用例都新建一条数据库连接。
func newHandler(e *env, dir string, dryRun bool) *dataschema.YamlToSqlHandler {
	return dataschema.NewYamlToSqlHandler().
		SetDB(e.db).
		SetYamlPath(filepath.Join(e.base, "etc", dir)).
		SetDryRun(dryRun)
}

// summarize 把变更记录压成一行行可读的断言目标，格式：表名/变更类型:对象(高危)(跳过)
func summarize(changes []dataschema.SchemaChange) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		s := c.Table + "/" + c.Kind
		if c.Object != "" {
			s += ":" + c.Object
		}
		if c.Dangerous {
			s += "(高危)"
		}
		if c.Skipped {
			s += "(跳过)"
		}
		out = append(out, s)
	}
	return out
}

// pendingCount 统计"会真的执行"的变更条数。
// 被跳过的变更一定不带 SQL，所以这个数为 0 就等价于没有任何语句要执行。
func pendingCount(changes []dataschema.SchemaChange) int {
	var n int
	for _, c := range changes {
		if !c.Skipped {
			n++
		}
	}
	return n
}

// capture 把 fn 执行期间的标准输出抓出来。
// 用例通过时只打印一行结论，失败时才把完整过程倒出来，否则几百行日志会把结论淹掉。
func capture(fn func()) (out string, recovered interface{}) {
	r, w, err := os.Pipe()
	if err != nil {
		defer func() { recovered = recover() }()
		fn()
		return "", recovered
	}
	orig := os.Stdout
	os.Stdout = w
	collect := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		io.Copy(&b, r)
		collect <- b.String()
	}()
	func() {
		defer func() { recovered = recover() }()
		fn()
	}()
	w.Close()
	os.Stdout = orig
	return <-collect, recovered
}

// ---------------------------------------------------------------------------
// 数据库状态复查用的小工具
// ---------------------------------------------------------------------------

// tableExists 表在不在
func tableExists(e *env, table string) (bool, error) {
	var n int64
	err := e.db.Raw("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table).Scan(&n).Error
	return n > 0, err
}

// columnOrder 表在库里的列顺序
func columnOrder(e *env, table string) ([]string, error) {
	var cols []string
	err := e.db.Raw("SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", table).Scan(&cols).Error
	return cols, err
}

// hasColumn 列在不在
func hasColumn(e *env, table, column string) (bool, error) {
	var n int64
	err := e.db.Raw("SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?", table, column).Scan(&n).Error
	return n > 0, err
}

// hasIndex 索引在不在
func hasIndex(e *env, table, index string) (bool, error) {
	var n int64
	err := e.db.Raw("SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?", table, index).Scan(&n).Error
	return n > 0, err
}

// indexColumns 索引包含的列，按索引内顺序
func indexColumns(e *env, table, index string) ([]string, error) {
	var cols []string
	err := e.db.Raw("SELECT COLUMN_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ? ORDER BY SEQ_IN_INDEX", table, index).Scan(&cols).Error
	return cols, err
}

// tableInfo 表注释与排序规则
func tableInfo(e *env, table string) (comment, collation string, err error) {
	var row struct {
		Comment   string `gorm:"column:c"`
		Collation string `gorm:"column:o"`
	}
	err = e.db.Raw("SELECT TABLE_COMMENT AS c, TABLE_COLLATION AS o FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table).Scan(&row).Error
	return row.Comment, row.Collation, err
}

// columnInfo 列的类型、注释、可空性、默认值
func columnInfo(e *env, table, column string) (decl, comment, nullable, def string, err error) {
	var row struct {
		Decl     string `gorm:"column:t"`
		Comment  string `gorm:"column:c"`
		Nullable string `gorm:"column:n"`
		Default  string `gorm:"column:d"`
	}
	err = e.db.Raw("SELECT COLUMN_TYPE AS t, COLUMN_COMMENT AS c, IS_NULLABLE AS n, COALESCE(COLUMN_DEFAULT, '<null>') AS d "+
		"FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?", table, column).Scan(&row).Error
	return row.Decl, row.Comment, row.Nullable, row.Default, err
}

// ---------------------------------------------------------------------------
// 断言小工具
// ---------------------------------------------------------------------------

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinList(list []string) string {
	if len(list) == 0 {
		return "无"
	}
	return strings.Join(list, " | ")
}

func containsAny(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// eqList 断言字符串清单一致，返回可直接打印的错误
func eqList(label string, got, want []string) error {
	if !sameList(got, want) {
		return fmt.Errorf("%s 期望 [%s]，实际 [%s]", label, joinList(want), joinList(got))
	}
	return nil
}

// eq 断言单值一致
func eq(label, got, want string) error {
	if got != want {
		return fmt.Errorf("%s 期望 %q，实际 %q", label, want, got)
	}
	return nil
}

// 下面这组断言把"查询 + 判定 + 报错文案"收在一起，
// 用例里就只剩一行 expectXxx，读起来跟自然语言一样。

// expectExists 断言表存在
func expectExists(e *env, table string) error {
	ok, err := tableExists(e, table)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("表 %s 应该存在", table)
	}
	return nil
}

// expectNotExists 断言表不存在
func expectNotExists(e *env, table string) error {
	ok, err := tableExists(e, table)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("表 %s 不该存在", table)
	}
	return nil
}

// expectColumn 断言列存在
func expectColumn(e *env, table, column string) error {
	ok, err := hasColumn(e, table, column)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s.%s 应该存在", table, column)
	}
	return nil
}

// expectNoColumn 断言列已被删除
func expectNoColumn(e *env, table, column string) error {
	ok, err := hasColumn(e, table, column)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%s.%s 应该已被删除", table, column)
	}
	return nil
}

// expectIndex 断言索引存在
func expectIndex(e *env, table, index string) error {
	ok, err := hasIndex(e, table, index)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s 的索引 %s 应该存在", table, index)
	}
	return nil
}

// expectNoIndex 断言索引已被删除
func expectNoIndex(e *env, table, index string) error {
	ok, err := hasIndex(e, table, index)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%s 的索引 %s 应该已被删除", table, index)
	}
	return nil
}
