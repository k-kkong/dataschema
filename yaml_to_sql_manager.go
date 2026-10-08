package dataschema

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/k-kkong/dataschema/information_schema"
	"github.com/tidwall/gjson"
)

// yml 中删掉的字段与索引该如何处理
const (
	// DropPolicyAlways 默认值：yml 里删掉的字段/索引，数据库里同步 DROP。
	// 不希望某个字段被删除时，在 yml 中保留它的定义即可。
	DropPolicyAlways = "always"
	// DropPolicyNever 从不执行 DROP，只在变更报告中提示差异
	DropPolicyNever = "never"
)

// DefaultMigrationTable 结构变更执行记录表的默认表名
const DefaultMigrationTable = "dataschema_migrations"

type YamlToSqlHandler struct {
	IsOutputBuildSchema      bool   // 是否输出编译后的结构
	IsEncryOutputBuildSchema bool   // 是否加密 编译后的结构
	EncryKey                 string // 加密key
	BuildSchemaDest          string //

	dsn string   //数据库连接dsn,列：用户:密码@(127.0.0.1:3306)/数据库?charset=utf8mb4&parseTime=True&loc=Local
	db  *gorm.DB //数据库连接

	YamlPath          string //yaml文件路径
	yamlFileFullPaths []string

	// schemas/results/sql 三者长度始终一致、下标一一对应。
	// 配置了 sharding_tables 时一份定义会展开成多张物理表，
	// 三个切片必须同步 append，否则下标错位会打印错表名甚至数组越界。
	schemas []*ymlTable
	results []tableResult
	sql     []string

	states   map[string]*dbTableState // 表名 -> 数据库现状
	changes  []SchemaChange           // 全部变更记录，含被跳过的
	warnings []string                 // 解析阶段的告警

	recursive         bool     // 是否递归扫描子目录
	tableInclude      []string // 只同步匹配的表，空表示全部
	tableExclude      []string // 排除匹配的表
	dropPolicy        string   // 见 DropPolicy* 常量
	keepColumnOrder   bool     // 新增列时是否按 yml 顺序落位
	syncCharset       bool     // 是否同步表字符集
	dryRun            bool     // 只生成不执行
	sqlExportPath     string   // 把生成的 SQL 导出到文件
	migrationHistory  bool     // 是否记录已执行的 SQL
	migrationTable    string   // 执行记录表名
	executedSqlHashes map[string]bool
	failedSQL         string // 执行失败的 SQL
}

// tableResult 一张表的比对结果
type tableResult struct {
	table   string
	source  string
	sql     string
	changes []SchemaChange
}

// NewYamlToSqlHandler 创建表结构维护器
func NewYamlToSqlHandler() *YamlToSqlHandler {
	return &YamlToSqlHandler{
		IsOutputBuildSchema:      false,
		IsEncryOutputBuildSchema: false,
		BuildSchemaDest:          "./dataschema.value",
		dropPolicy:               DropPolicyAlways,
		keepColumnOrder:          true,
		migrationTable:           DefaultMigrationTable,
	}
}

// SetDsn 设置数据库连接
func (ts *YamlToSqlHandler) SetDsn(dsn string) *YamlToSqlHandler {
	ts.dsn = dsn
	return ts
}

// SetDB 设置数据库连接
func (ts *YamlToSqlHandler) SetDB(db *gorm.DB) *YamlToSqlHandler {
	ts.db = db
	return ts
}

func (ts *YamlToSqlHandler) connectSql() {
	if ts.db == nil {
		if ts.dsn == "" {
			panic("数据库连接不能为空")
		}
		var configs = &gorm.Config{}
		db, err := gorm.Open(mysql.Open(ts.dsn), configs)
		if err != nil {
			panic(err)
		}
		ts.db = db
	}
}

// SetYamlPath 设置yaml配置文件路径
func (ts *YamlToSqlHandler) SetYamlPath(yamlPath string) *YamlToSqlHandler {
	ts.YamlPath = yamlPath
	return ts
}

// SetIsOutputBuildSchema 设置是否输出编译后的结构 用于加密或者强制发布前检查
func (ts *YamlToSqlHandler) SetIsOutputBuildSchema(value bool, encry bool, key string) *YamlToSqlHandler {
	ts.IsOutputBuildSchema = value
	ts.IsEncryOutputBuildSchema = encry
	ts.EncryKey = key
	return ts
}

// SetBuildSchemaDest 设置编译后的文件路径
func (ts *YamlToSqlHandler) SetBuildSchemaDest(dest string) *YamlToSqlHandler {
	ts.BuildSchemaDest = dest
	return ts
}

// SetRecursive 设置是否递归扫描子目录下的配置文件，默认只扫描 YamlPath 本层
func (ts *YamlToSqlHandler) SetRecursive(recursive bool) *YamlToSqlHandler {
	ts.recursive = recursive
	return ts
}

// SetTableFilter 设置只同步哪些表，支持 * ? 通配符；不设置表示全部同步
func (ts *YamlToSqlHandler) SetTableFilter(patterns ...string) *YamlToSqlHandler {
	ts.tableInclude = patterns
	return ts
}

// SetTableExclude 设置排除哪些表，支持 * ? 通配符
func (ts *YamlToSqlHandler) SetTableExclude(patterns ...string) *YamlToSqlHandler {
	ts.tableExclude = patterns
	return ts
}

// SetDropPolicy 设置 yml 中删掉的字段/索引如何处理。
// 默认 DropPolicyAlways；设为 DropPolicyNever 则只报告不删除。
func (ts *YamlToSqlHandler) SetDropPolicy(policy string) *YamlToSqlHandler {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case DropPolicyNever:
		ts.dropPolicy = DropPolicyNever
	default:
		ts.dropPolicy = DropPolicyAlways
	}
	return ts
}

// SetKeepColumnOrder 设置新增列时是否按 yml 声明的顺序落位（生成 AFTER/FIRST）。
// 默认开启；已存在的列不会被重排，因为重排会触发整表重建。
func (ts *YamlToSqlHandler) SetKeepColumnOrder(keep bool) *YamlToSqlHandler {
	ts.keepColumnOrder = keep
	return ts
}

// SetSyncTableCharset 设置表字符集/排序规则与 yml 不一致时是否真的执行同步。
// 默认关闭（只在报告中提示），因为 CONVERT TO CHARACTER SET 会重写整表数据，
// 大表上属于高危操作，需要使用者显式开启。
func (ts *YamlToSqlHandler) SetSyncTableCharset(sync bool) *YamlToSqlHandler {
	ts.syncCharset = sync
	return ts
}

// SetDryRun 设置只生成 SQL 不执行，配合 GetSql/GetChangeReport 做发布前检查
func (ts *YamlToSqlHandler) SetDryRun(dryRun bool) *YamlToSqlHandler {
	ts.dryRun = dryRun
	return ts
}

// SetSqlExportPath 设置把生成的 SQL 导出到文件，便于走 DBA 审核流程
func (ts *YamlToSqlHandler) SetSqlExportPath(dest string) *YamlToSqlHandler {
	ts.sqlExportPath = dest
	return ts
}

// SetMigrationHistory 设置是否记录已执行过的 SQL（按语句指纹去重）。
// 开启后重复执行会自动跳过已成功的语句，失败重跑时具备断点续跑能力。默认关闭。
func (ts *YamlToSqlHandler) SetMigrationHistory(enable bool, tableName string) *YamlToSqlHandler {
	ts.migrationHistory = enable
	if strings.TrimSpace(tableName) != "" {
		ts.migrationTable = strings.TrimSpace(tableName)
	}
	return ts
}

// GetSql 获取需要执行的sql
func (ts *YamlToSqlHandler) GetSql() []string {
	return ts.sql
}

// GetChangeReport 获取结构变更记录，包含因 DropPolicy 等原因被跳过的项
func (ts *YamlToSqlHandler) GetChangeReport() []SchemaChange {
	return ts.changes
}

// GetWarnings 获取配置文件解析阶段的告警
func (ts *YamlToSqlHandler) GetWarnings() []string {
	return ts.warnings
}

// GetFailedSql 获取执行失败的 SQL
func (ts *YamlToSqlHandler) GetFailedSql() string {
	return ts.failedSQL
}

// GetTables 获取本次参与同步的表名
func (ts *YamlToSqlHandler) GetTables() []string {
	names := make([]string, 0, len(ts.schemas))
	for _, t := range ts.schemas {
		names = append(names, t.Name)
	}
	return names
}

func (ts *YamlToSqlHandler) getyamlFileFullPaths() *YamlToSqlHandler {
	ts.yamlFileFullPaths = nil
	// 递归遍历需要用 var 先声明，否则函数字面量里引用不到自己
	var walk func(dir string) error
	walk = func(dir string) error {
		files, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			full := filepath.Join(dir, f.Name())
			if f.IsDir() {
				if ts.recursive {
					if err := walk(full); err != nil {
						return err
					}
				}
				continue
			}
			lower := strings.ToLower(f.Name())
			// 按后缀精确匹配，.yml 与 .yaml 都认，
			// a.yml.bak 这类备份文件不会被误当成配置读进来。
			if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
				continue
			}
			ts.yamlFileFullPaths = append(ts.yamlFileFullPaths, full)
		}
		return nil
	}
	// 读取目录失败直接终止：继续往下跑只会得到一份空的表清单，
	// 最终报出来的错误与真实原因无关，排查成本很高。
	if err := walk(ts.YamlPath); err != nil {
		fmt.Printf("\x1b[%dm 读取配置文件目录 %s 失败: %s \x1b[0m\n", 31, ts.YamlPath, err.Error())
		panic(fmt.Sprintf("读取配置文件目录 %s 失败: %s", ts.YamlPath, err.Error()))
	}
	sort.Strings(ts.yamlFileFullPaths)
	return ts
}

// parsedDoc 一份配置文件解析出来的结果
type parsedDoc struct {
	table  *ymlTable
	source string
}

func (ts *YamlToSqlHandler) getYamlDatas() *YamlToSqlHandler {
	docs := ts.parseYamlFiles(ts.yamlFileFullPaths)
	ts.collectTables(docs)
	return ts
}

// parseYamlFiles 解析配置文件并输出编译产物
func (ts *YamlToSqlHandler) parseYamlFiles(paths []string) []parsedDoc {
	docs := make([]parsedDoc, 0, len(paths))
	declared := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			ts.failParse(p, err)
		}
		docJSON, err := yamlFileToOrderedJSON(data)
		if err != nil {
			ts.failParse(p, err)
		}
		tbl, warns, err := parseTableDoc(docJSON, p)
		if err != nil {
			ts.failParse(p, err)
		}
		for _, w := range warns {
			ts.warnings = append(ts.warnings, fmt.Sprintf("%s: %s", p, w))
		}
		if prev, ok := declared[tbl.DeclaredName]; ok {
			fmt.Printf("\x1b[%dm配置文件: %s 序列化失败，重复定义的表（已存在于 %s） \x1b[0m\n", 31, p, prev)
			panic(fmt.Sprintf("\x1b[%dm配置文件: %s 序列化失败\x1b[0m\n", 31, p))
		}
		declared[tbl.DeclaredName] = p
		docs = append(docs, parsedDoc{table: tbl, source: p})
	}
	if ts.IsOutputBuildSchema {
		ts.writeBuildSchema(docs)
	}
	return docs
}

func (ts *YamlToSqlHandler) failParse(p string, err error) {
	fmt.Printf("\x1b[%dm配置文件: %s 序列化失败: %s\x1b[0m\n", 31, p, err.Error())
	panic(fmt.Sprintf("\x1b[%dm配置文件: %s 序列化失败: %s\x1b[0m\n", 31, p, err.Error()))
}

// writeBuildSchema 输出编译产物。
// 顶层形状是 {"表名": 文档JSON}，loadFromBuildSchema 按这个形状回读；
// 文档内部保留了 yml 的书写顺序，所以字段顺序可以稳定还原。
func (ts *YamlToSqlHandler) writeBuildSchema(docs []parsedDoc) {
	sorted := make([]*ymlTable, 0, len(docs))
	for _, d := range docs {
		sorted = append(sorted, d.table)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].DeclaredName < sorted[j].DeclaredName })

	content, err := buildSchemaJSON(sorted)
	if err != nil {
		fmt.Printf("\x1b[%dm 序列化编译产物失败 \x1b[0m\n", 31)
		panic(fmt.Sprintf("\x1b[%dm 序列化编译产物失败 \x1b[0m\n", 31))
	}
	if ts.IsEncryOutputBuildSchema {
		content, err = EncryptString(content, []byte(ts.EncryKey))
		if err != nil {
			fmt.Printf("\x1b[%dm 序列化编译产物加密失败: %s \x1b[0m\n", 31, err.Error())
			panic(fmt.Sprintf("\x1b[%dm 序列化编译产物加密失败: %s \x1b[0m\n", 31, err.Error()))
		}
	}
	if dir := path.Dir(ts.BuildSchemaDest); dir != "" {
		os.MkdirAll(dir, os.ModePerm)
	}
	file, err := os.Create(ts.BuildSchemaDest)
	if err != nil {
		fmt.Printf("\x1b[%dm 序列化产物写入失败 \x1b[0m\n", 31)
		panic(fmt.Sprintf("\x1b[%dm 序列化产物写入失败 %s \x1b[0m\n", 31, err.Error()))
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if _, err = fmt.Fprint(writer, content); err != nil {
		fmt.Printf("\x1b[%dm 序列化编译产物写入失败 %s\x1b[0m\n", 31, err.Error())
		panic(fmt.Sprintf("\x1b[%dm 序列化编译产物写入失败 %s \x1b[0m\n", 31, err.Error()))
	}
	if err = writer.Flush(); err != nil {
		fmt.Printf("\x1b[%dm 序列化编译产物写入失败 %s\x1b[0m\n", 31, err.Error())
		panic(fmt.Sprintf("\x1b[%dm 序列化编译产物写入失败 %s \x1b[0m\n", 31, err.Error()))
	}
}

// collectTables 展开分表并套用表名过滤，填充 schemas
func (ts *YamlToSqlHandler) collectTables(docs []parsedDoc) {
	ts.schemas = nil
	seen := map[string]string{}
	for _, d := range docs {
		for _, tbl := range d.table.expandSharding() {
			if !ts.matchTable(tbl.Name) {
				continue
			}
			if prev, ok := seen[tbl.Name]; ok {
				fmt.Printf("\x1b[%dm配置文件: %s 序列化失败，重复定义的表 %s（已存在于 %s） \x1b[0m\n", 31, d.source, tbl.Name, prev)
				panic(fmt.Sprintf("\x1b[%dm配置文件: %s 序列化失败\x1b[0m\n", 31, d.source))
			}
			seen[tbl.Name] = d.source
			ts.schemas = append(ts.schemas, tbl)
		}
	}
}

// matchTable 判断表名是否落在过滤条件内
func (ts *YamlToSqlHandler) matchTable(name string) bool {
	matchAny := func(patterns []string) bool {
		for _, p := range patterns {
			if p == "" {
				continue
			}
			if p == name {
				return true
			}
			if ok, err := path.Match(p, name); err == nil && ok {
				return true
			}
		}
		return false
	}
	if len(ts.tableExclude) > 0 && matchAny(ts.tableExclude) {
		return false
	}
	if len(ts.tableInclude) > 0 {
		return matchAny(ts.tableInclude)
	}
	return true
}

func (ts *YamlToSqlHandler) loadFromBuildSchema() *YamlToSqlHandler {
	bvalue, err := os.ReadFile(ts.BuildSchemaDest)
	if err != nil {
		fmt.Printf("\x1b[%dm 序列化编译产物读取失败 \x1b[0m\n", 31)
		panic(fmt.Sprintf("\x1b[%dm 序列化编译产物读取失败 \x1b[0m\n", 31))
	}

	bvaluestr := string(bvalue)
	if ts.IsEncryOutputBuildSchema {
		bvaluestr, err = DecryptString(bvaluestr, []byte(ts.EncryKey))
		if err != nil {
			fmt.Printf("\x1b[%dm 序列化编译产物解密失败 \x1b[0m\n", 31)
			panic(fmt.Sprintf("\x1b[%dm 序列化编译产物解密失败 \x1b[0m\n", 31))
		}
	}
	root := gjson.Parse(bvaluestr)
	if !root.IsObject() {
		fmt.Printf("\x1b[%dm 序列化编译产物格式不正确 \x1b[0m\n", 31)
		panic("序列化编译产物格式不正确")
	}

	type docItem struct {
		name string
		raw  string
	}
	var items []docItem
	root.ForEach(func(key, value gjson.Result) bool {
		items = append(items, docItem{name: key.String(), raw: value.Raw})
		return true
	})
	// gjson 遍历对象的顺序取决于产物文本本身的键顺序，
	// 这里显式按表名排序，让处理顺序与输出都稳定可复现。
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })

	docs := make([]parsedDoc, 0, len(items))
	for _, item := range items {
		tbl, warns, err := parseTableDoc(item.raw, ts.BuildSchemaDest)
		if err != nil {
			fmt.Printf("\x1b[%dm 序列化编译产物中的表 %s 解析失败: %s \x1b[0m\n", 31, item.name, err.Error())
			panic(fmt.Sprintf("\x1b[%dm 序列化编译产物中的表 %s 解析失败\x1b[0m\n", 31, item.name))
		}
		for _, w := range warns {
			ts.warnings = append(ts.warnings, fmt.Sprintf("%s: %s", item.name, w))
		}
		docs = append(docs, parsedDoc{table: tbl, source: ts.BuildSchemaDest})
	}
	ts.collectTables(docs)
	return ts
}

// loadDBSnapshot 一次性把本次涉及的表的现状全部读出来。
// 三条查询覆盖本次涉及的全部表（TABLES / COLUMNS / STATISTICS），往返次数与表的数量无关。
func (ts *YamlToSqlHandler) loadDBSnapshot() *YamlToSqlHandler {
	ts.states = map[string]*dbTableState{}
	names := make([]string, 0, len(ts.schemas))
	for _, t := range ts.schemas {
		names = append(names, t.Name)
	}
	if len(names) == 0 {
		return ts
	}
	stateOf := func(name string) *dbTableState {
		st, ok := ts.states[name]
		if !ok {
			st = newDBTableState(name)
			ts.states[name] = st
		}
		return st
	}

	var tables []information_schema.SqlTable
	ts.db.Table("INFORMATION_SCHEMA.TABLES").
		Select("TABLE_NAME,TABLE_COMMENT,TABLE_COLLATION").
		Where("TABLE_SCHEMA=database()").
		Where("TABLE_NAME IN ?", names).
		Find(&tables)
	for _, tb := range tables {
		st := stateOf(tb.TableName)
		st.Exists = true
		st.Comment = tb.TableComment
		st.Collation = tb.TableCollation
	}

	var columns []information_schema.SqlTableColumns
	ts.db.Table("`INFORMATION_SCHEMA`.`COLUMNS`").
		Where("TABLE_SCHEMA=database()").
		Where("TABLE_NAME IN ?", names).
		Order("TABLE_NAME, ORDINAL_POSITION").
		Find(&columns)
	// 已按 TABLE_NAME, ORDINAL_POSITION 排序，这里换成每张表各自的序号
	lastTable, pos := "", 0
	for _, sc := range columns {
		if sc.TableName != lastTable {
			lastTable, pos = sc.TableName, 0
		}
		col := &dbColumn{
			Name:     sc.ColumnName,
			Decl:     getTypeYml2SqlMapping(sc.ColumnType),
			Nullable: strings.ToLower(sc.IsNullable) == "yes",
			Comment:  sc.ColumnComment,
			Extra:    sc.Extra,
			Position: pos,
		}
		pos++
		if sc.ColumnDefault != nil {
			col.DefaultSet = true
			col.Default = *sc.ColumnDefault
		}
		stateOf(sc.TableName).addColumn(col)
	}

	var indexes []information_schema.SqlIndexes
	ts.db.Table("`INFORMATION_SCHEMA`.`STATISTICS`").
		Select("TABLE_NAME AS Table_name, NON_UNIQUE AS Non_unique, INDEX_NAME AS Key_name, "+
			"SEQ_IN_INDEX AS Seq_in_index, COLUMN_NAME AS Column_name, INDEX_TYPE AS Index_type").
		Where("TABLE_SCHEMA=database()").
		Where("TABLE_NAME IN ?", names).
		Order("TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX").
		Find(&indexes)
	for _, si := range indexes {
		stateOf(si.Table_name).addIndexColumn(si.Key_name, si.Column_name, si.IndexType, si.Non_unique)
	}
	return ts
}

func (ts *YamlToSqlHandler) doSchema() *YamlToSqlHandler {
	opt := diffOption{
		dropPolicy:      ts.dropPolicy,
		keepColumnOrder: ts.keepColumnOrder,
		syncCharset:     ts.syncCharset,
	}
	ts.results = nil
	ts.sql = nil
	ts.changes = nil
	for _, t := range ts.schemas {
		st, ok := ts.states[t.Name]
		if !ok {
			st = newDBTableState(t.Name)
		}
		changes, sqlText, err := diffTable(t, st, opt)
		if err != nil {
			fmt.Printf("\x1b[%dm 文件: %s 表: %s 不正确: %s\x1b[0m\n", 31, t.SourceFile, t.Name, err.Error())
			panic("配置文件不正确")
		}
		ts.results = append(ts.results, tableResult{table: t.Name, source: t.SourceFile, sql: sqlText, changes: changes})
		ts.sql = append(ts.sql, sqlText)
		ts.changes = append(ts.changes, changes...)
	}
	return ts
}

// verifyYmlFile 校验yml的合法行。
// 字段级与索引级的校验已经前移到解析阶段（parseTableDoc），
// 这里做需要看到全部表才能做的检查，并统一输出解析告警。
func (ts *YamlToSqlHandler) verifyYmlFile() *YamlToSqlHandler {
	for _, w := range ts.warnings {
		fmt.Printf("\x1b[%dm[告警] %s\x1b[0m\n", 33, w)
	}
	return ts
}

// splitSqlStatements 按分号切分语句，忽略字符串字面量与反引号标识符内部的分号。
// 字符串字面量与反引号标识符内部的分号不算分隔符，comment 或 default 里带分号也不会把语句切碎。
func splitSqlStatements(sqlText string) []string {
	var (
		out   []string
		cur   strings.Builder
		rune_ = []rune(sqlText)
	)
	for i := 0; i < len(rune_); i++ {
		ch := rune_[i]
		switch ch {
		case '\'', '`':
			quote := ch
			cur.WriteRune(ch)
			i++
			for ; i < len(rune_); i++ {
				c := rune_[i]
				cur.WriteRune(c)
				if quote == '\'' && c == '\\' && i+1 < len(rune_) {
					i++
					cur.WriteRune(rune_[i])
					continue
				}
				if c == quote {
					// '' 与 `` 是转义写法，需要再吃一个
					if i+1 < len(rune_) && rune_[i+1] == quote {
						i++
						cur.WriteRune(rune_[i])
						continue
					}
					break
				}
			}
		case ';':
			if stmt := strings.TrimSpace(cur.String()); stmt != "" {
				out = append(out, stmt)
			}
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	if stmt := strings.TrimSpace(cur.String()); stmt != "" {
		out = append(out, stmt)
	}
	return out
}

// hasRealChange 判断一段 SQL 文本里是否真的有需要执行的内容
func hasRealChange(sqlText string) bool {
	return len(splitSqlStatements(sqlText)) > 0
}

func (ts *YamlToSqlHandler) printChangeReport() {
	fmt.Printf("\x1b[%dm您将要执行的结构操作为： \x1b[0m\n", 34)
	var danger, skipped, effective int
	for _, r := range ts.results {
		if !hasRealChange(r.sql) && !hasReportableChange(r.changes) {
			continue
		}
		fmt.Printf(">>>>>>>>>>>>> %s (%s) >>>>>>>>>>>>>\n", r.table, r.source)
		for _, c := range r.changes {
			switch {
			case c.Skipped:
				skipped++
				fmt.Printf("\x1b[%dm[跳过] %s %s：%s\x1b[0m\n", 90, c.Kind, c.Object, c.Reason)
			default:
				effective++
				if c.Dangerous {
					danger++
				}
				label := fmt.Sprintf("[%s] %s", c.Kind, c.Object)
				if c.Reason != "" {
					label = fmt.Sprintf("%s：%s", label, c.Reason)
				}
				if c.Dangerous {
					fmt.Printf("\x1b[%dm%s\x1b[0m\n", 31, label)
				} else {
					fmt.Printf("\x1b[%dm%s\x1b[0m\n", 36, label)
				}
				if c.SQL != "" {
					fmt.Printf("\x1b[%dm%s \x1b[0m\n", 33, c.SQL)
				}
			}
		}
		fmt.Println("<<<<<<<<<<<<<", r.table, "<<<<<<<<<<<<<")
	}
	fmt.Printf("\x1b[%dm共 %d 张表，%d 条变更待执行，%d 条破坏性变更，%d 条已跳过 \x1b[0m\n",
		34, len(ts.results), effective, danger, skipped)
}

// hasReportableChange 是否有值得展示的变更（包括被跳过的）
func hasReportableChange(changes []SchemaChange) bool {
	return len(changes) > 0
}

// exportSql 把生成的 SQL 写到文件，便于走审核流程
func (ts *YamlToSqlHandler) exportSql() {
	if ts.sqlExportPath == "" {
		return
	}
	if dir := path.Dir(ts.sqlExportPath); dir != "" {
		os.MkdirAll(dir, os.ModePerm)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("-- dataschema 生成于 %s\n", time.Now().Format("2006-01-02 15:04:05")))
	for _, r := range ts.results {
		stmts := splitSqlStatements(r.sql)
		if len(stmts) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("\n-- >>> %s (%s)\n", r.table, r.source))
		for _, s := range stmts {
			b.WriteString(s)
			b.WriteString(";\n")
		}
		b.WriteString(fmt.Sprintf("-- <<< %s\n", r.table))
	}
	if err := os.WriteFile(ts.sqlExportPath, []byte(b.String()), 0o644); err != nil {
		fmt.Printf("\x1b[%dm SQL 导出失败: %s \x1b[0m\n", 31, err.Error())
		panic(fmt.Sprintf("\x1b[%dm SQL 导出失败: %s \x1b[0m\n", 31, err.Error()))
	}
	fmt.Printf("\x1b[%dmSQL 已导出到 %s \x1b[0m\n", 36, ts.sqlExportPath)
}

// sqlFingerprint 计算语句指纹，用于执行记录去重
func sqlFingerprint(stmt string) string {
	normalized := strings.Join(strings.Fields(stmt), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func (ts *YamlToSqlHandler) ensureMigrationTable() {
	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n"+
		"\t`id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',\n"+
		"\t`fingerprint` char(64) NOT NULL COMMENT 'sql指纹',\n"+
		"\t`table_name` varchar(191) NOT NULL COMMENT '所属表',\n"+
		"\t`sql_text` text NOT NULL COMMENT '已执行的sql',\n"+
		"\t`executed_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '执行时间',\n"+
		"\tPRIMARY KEY(`id`),\n"+
		"\tUNIQUE INDEX `unq_fingerprint` (`fingerprint`)\n"+
		") DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci ENGINE = InnoDB COMMENT = 'dataschema结构变更记录' ;",
		quoteIdent(ts.migrationTable))
	if err := ts.db.Exec(ddl).Error; err != nil {
		fmt.Printf("\x1b[%dm 创建变更记录表失败: %s \x1b[0m\n", 31, err.Error())
		panic(err)
	}
	var records []struct {
		Fingerprint string `gorm:"column:fingerprint"`
	}
	ts.db.Table(quoteIdent(ts.migrationTable)).Select("fingerprint").Find(&records)
	ts.executedSqlHashes = make(map[string]bool, len(records))
	for _, r := range records {
		ts.executedSqlHashes[r.Fingerprint] = true
	}
}

func (ts *YamlToSqlHandler) recordMigration(stmt, tableName string) {
	if !ts.migrationHistory {
		return
	}
	err := ts.db.Exec(fmt.Sprintf("INSERT INTO %s (`fingerprint`,`table_name`,`sql_text`,`executed_at`) VALUES(?,?,?,?)",
		quoteIdent(ts.migrationTable)),
		sqlFingerprint(stmt), tableName, stmt, time.Now().Format("2006-01-02 15:04:05")).Error
	if err != nil {
		// 记录失败不应该影响已经成功的结构变更，只提示
		fmt.Printf("\x1b[%dm 写入变更记录失败: %s \x1b[0m\n", 31, err.Error())
	}
}

// executeSql 逐条执行 SQL。
//
// DDL 不放进事务：MySQL 的 DDL 会隐式提交，事务与 Rollback 对它完全无效，
// 包起来只会让人误以为失败后库还是干净的。
// 这里逐条执行、逐条打印进度，失败时明确指出是哪一条、前面已经成功了多少条，
// 配合 SetMigrationHistory 可以从断点继续跑。
func (ts *YamlToSqlHandler) executeSql() {
	ts.exportSql()
	if ts.dryRun {
		fmt.Printf("\x1b[%dmDryRun 模式，不执行任何 SQL。可通过 GetSql()/GetChangeReport() 获取结果 \x1b[0m\n", 36)
		return
	}
	if ts.migrationHistory {
		ts.ensureMigrationTable()
	}

	var total, done, skippedCount int
	for _, r := range ts.results {
		total += len(splitSqlStatements(r.sql))
	}
	for _, r := range ts.results {
		for _, stmt := range splitSqlStatements(r.sql) {
			exec := stmt + ";"
			if ts.migrationHistory && ts.executedSqlHashes[sqlFingerprint(stmt)] {
				skippedCount++
				fmt.Printf("\x1b[%dm[已执行过，跳过] %s\x1b[0m\n", 90, exec)
				continue
			}
			fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
			fmt.Printf("\x1b[%dm正在执行sql:\n%s \x1b[0m\n", 34, exec)
			fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
			if err := ts.db.Exec(exec).Error; err != nil {
				ts.failedSQL = exec
				fmt.Printf("\x1b[%dm执行sql:\n%s\n时出现错误（已成功执行 %d/%d 条，DDL 无法回滚，请修复后重新执行） \x1b[0m\n",
					31, exec, done, total)
				panic(err)
			}
			ts.recordMigration(stmt, r.table)
			done++
		}
	}
	if skippedCount > 0 {
		fmt.Printf("\x1b[%dm已跳过 %d 条历史执行过的语句 \x1b[0m\n", 36, skippedCount)
	}
	fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
	fmt.Printf("\x1b[%dmSQL更新完毕： \x1b[0m\n", 36)
	fmt.Printf("\x1b[%dm<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<： \x1b[0m\n", 34)
}

func (ts *YamlToSqlHandler) doSqlSafe() *YamlToSqlHandler {
	ts.printChangeReport()
	// DryRun 就是纯预览，不该再问确认，否则导出 SQL 还得先敲个 Y
	if ts.dryRun {
		ts.exportSql()
		fmt.Printf("\x1b[%dmDryRun 模式，不执行任何 SQL。可通过 GetSql()/GetChangeReport() 获取结果 \x1b[0m\n", 36)
		return ts
	}
	fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
	fmt.Printf("\x1b[%dm确认执行请输入[ Y ]： \x1b[0m\n", 34)
	fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
	commend := ""
	fmt.Scanln(&commend)
	if commend == "Y" || commend == "y" {
		ts.executeSql()
	}
	return ts
}

func (ts *YamlToSqlHandler) doSql() *YamlToSqlHandler {
	ts.printChangeReport()
	if ts.dryRun {
		ts.exportSql()
		fmt.Printf("\x1b[%dmDryRun 模式，不执行任何 SQL。可通过 GetSql()/GetChangeReport() 获取结果 \x1b[0m\n", 36)
		return ts
	}
	fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
	fmt.Printf("\x1b[%dm确认执行请输入[ Y ]： \x1b[0m\n", 34)
	fmt.Printf("\x1b[%dm>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>： \x1b[0m\n", 34)
	ts.executeSql()
	return ts
}

// trimSql 去掉没有任何变更的表，保持 sql 与 results 下标一致
func (ts *YamlToSqlHandler) trimSql() *YamlToSqlHandler {
	var (
		newsql     []string
		newresults []tableResult
	)
	for i, v := range ts.sql {
		if !hasRealChange(v) {
			continue
		}
		newsql = append(newsql, v)
		newresults = append(newresults, ts.results[i])
	}
	ts.sql = newsql
	ts.results = newresults
	return ts
}

// ExecuteSchemaSafeCheck 执行根据配置文件同步表结构(安全操作，允许使用者进一步确认)
func (ts *YamlToSqlHandler) ExecuteSchemaSafeCheck() *YamlToSqlHandler {
	ts.connectSql()
	ts.getyamlFileFullPaths().
		getYamlDatas().verifyYmlFile().loadDBSnapshot().doSchema().doSqlSafe()

	return ts
}

// ExecuteSchema 执行根据配置文件同步表结构
func (ts *YamlToSqlHandler) ExecuteSchema() *YamlToSqlHandler {
	ts.connectSql()
	ts.getyamlFileFullPaths().
		getYamlDatas().verifyYmlFile().loadDBSnapshot().doSchema().doSql()

	return ts
}

// LoadSchema 加载编译后的表结构配置信息
func (ts *YamlToSqlHandler) LoadSchema() *YamlToSqlHandler {
	ts.connectSql()
	ts.loadFromBuildSchema().verifyYmlFile().loadDBSnapshot().doSchema()
	return ts
}

// VerifyIsCleanSchema 检查是否有结构变动
func (ts *YamlToSqlHandler) VerifyIsCleanSchema() bool {
	ts.trimSql()
	return len(ts.sql) < 1
}

// DoSql 执行sql
func (ts *YamlToSqlHandler) DoSql() *YamlToSqlHandler {
	ts.doSqlSafe()
	return ts
}
