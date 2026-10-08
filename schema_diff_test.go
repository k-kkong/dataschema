package dataschema

import (
	"fmt"
	"strings"
	"testing"
)

// 本文件测的都是不依赖数据库的纯函数。
// dbTableState 可以直接在内存里拼出来，因此 diff 的各种分支都能离线覆盖。

// ---------------------------------------------------------------------------
// isExprDefault：MySQL 5.7 与 8.0 回读的元数据形状不一样，必须两边都认
// ---------------------------------------------------------------------------

func TestIsExprDefaultAcrossMySQLVersions(t *testing.T) {
	cases := []struct {
		name string
		col  dbColumn
		want bool
	}{
		{
			// MySQL 8.0.13+ 会在 EXTRA 里打 DEFAULT_GENERATED 标记
			name: "8.0 datetime DEFAULT CURRENT_TIMESTAMP",
			col:  dbColumn{Decl: "datetime", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: "DEFAULT_GENERATED"},
			want: true,
		},
		{
			// MySQL 5.7 没有 DEFAULT_GENERATED 标记，EXTRA 是空的。
			// 判定不能只认这个标记（8.0.13 起才有），否则 5.7 上每次执行都多一条无意义的 MODIFY。
			name: "5.7 datetime DEFAULT CURRENT_TIMESTAMP",
			col:  dbColumn{Decl: "datetime", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: ""},
			want: true,
		},
		{
			name: "5.7 timestamp DEFAULT CURRENT_TIMESTAMP",
			col:  dbColumn{Decl: "timestamp", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: ""},
			want: true,
		},
		{
			name: "5.7 带小数秒精度",
			col:  dbColumn{Decl: "datetime(3)", Default: "CURRENT_TIMESTAMP(3)", DefaultSet: true, Extra: ""},
			want: true,
		},
		{
			name: "ON UPDATE 不影响默认值判定",
			col:  dbColumn{Decl: "datetime", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: "on update CURRENT_TIMESTAMP"},
			want: true,
		},
		{
			// varchar 上的 'CURRENT_TIMESTAMP' 只是个普通字符串，不能当表达式
			name: "varchar 的字面量 CURRENT_TIMESTAMP 不算表达式",
			col:  dbColumn{Decl: "varchar(32)", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: ""},
			want: false,
		},
		{
			name: "普通字面量默认值",
			col:  dbColumn{Decl: "int(11)", Default: "0", DefaultSet: true, Extra: ""},
			want: false,
		},
		{
			name: "没有默认值",
			col:  dbColumn{Decl: "datetime", DefaultSet: false, Extra: ""},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isExprDefault(&c.col); got != c.want {
				t.Errorf("isExprDefault(%+v) = %v, 期望 %v", c.col, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// autoFlagDiff：两个方向的处理必须不一样
// ---------------------------------------------------------------------------

func TestAutoFlagDiff(t *testing.T) {
	cases := []struct {
		name       string
		generator  string
		extra      string
		wantModify bool
		wantReport bool
	}{
		{name: "两边都没有", generator: "", extra: "", wantModify: false, wantReport: false},
		{name: "两边都是自增", generator: "AUTO_INCREMENT", extra: "auto_increment", wantModify: false, wantReport: false},
		{
			// 库里有自增、yml 没声明 -> 要改回来
			name: "库里有自增 yml 没声明", generator: "", extra: "auto_increment",
			wantModify: true, wantReport: false,
		},
		{
			name: "库里有 ON UPDATE yml 没声明", generator: "DEFAULT CURRENT_TIMESTAMP",
			extra: "on update CURRENT_TIMESTAMP", wantModify: true, wantReport: false,
		},
		{
			// yml 声明了、库里没有 -> 只报告不执行。
			// 给一个没有索引的列补
			// AUTO_INCREMENT 会被 MySQL 直接拒绝，这种硬失败会打断整个发布流程。
			name: "yml 声明自增 库里没有", generator: "AUTO_INCREMENT", extra: "",
			wantModify: false, wantReport: true,
		},
		{
			name: "yml 声明 ON UPDATE 库里没有", generator: "DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
			extra: "", wantModify: false, wantReport: true,
		},
		{
			name: "ON UPDATE 两边都有", generator: "DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
			extra: "on update CURRENT_TIMESTAMP", wantModify: false, wantReport: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			modify, report, desc := autoFlagDiff(c.generator, c.extra)
			if modify != c.wantModify || report != c.wantReport {
				t.Errorf("autoFlagDiff(%q, %q) = (modify:%v, report:%v), 期望 (modify:%v, report:%v)",
					c.generator, c.extra, modify, report, c.wantModify, c.wantReport)
			}
			if (modify || report) && desc == "" {
				t.Errorf("autoFlagDiff(%q, %q) 有差异却没给出描述", c.generator, c.extra)
			}
			if !modify && !report && desc != "" {
				t.Errorf("autoFlagDiff(%q, %q) 无差异却给出了描述 %q", c.generator, c.extra, desc)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// diffColumnReason：各类差异都要能被识别出来
// ---------------------------------------------------------------------------

func TestDiffColumnReason(t *testing.T) {
	cases := []struct {
		name       string
		yml        ymlColumn
		db         dbColumn
		wantReason string // 期望出现在 needModify 里的关键字，空表示不该有
		wantReport string // 期望出现在 reportOnly 里的关键字，空表示不该有
	}{
		{
			name: "完全一致",
			yml:  ymlColumn{Name: "a", Decl: "varchar(255)", Comment: "备注"},
			db:   dbColumn{Name: "a", Decl: "varchar(255)", Comment: "备注"},
		},
		{
			name:       "类型不同",
			yml:        ymlColumn{Name: "a", Decl: "varchar(64)"},
			db:         dbColumn{Name: "a", Decl: "varchar(255)"},
			wantReason: "类型",
		},
		{
			// MySQL 5.7 回读 int(11)、8.0 回读 int，归一化后必须视为相同
			name: "int 与 int(11) 等价",
			yml:  ymlColumn{Name: "a", Decl: "int(11)"},
			db:   dbColumn{Name: "a", Decl: "int(11)"},
		},
		{
			name:       "可空性不同",
			yml:        ymlColumn{Name: "a", Decl: "int(11)", Nullable: true},
			db:         dbColumn{Name: "a", Decl: "int(11)", Nullable: false},
			wantReason: "空不空",
		},
		{
			name:       "备注不同",
			yml:        ymlColumn{Name: "a", Decl: "int(11)", Comment: "新备注"},
			db:         dbColumn{Name: "a", Decl: "int(11)", Comment: "旧备注"},
			wantReason: "备注",
		},
		{
			name:       "默认值不同",
			yml:        ymlColumn{Name: "a", Decl: "int(11)", Default: "1", DefaultSet: true},
			db:         dbColumn{Name: "a", Decl: "int(11)", Default: "0", DefaultSet: true},
			wantReason: "默认",
		},
		{
			// text/blob/json 不允许字面默认值，数据库侧有值也不该比对
			name: "text 类型不比对默认值",
			yml:  ymlColumn{Name: "a", Decl: "text"},
			db:   dbColumn{Name: "a", Decl: "text", Default: "x", DefaultSet: true},
		},
		{
			// 5.7 上的 DEFAULT CURRENT_TIMESTAMP：yml 用 generator 声明，不该判成差异
			name: "5.7 表达式默认值已声明",
			yml:  ymlColumn{Name: "a", Decl: "datetime", Generator: "DEFAULT CURRENT_TIMESTAMP"},
			db:   dbColumn{Name: "a", Decl: "datetime", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: ""},
		},
		{
			name:       "5.7 表达式默认值未声明",
			yml:        ymlColumn{Name: "a", Decl: "datetime"},
			db:         dbColumn{Name: "a", Decl: "datetime", Default: "CURRENT_TIMESTAMP", DefaultSet: true, Extra: ""},
			wantReason: "默认",
		},
		{
			name:       "yml 声明自增但库里没有 只报告",
			yml:        ymlColumn{Name: "a", Decl: "int(11)", Generator: "AUTO_INCREMENT"},
			db:         dbColumn{Name: "a", Decl: "int(11)"},
			wantReport: "自动",
		},
		{
			name:       "库里有自增 yml 没声明 要执行",
			yml:        ymlColumn{Name: "a", Decl: "int(11)"},
			db:         dbColumn{Name: "a", Decl: "int(11)", Extra: "auto_increment"},
			wantReason: "自动",
		},
		{
			// EXTRA 里的 DEFAULT_GENERATED 只表示"默认值是表达式"，
			// 不该被当成自动生成标记，否则会产生大量无意义 MODIFY
			name: "忽略 DEFAULT_GENERATED 标记",
			yml:  ymlColumn{Name: "a", Decl: "datetime", Generator: "DEFAULT CURRENT_TIMESTAMP"},
			db: dbColumn{Name: "a", Decl: "datetime", Default: "CURRENT_TIMESTAMP",
				DefaultSet: true, Extra: "DEFAULT_GENERATED"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			modify, report := diffColumnReason(&c.yml, &c.db)
			if c.wantReason == "" && modify != "" {
				t.Errorf("不该产生 MODIFY，却得到 %q", modify)
			}
			if c.wantReason != "" && !strings.Contains(modify, c.wantReason) {
				t.Errorf("MODIFY 原因 %q 中没找到 %q", modify, c.wantReason)
			}
			if c.wantReport == "" && report != "" {
				t.Errorf("不该产生报告项，却得到 %q", report)
			}
			if c.wantReport != "" && !strings.Contains(report, c.wantReport) {
				t.Errorf("报告原因 %q 中没找到 %q", report, c.wantReport)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// diffTable：整表比对，覆盖建表/增删改列/增删改索引/主键/字符集/DropPolicy
// ---------------------------------------------------------------------------

// parseYmlForTest 把 yml 文本解析成表模型，测试专用
func parseYmlForTest(t *testing.T, yml string) *ymlTable {
	t.Helper()
	doc, err := yamlFileToOrderedJSON([]byte(yml))
	if err != nil {
		t.Fatalf("yaml 解析失败: %v", err)
	}
	tbl, _, err := parseTableDoc(doc, "test.yml")
	if err != nil {
		t.Fatalf("配置解析失败: %v", err)
	}
	return tbl
}

// kindsOf 取出变更记录里"会真的执行"的类型清单
func kindsOf(changes []SchemaChange, skipped bool) []string {
	var out []string
	for _, c := range changes {
		if c.Skipped != skipped {
			continue
		}
		out = append(out, c.Kind)
		if skipped && c.SQL != "" {
			out = append(out, "!!被跳过的变更不该带 SQL")
		}
	}
	return out
}

func TestDiffTableCreateWhenAbsent(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
    comment: 演示表
  id:
    id:
      type: integer unsigned
      nullable: false
      generator: AUTO_INCREMENT
  fields:
    name:
      type: varchar(64)
      nullable: false
      comment: 名称
  indexes:
    idx_name:
      columns: [name]
`)
	changes, sql, err := diffTable(tbl, newDBTableState("demo"), diffOption{dropPolicy: DropPolicyAlways})
	if err != nil {
		t.Fatalf("diffTable 出错: %v", err)
	}
	if got := kindsOf(changes, false); len(got) != 1 || got[0] != ChangeCreateTable {
		t.Fatalf("期望只有一条建表变更，实际 %v", got)
	}
	// 列顺序必须是 id 区在前、fields 区在后，各自保持 yml 书写顺序
	idPos, namePos := strings.Index(sql, "`id`"), strings.Index(sql, "`name`")
	if idPos < 0 || namePos < 0 || idPos > namePos {
		t.Errorf("建表语句里 id 列没有排在 name 列前面:\n%s", sql)
	}
	for _, want := range []string{"PRIMARY KEY(`id`)", "INDEX `idx_name` (`name`)", "COMMENT = '演示表'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("建表语句缺少 %q:\n%s", want, sql)
		}
	}
}

func TestDiffTableColumnChanges(t *testing.T) {
	base := `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    keep:
      type: varchar(64)
      nullable: false
      comment: 保持不变
    renamed_type:
      type: %s
      nullable: false
      comment: %s
`
	st := newDBTableState("demo")
	st.Exists = true
	st.Collation = "utf8mb4_general_ci"
	st.addColumn(&dbColumn{Name: "keep", Decl: "varchar(64)", Comment: "保持不变", Position: 0})
	st.addColumn(&dbColumn{Name: "renamed_type", Decl: "varchar(64)", Comment: "旧注释", Position: 1})
	st.addColumn(&dbColumn{Name: "gone", Decl: "int(11)", Position: 2})

	cases := []struct {
		name       string
		ymlType    string
		ymlComment string
		wantKind   string
		wantIn     string
	}{
		{name: "改类型", ymlType: "varchar(32)", ymlComment: "旧注释", wantKind: ChangeModifyColumn, wantIn: "类型"},
		{name: "改注释", ymlType: "varchar(64)", ymlComment: "新注释", wantKind: ChangeModifyColumn, wantIn: "备注"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tbl := parseYmlForTest(t, fmt.Sprintf(base, c.ymlType, c.ymlComment))
			changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyNever})
			if err != nil {
				t.Fatalf("diffTable 出错: %v", err)
			}
			var found bool
			for _, ch := range changes {
				if ch.Kind == c.wantKind && ch.Object == "renamed_type" && strings.Contains(ch.Reason, c.wantIn) {
					found = true
				}
			}
			if !found {
				t.Fatalf("没找到期望的变更 %s(%s)，实际 %+v", c.wantKind, c.wantIn, changes)
			}
			if !strings.Contains(sql, "MODIFY COLUMN `renamed_type`") {
				t.Errorf("SQL 里没有 MODIFY 语句:\n%s", sql)
			}
		})
	}
}

// TestDiffTableDropPolicy 删列/删索引受 DropPolicy 控制，默认 always 会真的 DROP
func TestDiffTableDropPolicy(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    keep:
      type: varchar(64)
      nullable: false
`)
	buildState := func() *dbTableState {
		st := newDBTableState("demo")
		st.Exists = true
		st.Collation = "utf8mb4_general_ci"
		st.addColumn(&dbColumn{Name: "keep", Decl: "varchar(64)", Position: 0})
		st.addColumn(&dbColumn{Name: "gone_col", Decl: "int(11)", Position: 1})
		st.addIndexColumn("gone_idx", "keep", "BTREE", 1)
		return st
	}

	t.Run("默认 always 会生成删除语句", func(t *testing.T) {
		changes, sql, err := diffTable(tbl, buildState(), diffOption{dropPolicy: DropPolicyAlways})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if !strings.Contains(sql, "DROP `gone_col`") {
			t.Errorf("缺少删列语句:\n%s", sql)
		}
		if !strings.Contains(sql, "DROP INDEX `gone_idx`") {
			t.Errorf("缺少删索引语句:\n%s", sql)
		}
		var dangerous int
		for _, c := range changes {
			if c.Dangerous && !c.Skipped {
				dangerous++
			}
		}
		if dangerous != 2 {
			t.Errorf("期望 2 条高危变更，实际 %d 条：%+v", dangerous, changes)
		}
	})

	t.Run("never 只报告不生成语句", func(t *testing.T) {
		changes, sql, err := diffTable(tbl, buildState(), diffOption{dropPolicy: DropPolicyNever})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if strings.Contains(sql, "DROP") {
			t.Errorf("DropPolicyNever 下不该出现 DROP:\n%s", sql)
		}
		skipped := kindsOf(changes, true)
		if len(skipped) != 2 {
			t.Errorf("期望 2 条被跳过的变更，实际 %v", skipped)
		}
	})
}

// TestDiffTableIndexRebuild 索引改列/改类型都要走"先删后建"
func TestDiffTableIndexRebuild(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varchar(64)
      nullable: false
    b:
      type: varchar(64)
      nullable: false
  indexes:
    idx_a:
      columns: [a, b]
  unique_indexes:
    idx_b:
      columns: [b]
`)
	st := newDBTableState("demo")
	st.Exists = true
	st.Collation = "utf8mb4_general_ci"
	st.addColumn(&dbColumn{Name: "a", Decl: "varchar(64)", Position: 0})
	st.addColumn(&dbColumn{Name: "b", Decl: "varchar(64)", Position: 1})
	// idx_a 库里只有 (a)，yml 要求 (a,b) -> 列变化，需要重建
	st.addIndexColumn("idx_a", "a", "BTREE", 1)
	// idx_b 库里是普通索引，yml 要求唯一索引 -> 类型变化，需要重建
	st.addIndexColumn("idx_b", "b", "BTREE", 1)

	changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyAlways})
	if err != nil {
		t.Fatalf("diffTable 出错: %v", err)
	}
	var rebuilds int
	for _, c := range changes {
		if c.Kind == ChangeRebuildIndex {
			rebuilds++
		}
	}
	if rebuilds != 2 {
		t.Fatalf("期望 2 条重建索引，实际 %d 条：%+v", rebuilds, changes)
	}
	for _, want := range []string{
		"DROP INDEX `idx_a`", "CREATE INDEX `idx_a` ON `demo`(`a`,`b`)",
		"DROP INDEX `idx_b`", "CREATE UNIQUE INDEX `idx_b` ON `demo`(`b`)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL 缺少 %q:\n%s", want, sql)
		}
	}
}

// TestDiffTableIndexIdempotent 结构化比较必须做到幂等：
// 只比名字、类型和列清单，yml 侧多写一个不参与建索引的配置项不算差异，
// 所以同一份配置反复执行不会把索引 drop 了再 create 一遍。
func TestDiffTableIndexIdempotent(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varchar(64)
      nullable: false
    b:
      type: varchar(64)
      nullable: false
  indexes:
    idx_ab:
      columns: [a, b]
  unique_indexes:
    unq_a:
      columns: [a]
  fulltext_indexes:
    ft_b:
      with_parser: ngram
      columns: [b]
`)
	st := newDBTableState("demo")
	st.Exists = true
	st.Collation = "utf8mb4_general_ci"
	st.addColumn(&dbColumn{Name: "a", Decl: "varchar(64)", Position: 0})
	st.addColumn(&dbColumn{Name: "b", Decl: "varchar(64)", Position: 1})
	st.addIndexColumn("idx_ab", "a", "BTREE", 1)
	st.addIndexColumn("idx_ab", "b", "BTREE", 1)
	st.addIndexColumn("unq_a", "a", "BTREE", 0)
	st.addIndexColumn("ft_b", "b", "FULLTEXT", 1)

	changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyAlways})
	if err != nil {
		t.Fatalf("diffTable 出错: %v", err)
	}
	if got := kindsOf(changes, false); len(got) != 0 {
		t.Fatalf("结构一致时不该有任何变更，实际 %v", got)
	}
	if strings.TrimSpace(sql) != "" {
		t.Errorf("结构一致时 SQL 应为空白，实际 %q", sql)
	}
}

// TestDiffTableIgnoredIndexIsNeverDropped 空间索引等不参与维护的类型永远不该被删
func TestDiffTableIgnoredIndexIsNeverDropped(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varchar(64)
      nullable: false
`)
	st := newDBTableState("demo")
	st.Exists = true
	st.Collation = "utf8mb4_general_ci"
	st.addColumn(&dbColumn{Name: "a", Decl: "varchar(64)", Position: 0})
	st.addIndexColumn("sp_a", "a", "SPATIAL", 1)

	changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyAlways})
	if err != nil {
		t.Fatalf("diffTable 出错: %v", err)
	}
	if strings.Contains(sql, "sp_a") {
		t.Errorf("不该对不维护的索引生成任何语句:\n%s", sql)
	}
	var reported bool
	for _, c := range changes {
		if c.Object == "sp_a" && c.Skipped {
			reported = true
		}
	}
	if !reported {
		t.Errorf("不维护的索引应该在报告里提示，实际 %+v", changes)
	}
}

// TestDiffTableCharsetDrift 字符集漂移默认只报告，开启开关后才生成语句
func TestDiffTableCharsetDrift(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varchar(64)
      nullable: false
`)
	buildState := func() *dbTableState {
		st := newDBTableState("demo")
		st.Exists = true
		st.Collation = "utf8mb4_unicode_ci"
		st.addColumn(&dbColumn{Name: "a", Decl: "varchar(64)", Position: 0})
		return st
	}

	t.Run("默认只报告", func(t *testing.T) {
		changes, sql, err := diffTable(tbl, buildState(), diffOption{dropPolicy: DropPolicyAlways})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if strings.Contains(sql, "CONVERT TO CHARACTER SET") {
			t.Errorf("默认不该同步字符集:\n%s", sql)
		}
		if got := kindsOf(changes, true); len(got) != 1 || got[0] != ChangeTableCharset {
			t.Errorf("期望一条被跳过的字符集变更，实际 %v", got)
		}
	})

	t.Run("开启后生成语句", func(t *testing.T) {
		changes, sql, err := diffTable(tbl, buildState(), diffOption{dropPolicy: DropPolicyAlways, syncCharset: true})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if !strings.Contains(sql, "CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci") {
			t.Errorf("缺少字符集同步语句:\n%s", sql)
		}
		if got := kindsOf(changes, false); len(got) != 1 || got[0] != ChangeTableCharset {
			t.Errorf("期望一条字符集变更，实际 %v", got)
		}
	})
}

// TestDiffTablePrimaryKey 只有显式声明 primary_indexes 才维护主键
func TestDiffTablePrimaryKey(t *testing.T) {
	st := newDBTableState("demo")
	st.Exists = true
	st.Collation = "utf8mb4_general_ci"
	st.addColumn(&dbColumn{Name: "a", Decl: "int(11)", Position: 0})
	st.addColumn(&dbColumn{Name: "b", Decl: "int(11)", Position: 1})
	st.addIndexColumn("PRIMARY", "a", "BTREE", 0)

	t.Run("未声明 primary_indexes 时只报告不改主键", func(t *testing.T) {
		tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  id:
    b:
      type: int
      nullable: false
  fields:
    a:
      type: int
      nullable: false
`)
		changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyAlways})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if strings.Contains(sql, "PRIMARY KEY") {
			t.Errorf("未声明 primary_indexes 时不该动主键:\n%s", sql)
		}
		if got := kindsOf(changes, true); len(got) != 1 || got[0] != ChangePrimaryKey {
			t.Errorf("期望一条被跳过的主键提示，实际 %v", got)
		}
	})

	t.Run("声明后会生成先删后加", func(t *testing.T) {
		tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  primary_indexes:
    columns: [b, a]
  fields:
    a:
      type: int
      nullable: false
    b:
      type: int
      nullable: false
`)
		changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyAlways})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if !strings.Contains(sql, "DROP PRIMARY KEY") || !strings.Contains(sql, "ADD PRIMARY KEY (`b`,`a`)") {
			t.Errorf("主键变更语句不正确:\n%s", sql)
		}
		if got := kindsOf(changes, false); len(got) != 1 || got[0] != ChangePrimaryKey {
			t.Errorf("期望一条主键变更，实际 %v", got)
		}
	})

	t.Run("主键一致时不动", func(t *testing.T) {
		tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  primary_indexes:
    columns: [a]
  fields:
    a:
      type: int
      nullable: false
    b:
      type: int
      nullable: false
`)
		changes, sql, err := diffTable(tbl, st, diffOption{dropPolicy: DropPolicyAlways})
		if err != nil {
			t.Fatalf("diffTable 出错: %v", err)
		}
		if strings.Contains(sql, "PRIMARY KEY") {
			t.Errorf("主键一致时不该生成语句:\n%s", sql)
		}
		if got := kindsOf(changes, false); len(got) != 0 {
			t.Errorf("主键一致时不该有变更，实际 %v", got)
		}
	})
}

// TestColumnPosition 新增列要按 yml 顺序落位，让库结构逐步向声明收敛。
// 已存在的列不会被重排，因为重排会触发整表重建。
func TestColumnPosition(t *testing.T) {
	tbl := parseYmlForTest(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
    b:
      type: int
      nullable: false
    c:
      type: int
      nullable: false
    d:
      type: int
      nullable: false
`)
	// 库里已有 a、c，缺 b、d
	st := newDBTableState("demo")
	st.Exists = true
	st.addColumn(&dbColumn{Name: "a", Decl: "int(11)", Position: 0})
	st.addColumn(&dbColumn{Name: "c", Decl: "int(11)", Position: 1})

	// b 刚被本批新增，记在 addedHere 里
	added := map[string]bool{"b": true}

	cases := []struct {
		name string
		idx  int
		want string
	}{
		{name: "b 紧跟在已存在的 a 后面", idx: 1, want: "AFTER `a`"},
		{name: "d 紧跟在已存在的 c 后面", idx: 3, want: "AFTER `c`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := columnPosition(tbl, c.idx, st, added); got != c.want {
				t.Errorf("columnPosition(idx=%d) = %q, 期望 %q", c.idx, got, c.want)
			}
		})
	}

	t.Run("前面的列全部不存在时落到 FIRST", func(t *testing.T) {
		if got := columnPosition(tbl, 0, st, map[string]bool{}); got != "FIRST" {
			t.Errorf("首列位置应为 FIRST，实际 %q", got)
		}
	})

	t.Run("可以跟在本批刚新增的列后面", func(t *testing.T) {
		// d 前面是 c，假设 c 也是本批新增的，就应该排在 c 后面
		if got := columnPosition(tbl, 3, newDBTableState("demo"), map[string]bool{"c": true}); got != "AFTER `c`" {
			t.Errorf("应排在本批新增的 c 后面，实际 %q", got)
		}
	})
}

func TestSameStringSlice(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, nil, true},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"b", "a"}, false}, // 顺序敏感：索引列顺序有意义
		{[]string{"a"}, []string{"a", "b"}, false},
	}
	for _, c := range cases {
		if got := sameStringSlice(c.a, c.b); got != c.want {
			t.Errorf("sameStringSlice(%v, %v) = %v, 期望 %v", c.a, c.b, got, c.want)
		}
	}
}
