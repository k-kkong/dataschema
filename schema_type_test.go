package dataschema

import (
	"strings"
	"testing"
)

// 下面三个 baseline* 函数是"类型映射的参照实现"，用简单的 switch 写死，
// 不参与任何生产逻辑，只在本文件里当基准用。
//
// 为什么要留这么一份：getTypeYml2SqlMapping 的输出直接决定建表/改表语句里的类型文本，
// 而它同时被 yml 声明与 INFORMATION_SCHEMA 回读两侧使用，输出一变，
// 已经上线的表就会在每次执行时被判定为"需要 MODIFY"。
// 所以这里的断言是：任何输入下，查注册表的实现与参照实现必须给出同样的结果；
// 要改映射就等于要改线上行为，必须同步更新参照实现。

func baselineGetTypeYml2SqlMapping(t string) string {
	t = strings.ToLower(t)
	value := t
	switch t {
	case "bigint":
		value = "bigint(20)"
	case "binary":
		value = "binary(1)"
	case "bit":
		value = "bit(1)"
	case "boolean", "bool":
		value = "tinyint(1)"
	case "char":
		value = "char(1)"
	case "decimal":
		value = "decimal(10,0)"
	case "int", "integer":
		value = "int(11)"
	case "mediumint":
		value = "mediumint(9)"
	case "smallint":
		value = "smallint(6)"
	case "tinyint":
		value = "tinyint(4)"
	case "varchar":
		value = "varchar(255)"
	case "year":
		value = "year(4)"
	case "integer unsigned", "int unsigned":
		value = "int(10) unsigned"
	}
	return value
}

func baselineIsNoDefaultType(dataType string) bool {
	switch dataType {
	case "tinytext", "mediumtext", "text", "longtext", "blob", "tinyblob",
		"mediumblob", "longblob":
		return true
	}
	return false
}

func baselineVerifyDataType(dataType string) int {
	switch dataType {
	case "int", "integer", "tinyint", "smallint", "mediumint", "bigint",
		"int unsigned", "integer unsigned", "tinyint unsigned", "smallint unsigned",
		"mediumint unsigned", "bigint unsigned", "bit":
		return 1
	case "float", "double", "decimal":
		return 2
	case "bool":
		return 3
	case "enum", "set", "varchar", "char", "tinytext", "mediumtext", "text", "longtext", "blob", "tinyblob",
		"mediumblob", "longblob", "binary", "varbinary":
		return 4
	case "date", "datetime", "timestamp", "time":
		return 5
	}
	return 0
}

// mappingCompatibilityInputs 覆盖 yml 里可能出现的写法与 INFORMATION_SCHEMA 回读的写法
var mappingCompatibilityInputs = []string{
	"", " ", "int", "integer", "INT", "Integer", "tinyint", "smallint", "mediumint", "bigint",
	"int unsigned", "integer unsigned", "tinyint unsigned", "smallint unsigned",
	"mediumint unsigned", "bigint unsigned", "bit",
	"float", "double", "decimal", "bool", "boolean",
	"enum", "set", "varchar", "char", "tinytext", "mediumtext", "text", "longtext",
	"blob", "tinyblob", "mediumblob", "longblob", "binary", "varbinary",
	"date", "datetime", "timestamp", "time", "year", "json",
	// 带参数的写法（yml 允许显式指定）
	"varchar(255)", "varchar(11)", "varchar(222)", "char(32)", "decimal(10,2)", "decimal(10,0)",
	"int(11)", "int(10) unsigned", "bigint(20)", "bigint(20) unsigned", "tinyint(1)", "tinyint(4)",
	"datetime(3)", "timestamp(6)", "time(2)", "bit(8)", "binary(16)", "varbinary(16)",
	"float(10,2)", "year(4)", "enum('a','b')", "set('x','y','z')",
	// 数据库回读的 COLUMN_TYPE 形态
	"int(10) unsigned", "longtext", "mediumtext", "double", "geometry", "point",
	// 非法/未知写法，必须原样透传，不能 panic
	"foobar", "varchar(", "varchar(255", "enum('a)",
}

func TestGetTypeYml2SqlMappingMatchesBaseline(t *testing.T) {
	for _, in := range mappingCompatibilityInputs {
		got := getTypeYml2SqlMapping(in)
		want := baselineGetTypeYml2SqlMapping(in)
		// 唯一允许的偏差是大小写：显式带参数时要保留参数原文的大小写，
		// 整体小写会把 enum('A') 的取值改坏，因此只在"忽略大小写"的意义上要求一致。
		if !strings.EqualFold(got, want) {
			t.Errorf("getTypeYml2SqlMapping(%q) = %q, 参照实现 = %q", in, got, want)
		}
	}
}

func TestIsNoDefaultTypeMatchesBaseline(t *testing.T) {
	for _, in := range mappingCompatibilityInputs {
		got := isNoDefaultType(in)
		want := baselineIsNoDefaultType(in)
		if got != want {
			// json 与空间类型同样不允许字面默认值，参照实现里没列，属于有意的差异
			if got && !want && (strings.Contains(in, "json") || strings.Contains(in, "geometry") || strings.Contains(in, "point")) {
				continue
			}
			t.Errorf("isNoDefaultType(%q) = %v, 参照实现 = %v", in, got, want)
		}
	}
	for _, must := range []string{"json", "geometry", "point", "longtext", "blob"} {
		if !isNoDefaultType(must) {
			t.Errorf("isNoDefaultType(%q) 应为 true", must)
		}
	}
}

// TestVerifyDataTypeMatchesBaseline 参照实现是精确匹配裸类型名，
// varchar(255)、INT、boolean 这类写法一律返回 0（未知），查注册表的实现要能正确分类。
// 因此这里只要求：参照实现能识别出来的，分类必须完全一致；
// 参照实现返回未知的，只要注册表认识这个类型，就不允许再判成未知。
func TestVerifyDataTypeMatchesBaseline(t *testing.T) {
	for _, in := range mappingCompatibilityInputs {
		got, want := verifyDataType(in), baselineVerifyDataType(in)
		if want != 0 && got != want {
			t.Errorf("verifyDataType(%q) = %d, 参照实现 = %d", in, got, want)
		}
		if want == 0 && got == 0 && isKnownType(in) {
			t.Errorf("verifyDataType(%q) 仍被判为未知类型", in)
		}
	}
}

// isKnownType 判断注册表是否认识该类型
func isKnownType(raw string) bool {
	p, ok := parseColumnType(raw)
	if !ok {
		return false
	}
	_, known := lookupType(p.base)
	return known
}

// TestNormalizeColumnTypeEquivalence 校验归一化能消除 yml 写法与数据库回读写法的差异，
// 这是"库结构与 yml 一致时不再产生任何 SQL"的前提。
func TestNormalizeColumnTypeEquivalence(t *testing.T) {
	equalPairs := [][2]string{
		{"int", "int(11)"},
		{"integer", "int"},
		{"int unsigned", "int(10) unsigned"},
		{"integer unsigned", "int unsigned"},
		{"bigint", "bigint(20)"},
		{"bigint unsigned", "bigint(20) unsigned"},
		{"tinyint", "tinyint(4)"},
		{"smallint", "smallint(6)"},
		{"mediumint", "mediumint(9)"},
		{"bool", "tinyint(1)"},
		{"boolean", "tinyint(1)"},
		{"varchar", "varchar(255)"},
		{"char", "char(1)"},
		{"decimal", "decimal(10,0)"},
		{"decimal(10)", "decimal(10,0)"},
		{"decimal(10, 2)", "decimal(10,2)"},
		{"year", "year(4)"},
		{"datetime", "datetime(0)"},
		{"timestamp", "timestamp(0)"},
		{"bit", "bit(1)"},
		{"binary", "binary(1)"},
		{"double precision", "double"},
		{"numeric", "decimal"},
		{"INT", "int(11)"},
		{"text", "text"},
		{"json", "json"},
		{"varbinary(16)", "varbinary(16)"},
		{"datetime(3)", "datetime(3)"},
		{"enum('a','b')", "enum('a','b')"},
	}
	for _, pair := range equalPairs {
		l, r := normalizeColumnType(pair[0]), normalizeColumnType(pair[1])
		if l != r {
			t.Errorf("归一化后应等价: %q -> %q, %q -> %q", pair[0], l, pair[1], r)
		}
		if !sameColumnType(pair[0], pair[1]) {
			t.Errorf("sameColumnType(%q, %q) 应为 true", pair[0], pair[1])
		}
	}

	diffPairs := [][2]string{
		{"int", "bigint"},
		{"tinyint", "tinyint(1)"}, // tinyint(1) 承载 bool 语义，不能与 tinyint 混同
		{"varchar(255)", "varchar(256)"},
		{"text", "longtext"},
		{"datetime", "timestamp"},
		{"datetime", "datetime(3)"},
		{"decimal(10,2)", "decimal(10,0)"},
		{"enum('a','b')", "enum('b','a')"},
		{"int", "int unsigned"},
		{"char(10)", "varchar(10)"},
	}
	for _, pair := range diffPairs {
		if sameColumnType(pair[0], pair[1]) {
			t.Errorf("归一化后不应等价: %q(%q) 与 %q(%q)",
				pair[0], normalizeColumnType(pair[0]), pair[1], normalizeColumnType(pair[1]))
		}
	}
}

func TestNormalizeColumnTypeIdempotent(t *testing.T) {
	for _, in := range mappingCompatibilityInputs {
		once := normalizeColumnType(in)
		if twice := normalizeColumnType(once); once != twice {
			t.Errorf("归一化不幂等: %q -> %q -> %q", in, once, twice)
		}
	}
}

func TestValidateColumnType(t *testing.T) {
	mustOK := []string{
		"int", "integer unsigned", "varchar", "varchar(255)", "text", "longtext", "json",
		"datetime", "datetime(3)", "decimal(10,2)", "bool", "enum('a','b')", "varbinary(16)",
		"bigint(20) unsigned", "foobar", // 未知类型放行并告警，不直接报错
	}
	for _, in := range mustOK {
		if _, _, err := validateColumnType(in); err != nil {
			t.Errorf("validateColumnType(%q) 不应报错: %v", in, err)
		}
	}
	if _, warns, err := validateColumnType("foobar"); err != nil || len(warns) == 0 {
		t.Errorf("未知类型应给出告警, warns=%v err=%v", warns, err)
	}

	mustFail := []string{
		"", "varbinary", "enum", "set", "varchar(", "text(10)", "json(1)",
		"varchar(70000)", "char(300)", "decimal(70,2)", "decimal(10,40)",
		"decimal(10,11)", "datetime(9)", "bit(65)", "enum(a,b)", "int(11,2)",
	}
	for _, in := range mustFail {
		if _, _, err := validateColumnType(in); err == nil {
			t.Errorf("validateColumnType(%q) 应报错", in)
		}
	}
}
