package dataschema

import (
	"fmt"
	"strings"
)

// 本文件负责"yml 声明的结构"与"数据库现状"的比对，产出结构化的变更记录与 SQL。
//
// 比对是逐项结构化进行的：列比类型/可空性/默认值/注释/自动生成标记，
// 索引比名字、类型（普通/唯一/全文）和列清单，主键比列与顺序。
// 索引不按 JSON 文本整体比较，这样 yml 里多写一个不参与建索引的配置项不会被当成差异，
// 同一份配置反复执行也不会把索引 drop 了再 create 一遍。
// 每一条变更都带着"为什么变"的原因，方便执行前人工确认。

// 变更类型，用于变更报告
const (
	ChangeCreateTable  = "建表"
	ChangeTableComment = "表注释"
	ChangeTableCharset = "表字符集"
	ChangeAddColumn    = "新增列"
	ChangeModifyColumn = "修改列"
	ChangeDropColumn   = "删除列"
	ChangeAddIndex     = "新增索引"
	ChangeRebuildIndex = "重建索引"
	ChangeDropIndex    = "删除索引"
	ChangePrimaryKey   = "主键"
)

// SchemaChange 一条结构变更记录
type SchemaChange struct {
	Table     string // 表名
	Kind      string // 变更类型，见 Change* 常量
	Object    string // 变更对象：列名或索引名，表级变更为空
	Reason    string // 变更原因
	SQL       string // 对应的 SQL，Skipped 为 true 时为空
	Dangerous bool   // 是否为破坏性变更（删列、删索引、改主键）
	Skipped   bool   // 检测到差异但按当前配置没有生成 SQL
}

// dbColumn 数据库里一列的现状
type dbColumn struct {
	Name       string
	Decl       string // 经过 getTypeYml2SqlMapping 处理的 COLUMN_TYPE
	Nullable   bool
	Default    string
	DefaultSet bool
	Comment    string
	Extra      string
	Position   int
}

// dbIndex 数据库里一条索引的现状
type dbIndex struct {
	Name     string
	Columns  []string
	Unique   bool
	Fulltext bool
	Primary  bool
	// Ignored 表示该索引类型不在维护范围内（例如空间索引），
	// 这类索引永远不会被本工具删除，只会在报告里提示。
	Ignored bool
}

// dbTableState 一张表在数据库里的现状
type dbTableState struct {
	Name      string
	Exists    bool
	Comment   string
	Collation string
	Columns   []*dbColumn
	columnMap map[string]*dbColumn
	Indexes   []*dbIndex
	indexMap  map[string]*dbIndex
	Primary   []string
}

func newDBTableState(name string) *dbTableState {
	return &dbTableState{
		Name:      name,
		columnMap: map[string]*dbColumn{},
		indexMap:  map[string]*dbIndex{},
	}
}

func (s *dbTableState) column(name string) (*dbColumn, bool) {
	c, ok := s.columnMap[name]
	return c, ok
}

func (s *dbTableState) index(name string) (*dbIndex, bool) {
	i, ok := s.indexMap[name]
	return i, ok
}

// addColumn 记录一列，保持数据库中的物理顺序
func (s *dbTableState) addColumn(c *dbColumn) {
	s.Columns = append(s.Columns, c)
	s.columnMap[c.Name] = c
}

// addIndexColumn 把一行索引信息累积到对应索引上
func (s *dbTableState) addIndexColumn(name, column, indexType string, nonUnique int) {
	idx, ok := s.indexMap[name]
	if !ok {
		idx = &dbIndex{Name: name}
		switch {
		case strings.EqualFold(name, "primary"):
			idx.Primary = true
			idx.Unique = true
		case strings.EqualFold(indexType, "fulltext"):
			idx.Fulltext = true
		case strings.EqualFold(indexType, "btree"):
			idx.Unique = nonUnique == 0
		default:
			// hash/spatial/rtree 等类型不参与维护
			idx.Ignored = true
		}
		s.indexMap[name] = idx
		s.Indexes = append(s.Indexes, idx)
	}
	idx.Columns = append(idx.Columns, column)
	if idx.Primary {
		s.Primary = append(s.Primary, column)
	}
}

// indexByName 按类型取出参与维护的索引
func (s *dbTableState) indexByName(name string, kind indexKind) (*dbIndex, bool) {
	idx, ok := s.indexMap[name]
	if !ok || idx.Ignored || idx.Primary {
		return nil, false
	}
	switch kind {
	case indexKindUnique:
		return idx, idx.Unique && !idx.Fulltext
	case indexKindFulltext:
		return idx, idx.Fulltext
	default:
		return idx, !idx.Unique && !idx.Fulltext
	}
}

// diffOption 比对行为的可调项
type diffOption struct {
	dropPolicy      string // 见 DropPolicy* 常量
	keepColumnOrder bool   // 新增列时是否按 yml 顺序落位（AFTER/FIRST）
	syncCharset     bool   // 是否同步表字符集
}

// diffTable 比对一张表，返回变更记录与该表对应的 SQL 文本
func diffTable(t *ymlTable, st *dbTableState, opt diffOption) ([]SchemaChange, string, error) {
	if !st.Exists {
		sql, err := buildCreateTableSQL(t)
		if err != nil {
			return nil, "", err
		}
		return []SchemaChange{{
			Table: t.Name,
			Kind:  ChangeCreateTable,
			SQL:   sql,
		}}, sql, nil
	}

	var (
		changes []SchemaChange
		// 表级 SQL 以换行开头，没有任何变更时整段只有空白
		head    strings.Builder
		dropIdx strings.Builder
		dropCol strings.Builder
	)
	head.WriteString("\n")
	// Skipped 的变更统一在这里把 SQL 抹掉，保证"报告里带 SQL 的就一定会执行"，
	// 避免各个分支自己记得清、自己忘了清。
	normalize := func(c SchemaChange) SchemaChange {
		c.Table = t.Name
		if c.Skipped {
			c.SQL = ""
		}
		return c
	}
	add := func(c SchemaChange) {
		c = normalize(c)
		changes = append(changes, c)
		if c.SQL != "" {
			head.WriteString(c.SQL)
		}
	}
	addDropIndex := func(c SchemaChange) {
		c = normalize(c)
		changes = append(changes, c)
		if c.SQL != "" {
			dropIdx.WriteString(c.SQL)
		}
	}
	addDropColumn := func(c SchemaChange) {
		c = normalize(c)
		changes = append(changes, c)
		if c.SQL != "" {
			dropCol.WriteString(c.SQL)
		}
	}

	// ---- 表注释 ----
	if st.Comment != t.Comment {
		add(SchemaChange{
			Kind:   ChangeTableComment,
			Reason: fmt.Sprintf("%q -> %q", st.Comment, t.Comment),
			SQL:    buildTableCommentSQL(t.Name, t.Comment),
		})
	}

	// ---- 表字符集：默认只报告不改库，要真同步需显式打开 SetSyncTableCharset ----
	if t.Collate != "" && !strings.EqualFold(st.Collation, t.Collate) {
		c := SchemaChange{
			Kind:    ChangeTableCharset,
			Reason:  fmt.Sprintf("%s -> %s", st.Collation, t.Collate),
			SQL:     buildTableCharsetSQL(t.Name, t.Charset, t.Collate),
			Skipped: !opt.syncCharset,
		}
		if !opt.syncCharset {
			c.Reason += "（未同步，CONVERT TO CHARACTER SET 会重写整表数据，如需同步请调用 SetSyncTableCharset(true)）"
		}
		add(c)
	}

	// ---- 已存在的列：修改 ----
	for _, c := range t.Columns {
		dbCol, ok := st.column(c.Name)
		if !ok {
			continue
		}
		reason, reportOnly := diffColumnReason(c, dbCol)
		switch {
		case reason != "":
			if reportOnly != "" {
				reason += "；" + reportOnly
			}
			add(SchemaChange{
				Kind:   ChangeModifyColumn,
				Object: c.Name,
				Reason: reason,
				SQL:    buildModifyColumnSQL(t.Name, c),
			})
		case reportOnly != "":
			// yml 声明了 generator 但库里没有：不执行，只提醒
			add(SchemaChange{
				Kind:    ChangeModifyColumn,
				Object:  c.Name,
				Reason:  reportOnly + "（yml 声明的 generator 库里没有，不自动补，请确认后手动处理）",
				Skipped: true,
			})
		}
	}

	// ---- 新增的列 ----
	addedHere := map[string]bool{}
	for i, c := range t.Columns {
		if _, ok := st.column(c.Name); ok {
			continue
		}
		position := ""
		if opt.keepColumnOrder {
			position = columnPosition(t, i, st, addedHere)
		}
		addedHere[c.Name] = true
		add(SchemaChange{
			Kind:   ChangeAddColumn,
			Object: c.Name,
			Reason: c.Decl,
			SQL:    buildAddColumnSQL(t.Name, c, position),
		})
	}

	// ---- 数据库里有、yml 里没有的列 ----
	for _, dbCol := range st.Columns {
		if _, ok := t.column(dbCol.Name); ok {
			continue
		}
		c := SchemaChange{
			Kind:      ChangeDropColumn,
			Object:    dbCol.Name,
			Reason:    "yml 中已不存在该字段",
			Dangerous: true,
			SQL:       buildDropColumnSQL(t.Name, dbCol.Name),
			Skipped:   opt.dropPolicy == DropPolicyNever,
		}
		if c.Skipped {
			c.SQL = ""
			c.Reason += "（按当前 DropPolicy 不执行删除）"
		}
		addDropColumn(c)
	}

	// ---- 索引：按 唯一索引 -> 主键 -> 全文索引 -> 普通索引 的固定顺序产出 ----
	diffIndexGroup(t, st, opt, t.UniqueIndexes, indexKindUnique, add, addDropIndex)
	diffPrimaryKey(t, st, add, addDropIndex)
	diffIndexGroup(t, st, opt, t.FulltextIndexes, indexKindFulltext, add, addDropIndex)
	diffIndexGroup(t, st, opt, t.Indexes, indexKindNormal, add, addDropIndex)

	// 不参与维护的索引类型只提示，不动它
	for _, idx := range st.Indexes {
		if idx.Ignored {
			changes = append(changes, SchemaChange{
				Table:   t.Name,
				Kind:    ChangeDropIndex,
				Object:  idx.Name,
				Reason:  "该索引类型不在维护范围内，已跳过",
				Skipped: true,
			})
		}
	}

	sql := head.String() + dropIdx.String() + dropCol.String()
	return changes, sql, nil
}

// diffIndexGroup 比对一组索引：yml 有库里有 -> 比列；yml 有库里没 -> 建；库里有 yml 没 -> 删
func diffIndexGroup(
	t *ymlTable,
	st *dbTableState,
	opt diffOption,
	declared []*ymlIndex,
	kind indexKind,
	add func(SchemaChange),
	addDrop func(SchemaChange),
) {
	declaredNames := map[string]bool{}
	for _, idx := range declared {
		declaredNames[idx.Name] = true
		dbIdx, ok := st.indexByName(idx.Name, kind)
		if !ok {
			if existsIdx, exists := st.index(idx.Name); exists {
				if existsIdx.Ignored || existsIdx.Primary {
					// 数据库里同名索引属于不维护的类型，建不了也不删，只提示
					add(SchemaChange{
						Kind:    ChangeAddIndex,
						Object:  idx.Name,
						Reason:  "数据库中已存在同名的其它类型索引，已跳过",
						Skipped: true,
					})
					continue
				}
				// 同名但类型不同（例如把普通索引改成了唯一索引），先删再建
				add(SchemaChange{
					Kind:      ChangeRebuildIndex,
					Object:    idx.Name,
					Reason:    fmt.Sprintf("索引类型发生变化：%s -> %s", dbIndexKindName(existsIdx), kind),
					Dangerous: true,
					SQL:       buildDropIndexSQL(t.Name, idx.Name) + buildCreateIndexSQL(t.Name, idx),
				})
				continue
			}
			add(SchemaChange{
				Kind:   ChangeAddIndex,
				Object: idx.Name,
				Reason: fmt.Sprintf("%s(%s)", kind, strings.Join(idx.Columns, ",")),
				SQL:    buildCreateIndexSQL(t.Name, idx),
			})
			continue
		}
		if !sameStringSlice(dbIdx.Columns, idx.Columns) {
			add(SchemaChange{
				Kind:      ChangeRebuildIndex,
				Object:    idx.Name,
				Reason:    fmt.Sprintf("列发生变化：(%s) -> (%s)", strings.Join(dbIdx.Columns, ","), strings.Join(idx.Columns, ",")),
				Dangerous: true,
				SQL:       buildDropIndexSQL(t.Name, idx.Name) + buildCreateIndexSQL(t.Name, idx),
			})
		}
	}

	for _, dbIdx := range st.Indexes {
		if dbIdx.Ignored || dbIdx.Primary || declaredNames[dbIdx.Name] {
			continue
		}
		if dbIndexKindOf(dbIdx) != kind {
			continue
		}
		c := SchemaChange{
			Kind:      ChangeDropIndex,
			Object:    dbIdx.Name,
			Reason:    "yml 中已不存在该索引",
			Dangerous: true,
			SQL:       buildDropIndexSQL(t.Name, dbIdx.Name),
			Skipped:   opt.dropPolicy == DropPolicyNever,
		}
		if c.Skipped {
			c.SQL = ""
			c.Reason += "（按当前 DropPolicy 不执行删除）"
		}
		addDrop(c)
	}
}

func dbIndexKindOf(idx *dbIndex) indexKind {
	switch {
	case idx.Fulltext:
		return indexKindFulltext
	case idx.Unique:
		return indexKindUnique
	default:
		return indexKindNormal
	}
}

func dbIndexKindName(idx *dbIndex) string {
	return dbIndexKindOf(idx).String()
}

// diffPrimaryKey 比对主键。
// 只有 yml 显式声明了 primary_indexes 才维护主键，
// 只写 id 区的情况下主键由建表语句决定，不做后续变更。
func diffPrimaryKey(t *ymlTable, st *dbTableState, add func(SchemaChange), addDrop func(SchemaChange)) {
	if !t.HasPrimaryDecl {
		if len(t.PrimaryColumns) > 0 && len(st.Primary) > 0 && !sameStringSlice(st.Primary, t.PrimaryColumns) {
			add(SchemaChange{
				Kind: ChangePrimaryKey,
				Reason: fmt.Sprintf("主键区(id)推导出的主键 (%s) 与数据库 (%s) 不一致，"+
					"如需维护主键请显式声明 primary_indexes", strings.Join(t.PrimaryColumns, ","), strings.Join(st.Primary, ",")),
				Skipped: true,
			})
		}
		return
	}
	if sameStringSlice(st.Primary, t.PrimaryColumns) {
		return
	}

	dropSQL := buildDropPrimaryKeySQL(t.Name)
	addSQL := ""
	if len(t.PrimaryColumns) > 0 {
		addSQL = buildAddPrimaryKeySQL(t.Name, t.PrimaryColumns)
	}

	switch {
	case len(t.PrimaryColumns) == 0 && len(st.Primary) > 0:
		// yml 声明了空的主键，表示要删除主键
		addDrop(SchemaChange{
			Kind:      ChangePrimaryKey,
			Reason:    fmt.Sprintf("(%s) -> 无主键", strings.Join(st.Primary, ",")),
			Dangerous: true,
			SQL:       dropSQL,
		})
	case len(t.PrimaryColumns) > 0 && len(st.Primary) == 0:
		add(SchemaChange{
			Kind:      ChangePrimaryKey,
			Reason:    fmt.Sprintf("无主键 -> (%s)", strings.Join(t.PrimaryColumns, ",")),
			Dangerous: true,
			SQL:       addSQL,
		})
	default:
		add(SchemaChange{
			Kind:      ChangePrimaryKey,
			Reason:    fmt.Sprintf("(%s) -> (%s)", strings.Join(st.Primary, ","), strings.Join(t.PrimaryColumns, ",")),
			Dangerous: true,
			SQL:       dropSQL + addSQL,
		})
	}
}

// columnPosition 计算新增列应该落在哪个位置，让数据库的列顺序向 yml 声明的顺序收敛。
// 只在 ADD COLUMN 上使用，不会重排已经存在的列（重排会触发表重建）。
func columnPosition(t *ymlTable, idx int, st *dbTableState, addedHere map[string]bool) string {
	for i := idx - 1; i >= 0; i-- {
		prev := t.Columns[i].Name
		if _, ok := st.column(prev); ok {
			return "AFTER " + quoteIdent(prev)
		}
		if addedHere[prev] {
			return "AFTER " + quoteIdent(prev)
		}
	}
	return "FIRST"
}

// diffColumnReason 判断一列是否需要修改。
// 返回两个原因串：needModify 会真的生成 MODIFY 语句，reportOnly 只进变更报告不执行；
// 两个都为空表示无需修改。
func diffColumnReason(c *ymlColumn, db *dbColumn) (needModify, reportOnly string) {
	var reasons, reports []string

	// 类型
	if !sameColumnType(c.Decl, db.Decl) {
		reasons = append(reasons, fmt.Sprintf("类型(%s->%s)", db.Decl, c.Decl))
	}
	// 是否可空
	if c.Nullable != db.Nullable {
		reasons = append(reasons, fmt.Sprintf("空不空(%v->%v)", db.Nullable, c.Nullable))
	}
	// 备注
	if c.Comment != db.Comment {
		reasons = append(reasons, "备注")
	}
	// 默认值：text/blob/json 这类类型不允许字面默认值，不参与比较
	if !isNoDefaultType(db.Decl) {
		if isExprDefault(db) {
			if !ymlWantsExprDefault(c) {
				reasons = append(reasons, fmt.Sprintf("默认(%s->无)", db.Default))
			}
		} else if c.DefaultSet {
			if !db.DefaultSet || db.Default != c.Default {
				reasons = append(reasons, fmt.Sprintf("默认(%s->%s)", db.Default, c.Default))
			}
		} else if db.DefaultSet && db.Default != "" {
			reasons = append(reasons, fmt.Sprintf("默认(%s->无)", db.Default))
		}
	}
	// 自动生成：AUTO_INCREMENT 与 ON UPDATE CURRENT_TIMESTAMP
	if modify, report, desc := autoFlagDiff(c.Generator, db.Extra); modify {
		reasons = append(reasons, desc)
	} else if report {
		reports = append(reports, desc)
	}

	return strings.Join(reasons, " "), strings.Join(reports, " ")
}

func nullToNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "无"
	}
	return s
}

// isExprDefault 数据库里的默认值是否是 CURRENT_TIMESTAMP 这类表达式。
//
// MySQL 8.0.13+ 会在 EXTRA 里打上 DEFAULT_GENERATED 标记，但 5.7 根本没这个标记，
// 只有 COLUMN_DEFAULT='CURRENT_TIMESTAMP'。因此不能只依赖 EXTRA，
// 否则在 5.7 上会把它当成普通字面量默认值，导致每次执行都多出一条无意义的 MODIFY。
func isExprDefault(db *dbColumn) bool {
	if strings.Contains(strings.ToLower(db.Extra), "default_generated") {
		return true
	}
	d := strings.ToUpper(strings.TrimSpace(db.Default))
	if d == "" || !isTemporalType(db.Decl) {
		return false
	}
	for _, prefix := range []string{"CURRENT_TIMESTAMP", "NOW(", "LOCALTIME", "LOCALTIMESTAMP"} {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}

// ymlWantsExprDefault yml 是否声明了 CURRENT_TIMESTAMP 默认值
func ymlWantsExprDefault(c *ymlColumn) bool {
	gen := strings.ToLower(c.Generator)
	if strings.HasPrefix(strings.TrimSpace(gen), "default") && strings.Contains(gen, "current_timestamp") {
		return true
	}
	return c.DefaultSet && strings.Contains(strings.ToLower(c.Default), "current_timestamp")
}

// autoFlags 抽取"自动生成"相关的语义标记。
// MySQL 回读的 EXTRA 里会带 DEFAULT_GENERATED 这个标记位，它只表示"默认值是表达式"，
// 默认值的比较已经单独处理过了，这里要忽略掉，否则会产生大量无意义的 MODIFY。
func autoFlags(s string) (autoInc, onUpdate bool) {
	lower := strings.ToLower(s)
	autoInc = strings.Contains(lower, "auto_increment")
	onUpdate = strings.Contains(lower, "on update current_timestamp")
	return
}

// autoFlagDiff 比较 yml 与数据库的自动生成标记，并区分两个方向：
//
//   - 库里有、yml 没声明 -> 需要执行 MODIFY
//   - yml 声明了、库里没 -> 只报告不执行
//
// 后者只报告不执行是有意的：给一个没有索引的列补 AUTO_INCREMENT 会被 MySQL 直接拒绝，
// 这种硬失败会把整个发布流程打断，所以放进变更报告里提醒人工确认。
func autoFlagDiff(generator, extra string) (needModify, reportOnly bool, desc string) {
	a1, u1 := autoFlags(generator)
	a2, u2 := autoFlags(extra)
	if a1 == a2 && u1 == u2 {
		return false, false, ""
	}
	desc = fmt.Sprintf("自动(%s->%s)", nullToNone(extra), nullToNone(generator))
	if (a2 && !a1) || (u2 && !u1) {
		return true, false, desc
	}
	return false, true, desc
}

// sameStringSlice 顺序敏感的字符串切片比较
func sameStringSlice(a, b []string) bool {
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
