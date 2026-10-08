package dataschema

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// exampleDSN 下面所有案例统一从这里拿数据库连接串。
//
// 连接串里带账号密码，不要写进代码、也不要提交到仓库，一律走环境变量：
//
//	export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
//
// 关于本文件里的案例，有两件事先说清楚：
//
//  1. 所有 Example 都没有 // Output: 注释，按 Go 的约定「只编译不执行」，
//     所以 go test ./... 只会校验它们的写法，不会去动你的数据库；
//     注意这时指定 -run 也不会执行（Go 的行为，不是本库的限制）。
//     想对着真库把所有 case 跑一遍并带上断言，用仓库里的回归程序：
//
//     export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
//     go run ./cmd/test_schema_cases
//
//  2. 每个 Example 开头的 if exampleDSN() == "" { return } 是第二道保险：
//     万一哪天给它们补上了 // Output: 注释，没配环境变量也只会跳过，
//     不会连到一个不存在的库上。
//
// yml -> 表结构同步的案例，用到的配置文件都在 ./cmd/test_schema_cases/etc/ 下。
// 那份目录本身就是「同一张表的多个版本」，想看结构怎么一步步演进（建表 -> 加字段
// -> 改类型 -> 改注释 -> 改默认值 -> 删字段 -> 索引增删改 -> 全文索引 -> 删索引），
// 直接翻对应的 yml 文件头注释即可；想看它们跑起来的完整过程与断言，执行：
//
//	go run ./cmd/test_schema_cases
func exampleDSN() string {
	return strings.TrimSpace(os.Getenv("APP_MYSQL_DSN"))
}

func ExampleTblToStructHandler_GenerateAllTblStruct() {
	if exampleDSN() == "" {
		return
	}

	//案例1 简易
	{
		th := NewTblToStructHandler()
		th.SetDsn("root:tiger@(127.0.0.1:3306)/pulingfu?charset=utf8mb4&parseTime=True&loc=Local").
			GenerateAllTblStruct()
	}

	//案例2 复杂
	{
		th := NewTblToStructHandler()
		th.SetDsn("root:tiger@(127.0.0.1:3306)/pulingfu?charset=utf8mb4&parseTime=True&loc=Local").
			SetStructOrmTag("gorm").     //设置所生成对应的orm 标记类型
			SetOtherTags("json", "msg"). //添加其他的标签 如json ==> `json:"xxx"` msg ==> `msg:"xxx"`
			SeTblStructColumnNameInfo(
				CAMEL_CASE,                         //设置字段名写法类型为骆驼写法
				FIELD_ORDER_FIELD_NAME,             // 设置字段名排序方式为字段名称排序
				"column_prefix_", "_column_suffix", // 设置字段名前缀和后缀 可以为空字符串
			).
			SetTblStructNameInfo(CAMEL_CASE, "tbl_prefix_", "_tbl_suffix"). //设置生成的结构体名类型为CamelCase写法，以及前后缀
			GenerateAllTblStruct()
	}

	// 案例3 高度自定义
	{
		th := NewTblToStructHandler()
		savePrefix := "./pkg/models/tbl_sql_auto_model/" //设置保存路径前缀
		th.SetDsn(exampleDSN())
		fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
		fmt.Printf("\x1b[%dm您即将自动生成go struct model： \x1b[0m\n", 34)
		fmt.Printf("\x1b[%dm多表可以使用逗号隔开： \x1b[0m\n", 34)
		fmt.Printf("\x1b[%dm请输入要生成的sql表名： \x1b[0m\n", 34)
		fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
		commend := ""
		fmt.Scanln(&commend)
		commend = strings.ReplaceAll(commend, "，", ",")
		commend = strings.ReplaceAll(commend, "-", ",")
		commend = strings.ReplaceAll(commend, ".", ",")
		commend = strings.ReplaceAll(commend, "。", ",")
		commends := strings.Split(commend, ",")
		for _, commend = range commends {
			if commend == "" {
				continue
			}
			th.SetTableName(commend).
				SetSavePath(fmt.Sprintf("%s/tbl_%s/tbl_%s.go", savePrefix, commend, commend)).
				SetPackageInfo(commend, "tbl_", "").
				GenerateTblStruct()
		}
	}

}

func ExampleYamlToSqlHandler_ExecuteSchemaSafeCheck() {
	if exampleDSN() == "" {
		return
	}

	// 配置文件请参考 目录./cmd/test_yaml_to_sql/etc2/ 下的案例
	{
		yts := NewYamlToSqlHandler().SetYamlPath("./cmd/test_yaml_to_sql/etc2/").
			SetDsn(exampleDSN())
		yts.ExecuteSchemaSafeCheck()
	}

	// 配置文件请参考 目录./cmd/test_yaml_to_sql/etc/ 下的案例
	{
		yts := NewYamlToSqlHandler().SetYamlPath("./cmd/test_yaml_to_sql/etc/").
			SetDsn(exampleDSN())
		yts.ExecuteSchemaSafeCheck()
	}

}

// ---------------------------------------------------------------------------
// yml -> 表结构同步：按能力逐个给案例
// ---------------------------------------------------------------------------

// 案例：最常用的入口——把 yml 声明的结构同步到数据库。
//
// 建表、加字段、改类型、改注释、改可空性与默认值、增删改索引、改主键，
// 全部由 ExecuteSchema 一次完成，使用者不需要自己判断“库里现在是什么状态”：
// 它会先把 yml 解析成结构描述，再去 information_schema 把库里的真实结构拉回来，
// 两边比对之后只针对差异生成 SQL。
//
// 同一份配置反复执行是安全的：结构与 yml 一致时不会生成任何 SQL（幂等）。
func ExampleYamlToSqlHandler_ExecuteSchema() {
	if exampleDSN() == "" {
		return
	}

	NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/demo_create/").
		ExecuteSchema()
}

// 案例：项目里已经有一个全局 *gorm.DB，直接复用，不让本库再建一条连接。
//
// SetDsn 与 SetDB 二选一，两个都调时以 SetDB 为准。
// 连接池、超时、日志级别这些配置跟着项目自己的 db 走，也更好统一。
func ExampleYamlToSqlHandler_SetDB() {
	if exampleDSN() == "" {
		return
	}

	db, err := gorm.Open(mysql.Open(exampleDSN()), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	NewYamlToSqlHandler().
		SetDB(db).
		SetYamlPath("./cmd/test_schema_cases/etc/demo_create/").
		ExecuteSchema()
}

// 案例：上线前先看清楚“到底要动什么”，一条 SQL 都不执行。
//
// SetDryRun(true) 之后，无论调 ExecuteSchema 还是 ExecuteSchemaSafeCheck 都只生成 SQL；
// 再配一个 SetSqlExportPath，就能把语句写成文件交给 DBA 审核。
// 这是大表上线前最推荐的用法：先看报告、再看 SQL、最后才真执行。
func ExampleYamlToSqlHandler_SetDryRun() {
	if exampleDSN() == "" {
		return
	}

	h := NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/demo_add_column/").
		SetDryRun(true).                                            // 只生成不执行
		SetSqlExportPath(filepath.Join(os.TempDir(), "schema.sql")) // 导出给 DBA
	h.ExecuteSchemaSafeCheck()

	for _, c := range h.GetChangeReport() {
		fmt.Printf("%s %s %s：%s\n", c.Table, c.Kind, c.Object, c.Reason)
	}
	for _, s := range h.GetSql() {
		fmt.Println(s)
	}
}

// 案例：把变更报告接到自己的发布流程里，遇到破坏性变更先拦一道。
//
// GetChangeReport 返回的是结构化的 SchemaChange，不是一堆字符串，所以能直接拿来判定：
//   - Dangerous：删列、删索引、改主键这类会丢数据/锁表的操作；
//   - Skipped：确实有差异，但按当前策略（如 DropPolicyNever、字符集未开启同步）不执行，
//     这类记录一定不带 SQL。
//
// 确认没有高危项后再放行：这里直接复用刚才预览算好的 SQL，不会重新比对一次；
// DoSql 执行前会要求输入 Y 确认，不需要交互就用 SetDryRun(false).ExecuteSchema()。
func ExampleYamlToSqlHandler_GetChangeReport() {
	if exampleDSN() == "" {
		return
	}

	h := NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/demo_drop_column/").
		SetDryRun(true)
	h.ExecuteSchemaSafeCheck()

	var dangerous int
	for _, c := range h.GetChangeReport() {
		switch {
		case c.Skipped:
			fmt.Printf("[跳过] %s %s:%s %s\n", c.Table, c.Kind, c.Object, c.Reason)
		case c.Dangerous:
			dangerous++
			fmt.Printf("[高危] %s %s:%s %s\n", c.Table, c.Kind, c.Object, c.Reason)
		default:
			fmt.Printf("[普通] %s %s:%s %s\n", c.Table, c.Kind, c.Object, c.Reason)
		}
	}
	if dangerous == 0 {
		h.SetDryRun(false).DoSql()
	}
}

// 案例：yml 里删掉的字段/索引，只报告不删。
//
// 默认是 DropPolicyAlways：yml 就是唯一事实来源，
// 库里多出来的字段会被 DROP。但一张表被多个服务共用时，
// 自己少写一个字段不代表就应该把它删掉，这时用 DropPolicyNever：
// 差异照样进变更报告（标记为 Skipped），但不会生成 DROP 语句。
func ExampleYamlToSqlHandler_SetDropPolicy() {
	if exampleDSN() == "" {
		return
	}

	NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/keep_drop_less/").
		SetDropPolicy(DropPolicyNever).
		SetDryRun(true).
		ExecuteSchemaSafeCheck()
}

// 案例：表字符集/排序规则漂移。
//
// 库里表的 collate 与 yml 不一致时，默认只把它写进变更报告（标记为 Skipped），
// 因为 ALTER TABLE ... CONVERT TO CHARACTER SET 会重写整表数据，
// 大表上属于高危操作，必须使用者显式打开 SetSyncTableCharset(true) 才真的执行。
func ExampleYamlToSqlHandler_SetSyncTableCharset() {
	if exampleDSN() == "" {
		return
	}

	NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/charset_drift/").
		SetSyncTableCharset(true).
		ExecuteSchema()
}

// 案例：只同步部分表。
//
// SetTableFilter 与 SetTableExclude 都支持 * ? 通配符，比对的是 yml 里声明的表名。
// 两者可以同时用：先按 filter 选中，再按 exclude 剔除。
// 这在灰度发布时很有用：新表先上线，存量表下一轮再动。
//
// 默认只扫描 YamlPath 本层的配置文件；配置按模块分了子目录时，
// 加上 SetRecursive(true) 就会连子目录一起扫。
func ExampleYamlToSqlHandler_SetTableFilter() {
	if exampleDSN() == "" {
		return
	}

	h := NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_yaml_to_sql/etc/").
		SetTableFilter("plf_tbl_user*").   // 只同步 plf_tbl_user 开头的表
		SetTableExclude("plf_tbl_user_4"). // 但排除这一张
		SetDryRun(true)
	h.ExecuteSchemaSafeCheck()

	fmt.Println("本次参与同步的表：", h.GetTables())
}

// 案例：配置写错了，告警但不中断。
//
// 配置项拼错（engin / defalut）、default 写了键但没给值、分词器不是 MySQL 内置的……
// 这些都是“很可能是因为写错了，但不影响建表”的情况：
// 硬报错会把在跑的业务弄挂，静默忽略又要到数据库才暴露，所以统一进 GetWarnings。
//
// 注意区分：真正会让表建不出来的错误（索引引用了不存在的列、缺少 Table 节点、
// 字段没写 type、类型括号没闭合、enum/set 没写取值列表、同一层重复定义配置项）
// 是在解析阶段直接终止并指出文件/表/字段位置的，不会拖到数据库报错。
func ExampleYamlToSqlHandler_GetWarnings() {
	if exampleDSN() == "" {
		return
	}

	h := NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/warn_typo/").
		SetDryRun(true)
	h.ExecuteSchemaSafeCheck()

	for _, w := range h.GetWarnings() {
		fmt.Println("配置告警：", w)
	}
}

// 案例：大批量变更中断后接着跑，不重复执行已经成功的语句。
//
// DDL 无法回滚：执行到一半报错时，前面的语句已经生效了。
// 开启 SetMigrationHistory 后，每条执行成功的语句会按指纹记到历史表里，
// 修复配置后重跑会自动跳过它们。默认关闭（历史表名可自定义）。
func ExampleYamlToSqlHandler_SetMigrationHistory() {
	if exampleDSN() == "" {
		return
	}

	NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/demo_create/").
		SetMigrationHistory(true, "ds_migration_history").
		ExecuteSchema()
}

// 案例：只发布编译产物、不发布 yml，并在启动时自检线上结构。
//
// 第一步（构建阶段）：把 yml 编译成一份产物文件。
// 产物里保留了字段的书写顺序与标量原文（'007'、'1.10' 这类默认值不会被当成数字改写），
// 需要防篡改时可以传 encry=true 与密钥把它加密。
func ExampleYamlToSqlHandler_SetIsOutputBuildSchema() {
	if exampleDSN() == "" {
		return
	}

	dest := filepath.Join(os.TempDir(), "ds_case_build.value")

	NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/build_create/").
		SetIsOutputBuildSchema(true, false, ""). // 输出产物、不加密
		SetBuildSchemaDest(dest).
		ExecuteSchema()
}

// 第二步（服务启动时）：从编译产物回读，确认线上库结构与发布物完全一致。
//
// VerifyIsCleanSchema 返回 false 就说明有人手工改过表，或者发布物与库对不上；
// 把它接在启动流程里，能让“配置与实现不一致”在启动阶段就暴露，
// 而不是等到线上查询报 Unknown column 才发现。
func ExampleYamlToSqlHandler_LoadSchema() {
	if exampleDSN() == "" {
		return
	}

	dest := filepath.Join(os.TempDir(), "ds_case_build.value")

	h := NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetBuildSchemaDest(dest) // 只给产物路径，不再读 yml
	h.LoadSchema()

	if h.VerifyIsCleanSchema() {
		fmt.Println("线上表结构与发布物一致")
		return
	}
	fmt.Println("检测到结构差异：")
	for _, c := range h.GetChangeReport() {
		fmt.Printf("  %s %s:%s %s\n", c.Table, c.Kind, c.Object, c.Reason)
	}
}

// 案例：一份定义展开成多张分表。
//
// yml 里写了 sharding_tables 时，建表、比对、变更全部针对展开后的物理表（ds_case_shard_01/02），
// 声明名本身不会被建成表；后续改字段也会同时落到每一张分表上。
func ExampleYamlToSqlHandler_ExecuteSchema_sharding() {
	if exampleDSN() == "" {
		return
	}

	h := NewYamlToSqlHandler().
		SetDsn(exampleDSN()).
		SetYamlPath("./cmd/test_schema_cases/etc/shard_create/").
		SetDryRun(true)
	h.ExecuteSchemaSafeCheck()

	fmt.Println("展开后的物理表：", h.GetTables())
}
