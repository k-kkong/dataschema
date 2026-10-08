package dataschema

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// 本文件覆盖 yaml -> 内存模型的解析，重点盯三件必须成立的事：
//  1. 保留 yml 的书写顺序，建表语句的列顺序 = id 区在前 + fields 区按书写顺序
//  2. 保留标量原文，default: 007 不能被解析成 7、default: 1.10 不能被解析成 1.1
//  3. 字段名里含 . * ? # @ 这些字符时也要取到正确的值

// mustParse 解析 yml 文本，失败直接终止测试
func mustParse(t *testing.T, yml string) *ymlTable {
	t.Helper()
	doc, err := yamlFileToOrderedJSON([]byte(yml))
	if err != nil {
		t.Fatalf("yaml 转 JSON 失败: %v", err)
	}
	tbl, _, err := parseTableDoc(doc, "test.yml")
	if err != nil {
		t.Fatalf("配置解析失败: %v", err)
	}
	return tbl
}

// parseWithWarns 解析 yml 文本并返回告警
func parseWithWarns(t *testing.T, yml string) (*ymlTable, []string) {
	t.Helper()
	doc, err := yamlFileToOrderedJSON([]byte(yml))
	if err != nil {
		t.Fatalf("yaml 转 JSON 失败: %v", err)
	}
	tbl, warns, err := parseTableDoc(doc, "test.yml")
	if err != nil {
		t.Fatalf("配置解析失败: %v", err)
	}
	return tbl, warns
}

// TestColumnOrderFollowsYml 建表列顺序必须是 id 区在前、fields 区在后，各自保持书写顺序
func TestColumnOrderFollowsYml(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    zebra:
      type: int
      nullable: false
    apple:
      type: int
      nullable: false
    mango:
      type: int
      nullable: false
  id:
    pk2:
      type: integer unsigned
      nullable: false
    pk1:
      type: integer unsigned
      nullable: false
      generator: AUTO_INCREMENT
`)
	// 注意 yml 里 fields 写在 id 前面、且字段名刻意用字母序倒排，
	// 期望结果必须是 pk2/pk1/zebra/apple/mango：id 区先走，各自保持书写顺序。
	// 一旦顺序被排成字母序，就会变成 apple/mango/zebra/pk1/pk2。
	want := []string{"pk2", "pk1", "zebra", "apple", "mango"}
	if len(tbl.Columns) != len(want) {
		t.Fatalf("列数量 %d，期望 %d", len(tbl.Columns), len(want))
	}
	for i, name := range want {
		if tbl.Columns[i].Name != name {
			t.Errorf("第 %d 列应为 %q，实际 %q", i, name, tbl.Columns[i].Name)
		}
	}
	// id 区的字段必须被标记出来
	for _, c := range tbl.Columns {
		if want := c.Name == "pk1" || c.Name == "pk2"; c.FromID != want {
			t.Errorf("字段 %q 的 FromID = %v，期望 %v", c.Name, c.FromID, want)
		}
	}
}

// TestIndexOrderFollowsYml 索引也要按书写顺序，不能变成字母序
func TestIndexOrderFollowsYml(t *testing.T) {
	tbl := mustParse(t, `
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
      type: varchar(64)
      nullable: false
  indexes:
    idx_c:
      columns: [c]
    idx_a:
      columns: [a]
    idx_b:
      columns: [b, a]
  unique_indexes:
    unq_b:
      columns: [b]
    unq_a:
      columns: [a]
`)
	wantNormal := []string{"idx_c", "idx_a", "idx_b"}
	for i, name := range wantNormal {
		if tbl.Indexes[i].Name != name {
			t.Errorf("第 %d 个普通索引应为 %q，实际 %q", i, name, tbl.Indexes[i].Name)
		}
	}
	wantUnique := []string{"unq_b", "unq_a"}
	for i, name := range wantUnique {
		if tbl.UniqueIndexes[i].Name != name {
			t.Errorf("第 %d 个唯一索引应为 %q，实际 %q", i, name, tbl.UniqueIndexes[i].Name)
		}
	}
	// 复合索引的列顺序同样有意义
	if got := strings.Join(tbl.Indexes[2].Columns, ","); got != "b,a" {
		t.Errorf("idx_b 的列顺序应为 b,a，实际 %q", got)
	}
}

// TestScalarRawTextPreserved 标量必须按原文保留，不能被 yaml 的隐式类型转换改写
func TestScalarRawTextPreserved(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    leading_zero:
      type: varchar(8)
      nullable: false
      default: 007
    trailing_zero:
      type: varchar(8)
      nullable: false
      default: 1.10
    big_number:
      type: varchar(32)
      nullable: false
      default: 12345678901234567890
    bool_like:
      type: varchar(8)
      nullable: false
      default: 'no'
    empty_string:
      type: varchar(8)
      nullable: false
      default: ''
`)
	want := map[string]string{
		"leading_zero":  "007",  // 前导零必须原样保留，被当成数字就会变成 7
		"trailing_zero": "1.10", // 末尾零必须原样保留，被当成浮点就会变成 1.1
		"big_number":    "12345678901234567890",
		"bool_like":     "no", // 不能被当成布尔值
		"empty_string":  "",
	}
	for name, expect := range want {
		c, ok := tbl.column(name)
		if !ok {
			t.Fatalf("找不到字段 %q", name)
		}
		if !c.DefaultSet {
			t.Errorf("字段 %q 应记录为已声明 default", name)
		}
		if c.Default != expect {
			t.Errorf("字段 %q 的 default = %q，期望 %q", name, c.Default, expect)
		}
	}
}

// TestDefaultDeclaredVsAbsent 声明为空串、写成 yaml 空值、完全没声明，三者必须区分开
func TestDefaultDeclaredVsAbsent(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    explicit_empty:
      type: varchar(8)
      nullable: false
      default: ''
    yaml_null:
      type: varchar(8)
      nullable: false
      default:
    absent:
      type: varchar(8)
      nullable: false
`)
	cases := []struct {
		name        string
		wantSet     bool
		wantNull    bool
		wantDefault string
	}{
		{name: "explicit_empty", wantSet: true, wantNull: false, wantDefault: ""},
		{name: "yaml_null", wantSet: true, wantNull: true, wantDefault: ""},
		{name: "absent", wantSet: false, wantNull: false, wantDefault: ""},
	}
	for _, c := range cases {
		col, ok := tbl.column(c.name)
		if !ok {
			t.Fatalf("找不到字段 %q", c.name)
		}
		if col.DefaultSet != c.wantSet || col.DefaultNull != c.wantNull || col.Default != c.wantDefault {
			t.Errorf("字段 %q: DefaultSet=%v DefaultNull=%v Default=%q，期望 %v/%v/%q",
				c.name, col.DefaultSet, col.DefaultNull, col.Default, c.wantSet, c.wantNull, c.wantDefault)
		}
	}

	// 只有写成 yaml 空值才该告警，显式写 '' 是正常用法
	_, warns := parseWithWarns(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    ok:
      type: varchar(8)
      nullable: false
      default: ''
    bad:
      type: varchar(8)
      nullable: false
      default:
`)
	var hitOK, hitBad bool
	for _, w := range warns {
		if strings.Contains(w, "'ok'") {
			hitOK = true
		}
		if strings.Contains(w, "'bad'") {
			hitBad = true
		}
	}
	if hitOK {
		t.Errorf("显式写 default: '' 不该告警，实际告警：%v", warns)
	}
	if !hitBad {
		t.Errorf("default 写成 yaml 空值应该告警，实际：%v", warns)
	}
}

// TestNullableParsing nullable 要能认常见写法，写错要报错而不是静默当成 false
func TestNullableParsing(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"true", true}, {"false", false},
		{"yes", true}, {"no", false},
		{"on", true}, {"off", false},
		{"TRUE", true}, {"False", false},
	} {
		tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: `+tc.text+`
`)
		if c, _ := tbl.column("a"); c.Nullable != tc.want {
			t.Errorf("nullable: %s 解析为 %v，期望 %v", tc.text, c.Nullable, tc.want)
		}
	}

	// 写错必须报错：静默当成 false 会让列变成 NOT NULL，属于破坏性后果
	doc, err := yamlFileToOrderedJSON([]byte(`
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: 可以
`))
	if err != nil {
		t.Fatalf("yaml 转 JSON 失败: %v", err)
	}
	if _, _, err := parseTableDoc(doc, "test.yml"); err == nil {
		t.Error("nullable 写成非法值时应该报错")
	}
}

// TestGjsonSpecialCharsInNames 字段名含 gjson 特殊字符时不能取错值。
// 解析完成后所有取值都走 Go map/slice，不拼 "fields.<名字>.type" 这种字符串路径，
// 所以名字里带 . * ? # @ 也不会错位。
func TestGjsonSpecialCharsInNames(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    "a.b":
      type: varchar(16)
      nullable: false
      comment: 带点
    "a*b":
      type: varchar(16)
      nullable: false
      comment: 带星号
    "a?b":
      type: varchar(16)
      nullable: false
      comment: 带问号
    "a#b":
      type: varchar(16)
      nullable: false
      comment: 带井号
    "a@b":
      type: varchar(16)
      nullable: false
      comment: 带 at
`)
	for _, name := range []string{"a.b", "a*b", "a?b", "a#b", "a@b"} {
		c, ok := tbl.column(name)
		if !ok {
			t.Fatalf("找不到字段 %q", name)
		}
		if c.Decl != "varchar(16)" {
			t.Errorf("字段 %q 的类型 = %q，期望 varchar(16)", name, c.Decl)
		}
		if !strings.HasPrefix(c.Comment, "带") {
			t.Errorf("字段 %q 的备注取错了：%q", name, c.Comment)
		}
	}
}

// TestDuplicateKeyDetected 同一层重复定义配置项必须报错，否则后写的会静默覆盖先写的
func TestDuplicateKeyDetected(t *testing.T) {
	_, err := yamlFileToOrderedJSON([]byte(`
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
    a:
      type: varchar(8)
      nullable: false
`))
	if err == nil {
		t.Fatal("重复定义字段 a 应该报错")
	}
	if !strings.Contains(err.Error(), "重复定义") {
		t.Errorf("报错信息应指出重复定义，实际：%v", err)
	}
}

// TestMergeKeySupported yaml 的合并键 << 要能正确展开，且显式声明优先
func TestMergeKeySupported(t *testing.T) {
	tbl := mustParse(t, `
Base: &base
  type: varchar(32)
  nullable: false
  comment: 来自锚点
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      <<: *base
    b:
      <<: *base
      comment: 覆盖锚点
      type: varchar(64)
`)
	a, _ := tbl.column("a")
	if a.Decl != "varchar(32)" || a.Comment != "来自锚点" {
		t.Errorf("a 未正确继承锚点：%+v", a)
	}
	b, _ := tbl.column("b")
	if b.Decl != "varchar(64)" || b.Comment != "覆盖锚点" {
		t.Errorf("b 的显式声明应覆盖锚点：%+v", b)
	}
}

// TestShardingExpansion 分表展开后每张表都要有正确的名字，且共享同一份字段定义
func TestShardingExpansion(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  sharding_tables: demo_01, demo_02 ,demo_03
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
`)
	want := []string{"demo_01", "demo_02", "demo_03"}
	got := tbl.expandSharding()
	if len(got) != len(want) {
		t.Fatalf("展开出 %d 张表，期望 %d 张", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("第 %d 张分表名 = %q，期望 %q", i, got[i].Name, name)
		}
		if got[i].DeclaredName != "demo" {
			t.Errorf("第 %d 张分表的 DeclaredName = %q，期望 demo", i, got[i].DeclaredName)
		}
		if len(got[i].Columns) != 1 {
			t.Errorf("第 %d 张分表丢了字段定义", i)
		}
	}

	// 没配分表时应返回自身，且 Name 等于声明的表名
	single := mustParse(t, `
Table:
  table: only_one
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
`).expandSharding()
	if len(single) != 1 || single[0].Name != "only_one" {
		t.Errorf("未配分表时应返回自身，实际 %+v", single)
	}
}

// TestNameIsSetBeforeExpansion Name 在解析阶段就要有值，
// 否则任何早于 expandSharding 使用它的地方都会拼出空表名的 SQL
func TestNameIsSetBeforeExpansion(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
`)
	if tbl.Name != "demo" {
		t.Errorf("解析完成后 Name = %q，期望 demo", tbl.Name)
	}
}

// TestPrimaryColumnsFromIDSection 没写 primary_indexes 时，主键由 id 区按顺序推导
func TestPrimaryColumnsFromIDSection(t *testing.T) {
	tbl := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  id:
    tenant_id:
      type: integer unsigned
      nullable: false
    id:
      type: integer unsigned
      nullable: false
      generator: AUTO_INCREMENT
  fields:
    a:
      type: int
      nullable: false
`)
	if tbl.HasPrimaryDecl {
		t.Error("没写 primary_indexes 时 HasPrimaryDecl 应为 false")
	}
	if got := strings.Join(tbl.PrimaryColumns, ","); got != "tenant_id,id" {
		t.Errorf("推导出的主键 = %q，期望 tenant_id,id", got)
	}

	explicit := mustParse(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  primary_indexes:
    columns: [id, tenant_id]
  id:
    tenant_id:
      type: integer unsigned
      nullable: false
    id:
      type: integer unsigned
      nullable: false
  fields:
    a:
      type: int
      nullable: false
`)
	if !explicit.HasPrimaryDecl {
		t.Error("写了 primary_indexes 时 HasPrimaryDecl 应为 true")
	}
	if got := strings.Join(explicit.PrimaryColumns, ","); got != "id,tenant_id" {
		t.Errorf("显式主键 = %q，期望 id,tenant_id", got)
	}
}

// TestParseErrors 各种配置错误都要有明确的报错
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name   string
		yml    string
		wantIn string
	}{
		{
			name:   "缺少 Table 节点",
			yml:    "NotTable:\n  a: 1\n",
			wantIn: "缺少 Table 节点",
		},
		{
			name: "缺少表名",
			yml: `
Table:
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
`,
			wantIn: "缺少表名",
		},
		{
			name: "一个字段都没有",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
`,
			wantIn: "没有声明任何字段",
		},
		{
			name: "主键区与普通字段重名",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  id:
    a:
      type: int
      nullable: false
  fields:
    a:
      type: int
      nullable: false
`,
			wantIn: "主键字段和普通字段重名",
		},
		{
			name: "索引引用了不存在的列",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
  indexes:
    idx_missing:
      columns: [not_exist]
`,
			wantIn: "is not find",
		},
		{
			name: "主键列未在字段中定义",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  primary_indexes:
    columns: [ghost]
  fields:
    a:
      type: int
      nullable: false
`,
			wantIn: "未在 id/fields 中定义",
		},
		{
			name: "字段缺少 type",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      nullable: false
`,
			wantIn: "缺少 type",
		},
		{
			name: "类型括号未闭合",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varchar(8
      nullable: false
`,
			wantIn: "格式不正确",
		},
		{
			name: "enum 没写取值列表",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: enum
      nullable: false
`,
			wantIn: "必须声明取值列表",
		},
		{
			name: "索引名重复",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: int
      nullable: false
  indexes:
    dup:
      columns: [a]
  unique_indexes:
    dup:
      columns: [a]
`,
			wantIn: "重复定义",
		},
		{
			name: "普通索引写了 with_parser",
			yml: `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varchar(32)
      nullable: false
  indexes:
    idx_a:
      columns: [a]
      with_parser: ngram
`,
			wantIn: "只有全文索引才支持 with_parser",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := yamlFileToOrderedJSON([]byte(c.yml))
			if err != nil {
				// yaml 本身就非法也算捕获到了
				if !strings.Contains(err.Error(), c.wantIn) {
					t.Fatalf("yaml 阶段报错 %v，未包含 %q", err, c.wantIn)
				}
				return
			}
			_, _, err = parseTableDoc(doc, "test.yml")
			if err == nil {
				t.Fatalf("应该报错但没有")
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("报错 %v 未包含 %q", err, c.wantIn)
			}
		})
	}
}

// TestUnknownKeysWarnNotFail 写错配置名只告警不报错，但必须让人看见：
// 完全静默的话，把 indexes 写成 index 就悄悄失效了，要到线上查询变慢才会发现
func TestUnknownKeysWarnNotFail(t *testing.T) {
	_, warns := parseWithWarns(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
    engin: InnoDB
  fields:
    a:
      type: int
      nullable: false
      defalut: '0'
`)
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "engin") {
		t.Errorf("options 下写错的配置项应被告警，实际：%v", warns)
	}
	if !strings.Contains(joined, "defalut") {
		t.Errorf("字段下写错的配置项应被告警，实际：%v", warns)
	}
}

// TestNonBuiltinFulltextParserWarns 非内置分词器要告警：
// 建索引语句会原样带上它，没装插件的话数据库会直接拒绝
func TestNonBuiltinFulltextParserWarns(t *testing.T) {
	_, warns := parseWithWarns(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: text
      nullable: false
  fulltext_indexes:
    ft_a:
      with_parser: simple
      columns: [a]
`)
	if !strings.Contains(strings.Join(warns, "\n"), "simple") {
		t.Errorf("非内置分词器应告警，实际：%v", warns)
	}

	// 内置的不该告警
	_, warns = parseWithWarns(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: text
      nullable: false
  fulltext_indexes:
    ft_a:
      with_parser: ngram
      columns: [a]
`)
	if strings.Contains(strings.Join(warns, "\n"), "不是 MySQL 内置") {
		t.Errorf("ngram 是内置分词器，不该告警，实际：%v", warns)
	}
}

// TestUnknownTypeWarnsNotFails 类型注册表认不出来的类型不能直接报错：
// MySQL 方言众多，注册表未必穷尽，认不出来就原样透传，
// 硬报错会把在跑的业务弄挂。但必须告警，否则写错类型名要到数据库才暴露。
func TestUnknownTypeWarnsNotFails(t *testing.T) {
	tbl, warns := parseWithWarns(t, `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
  fields:
    a:
      type: varcharrr(8)
      nullable: false
`)
	c, _ := tbl.column("a")
	if c.Decl != "varcharrr(8)" {
		t.Errorf("未知类型应原样透传，实际 %q", c.Decl)
	}
	if !strings.Contains(strings.Join(warns, "\n"), "varcharrr") {
		t.Errorf("未知类型应告警，实际：%v", warns)
	}
}

// TestBuildSchemaJSONRoundTrip 编译产物写出去再读回来，模型必须完全一致
func TestBuildSchemaJSONRoundTrip(t *testing.T) {
	src := `
Table:
  type: entity
  table: demo
  sharding_tables: demo_01,demo_02
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
    comment: 演示表
  indexes:
    idx_b:
      columns: [zebra]
    idx_a:
      columns: [apple]
  unique_indexes:
    unq_a:
      columns: [apple]
  id:
    id:
      type: integer unsigned
      nullable: false
      generator: AUTO_INCREMENT
      comment: 主键
  fields:
    zebra:
      type: varchar
      nullable: false
      default: 007
      comment: 斑马
    apple:
      type: int
      nullable: true
      comment: ''
`
	tbl := mustParse(t, src)
	doc, err := buildSchemaJSON([]*ymlTable{tbl})
	if err != nil {
		t.Fatalf("buildSchemaJSON 失败: %v", err)
	}

	// 顶层形状是 {"声明的表名": 文档}，loadFromBuildSchema 按这个形状回读
	if !strings.HasPrefix(doc, `{"demo":{"Table":`) {
		n := 60
		if len(doc) < n {
			n = len(doc)
		}
		t.Errorf("编译产物顶层形状不对：%s", doc[:n])
	}

	// loadFromBuildSchema 就是取每个顶层键的原文再交给 parseTableDoc，这里照做一遍
	back := gjson.Parse(doc).Get("demo")
	if !back.Exists() {
		t.Fatalf("编译产物里没有 demo 这个键：%s", doc)
	}
	again, _, err := parseTableDoc(back.Raw, "demo.value")
	if err != nil {
		t.Fatalf("回读编译产物失败: %v", err)
	}
	// 逐列比对顺序与内容
	if len(again.Columns) != len(tbl.Columns) {
		t.Fatalf("回读后列数量 %d，原为 %d", len(again.Columns), len(tbl.Columns))
	}
	for i := range tbl.Columns {
		a, b := tbl.Columns[i], again.Columns[i]
		if a.Name != b.Name || a.Decl != b.Decl || a.Default != b.Default ||
			a.DefaultSet != b.DefaultSet || a.Comment != b.Comment ||
			a.Nullable != b.Nullable || a.Generator != b.Generator || a.FromID != b.FromID {
			t.Errorf("第 %d 列回读后不一致:\n原 %+v\n新 %+v", i, a, b)
		}
	}
	if strings.Join(again.PrimaryColumns, ",") != strings.Join(tbl.PrimaryColumns, ",") {
		t.Errorf("主键回读不一致")
	}
	if len(again.Indexes) != 2 || again.Indexes[0].Name != "idx_b" {
		t.Errorf("索引顺序回读后丢失：%+v", again.Indexes)
	}
	if again.ShardingTables == nil || len(again.ShardingTables) != 2 {
		t.Errorf("分表配置回读后丢失：%+v", again.ShardingTables)
	}
}
