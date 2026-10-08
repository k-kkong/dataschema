package dataschema

import (
	"fmt"
	"strconv"
	"strings"
)

// 本文件是"数据库字段类型"的唯一信息来源。
//
// 类型相关的判断统一收敛为一张注册表 sqlTypeRegistry：
// getTypeYml2SqlMapping / isNoDefaultType / verifyDataType / validateColumnType
// 全部查表实现，不再各写一份 switch。
// 每个类型只在注册表里登记一次（规范名 + 别名 + 参数形态 + 分类 + 是否允许默认值），
// 所以不会出现"json 在这个清单里有、在那个清单里没有"这种漂移。
// 新增一个类型只需要往注册表里加一条，上述函数自动生效。

// typeArgsKind 类型参数的形态，用于校验 yml 中声明的参数是否合法
type typeArgsKind int

const (
	typeArgsNone      typeArgsKind = iota // 不接受参数：text/json/date 等
	typeArgsIntWidth                      // 整数显示宽度：int(11)，MySQL 8 已废弃但仍容忍
	typeArgsLength                        // 长度：char/varchar/binary/varbinary/bit
	typeArgsPrecision                     // 精度：decimal(M,D)/float(M,D)
	typeArgsFsp                           // 小数秒精度：datetime(0~6)
	typeArgsValues                        // 取值列表：enum/set
	typeArgsYear                          // year(4)，归一化时直接丢弃
)

// sqlTypeSpec 一种数据库类型的元信息
type sqlTypeSpec struct {
	canonical    string // 规范类型名，别名会被归一到这个名字
	decl         string // 未声明参数时补齐的完整声明，空表示无需补参数
	declUnsigned string // 未声明参数且带 unsigned 时补齐的完整声明，空表示不特殊处理
	forceArgs    string // 强制参数，例如 bool 恒等于 tinyint(1)
	argsKind     typeArgsKind
	argsRequired bool // 参数必填，缺失即为非法声明
	allowDefault bool // 是否允许字面默认值
	dropWidth    bool // 归一化时是否丢弃显示宽度（整数类与 year）
	argMin       int  // 第一个参数下限
	argMax       int  // 第一个参数上限
	arg2Max      int  // 第二个参数上限（精度的标度）
	category     int  // verifyDataType 返回的分类值，含义见该函数的注释

	defaultArgs         []string // 由 decl 解析而来，归一化时补参用
	defaultArgsUnsigned []string // 由 declUnsigned 解析而来
}

// sqlTypeRegistry 类型注册表，key 为规范名与全部别名
var sqlTypeRegistry = map[string]*sqlTypeSpec{}

func init() {
	specs := []*sqlTypeSpec{
		// ---- 整数族：verifyDataType 分类 1 ----
		{canonical: "tinyint", decl: "tinyint(4)", argsKind: typeArgsIntWidth, allowDefault: true, dropWidth: true, argMin: 0, argMax: 4, category: 1},
		{canonical: "smallint", decl: "smallint(6)", argsKind: typeArgsIntWidth, allowDefault: true, dropWidth: true, argMin: 0, argMax: 6, category: 1},
		{canonical: "mediumint", decl: "mediumint(9)", argsKind: typeArgsIntWidth, allowDefault: true, dropWidth: true, argMin: 0, argMax: 9, category: 1},
		{canonical: "int", decl: "int(11)", declUnsigned: "int(10) unsigned", argsKind: typeArgsIntWidth, allowDefault: true, dropWidth: true, argMin: 0, argMax: 11, category: 1},
		{canonical: "bigint", decl: "bigint(20)", argsKind: typeArgsIntWidth, allowDefault: true, dropWidth: true, argMin: 0, argMax: 20, category: 1},
		{canonical: "bit", decl: "bit(1)", argsKind: typeArgsLength, allowDefault: true, argMin: 1, argMax: 64, category: 1},

		// ---- 浮点/定点族：分类 2 ----
		{canonical: "float", argsKind: typeArgsPrecision, allowDefault: true, argMin: 0, argMax: 65, arg2Max: 30, category: 2},
		{canonical: "double", argsKind: typeArgsPrecision, allowDefault: true, argMin: 0, argMax: 65, arg2Max: 30, category: 2},
		{canonical: "decimal", decl: "decimal(10,0)", argsKind: typeArgsPrecision, allowDefault: true, argMin: 1, argMax: 65, arg2Max: 30, category: 2},

		// ---- 布尔：分类 3，落到 tinyint(1) ----
		{canonical: "tinyint", decl: "tinyint(1)", forceArgs: "1", argsKind: typeArgsLength, allowDefault: true, argMin: 1, argMax: 1, category: 3},

		// ---- 字符串/二进制/枚举族：分类 4 ----
		{canonical: "char", decl: "char(1)", argsKind: typeArgsLength, allowDefault: true, argMin: 0, argMax: 255, category: 4},
		{canonical: "varchar", decl: "varchar(255)", argsKind: typeArgsLength, allowDefault: true, argMin: 1, argMax: 65535, category: 4},
		{canonical: "binary", decl: "binary(1)", argsKind: typeArgsLength, allowDefault: true, argMin: 0, argMax: 255, category: 4},
		{canonical: "varbinary", argsKind: typeArgsLength, argsRequired: true, allowDefault: true, argMin: 1, argMax: 65535, category: 4},
		{canonical: "tinytext", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "mediumtext", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "text", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "longtext", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "tinyblob", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "mediumblob", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "blob", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "longblob", argsKind: typeArgsNone, allowDefault: false, category: 4},
		{canonical: "enum", argsKind: typeArgsValues, argsRequired: true, allowDefault: true, category: 4},
		{canonical: "set", argsKind: typeArgsValues, argsRequired: true, allowDefault: true, category: 4},
		{canonical: "json", argsKind: typeArgsNone, allowDefault: false, category: 4},

		// ---- 时间族：分类 5 ----
		{canonical: "date", argsKind: typeArgsNone, allowDefault: true, category: 5},
		{canonical: "datetime", argsKind: typeArgsFsp, allowDefault: true, argMin: 0, argMax: 6, category: 5},
		{canonical: "timestamp", argsKind: typeArgsFsp, allowDefault: true, argMin: 0, argMax: 6, category: 5},
		{canonical: "time", argsKind: typeArgsFsp, allowDefault: true, argMin: 0, argMax: 6, category: 5},
		{canonical: "year", decl: "year(4)", argsKind: typeArgsYear, allowDefault: true, dropWidth: true, argMin: 0, argMax: 4, category: 5},

		// ---- 空间类型：MySQL 不允许字面默认值 ----
		{canonical: "geometry", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "point", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "linestring", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "polygon", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "multipoint", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "multilinestring", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "multipolygon", argsKind: typeArgsNone, allowDefault: false, category: 6},
		{canonical: "geometrycollection", argsKind: typeArgsNone, allowDefault: false, category: 6},
	}

	// MySQL 官方同义词，统一归一到规范名
	aliasOf := map[string]string{
		"integer":            "int",
		"middleint":          "mediumint",
		"bool":               "bool",
		"boolean":            "bool",
		"numeric":            "decimal",
		"dec":                "decimal",
		"fixed":              "decimal",
		"real":               "double",
		"double precision":   "double",
		"character":          "char",
		"character varying":  "varchar",
		"long varchar":       "mediumtext",
		"long":               "mediumtext",
		"nchar":              "char",
		"national char":      "char",
		"national character": "char",
	}

	byCanonical := map[string]*sqlTypeSpec{}
	for _, s := range specs {
		// bool 这一项的 canonical 是 tinyint，需要单独用 "bool" 作为 key 注册
		if s.forceArgs != "" {
			spec := *s
			spec.defaultArgs = parseArgsFromDecl(spec.decl)
			byCanonical["bool"] = &spec
			continue
		}
		s.defaultArgs = parseArgsFromDecl(s.decl)
		s.defaultArgsUnsigned = parseArgsFromDecl(stripUnsigned(s.declUnsigned))
		byCanonical[s.canonical] = s
		sqlTypeRegistry[s.canonical] = s
	}
	if b, ok := byCanonical["bool"]; ok {
		sqlTypeRegistry["bool"] = b
	}
	for alias, canonical := range aliasOf {
		if spec, ok := byCanonical[canonical]; ok {
			sqlTypeRegistry[alias] = spec
		}
	}
}

// stripUnsigned 去掉声明尾部的 unsigned，便于解析出参数部分
func stripUnsigned(decl string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(decl), "unsigned"))
}

// parseArgsFromDecl 从形如 varchar(255) 的声明里取出参数列表
func parseArgsFromDecl(decl string) []string {
	if decl == "" {
		return nil
	}
	i := strings.Index(decl, "(")
	j := strings.LastIndex(decl, ")")
	if i < 0 || j <= i {
		return nil
	}
	args, _ := splitTypeArgs(decl[i+1 : j])
	return args
}

// lookupType 按名字（含别名）查注册表
func lookupType(name string) (*sqlTypeSpec, bool) {
	spec, ok := sqlTypeRegistry[strings.ToLower(strings.TrimSpace(name))]
	return spec, ok
}

// parsedColumnType 拆解后的类型声明
type parsedColumnType struct {
	base      string   // 原始基础类型名（已小写）
	canonical string   // 归一后的基础类型名
	args      []string // 参数列表，保留原文大小写（enum/set 的取值大小写有意义）
	hasArgs   bool
	unsigned  bool
	zerofill  bool
	rest      []string // 其它无法识别的修饰词，原样保留
}

// String 按 "base(args) unsigned zerofill rest" 的固定顺序还原声明
func (p parsedColumnType) String() string {
	var b strings.Builder
	b.WriteString(p.canonical)
	if p.hasArgs {
		b.WriteString("(")
		b.WriteString(strings.Join(p.args, ","))
		b.WriteString(")")
	}
	if p.unsigned {
		b.WriteString(" unsigned")
	}
	if p.zerofill {
		b.WriteString(" zerofill")
	}
	for _, r := range p.rest {
		b.WriteString(" ")
		b.WriteString(r)
	}
	return b.String()
}

// splitTypeArgs 按逗号切分类型参数，忽略单引号内部的逗号（enum('a','b')）
func splitTypeArgs(text string) ([]string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var (
		args    []string
		cur     strings.Builder
		inQuote bool
	)
	for _, r := range text {
		switch {
		case r == '\'':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			args = append(args, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	args = append(args, strings.TrimSpace(cur.String()))
	if inQuote {
		return args, fmt.Errorf("引号未闭合")
	}
	return args, nil
}

// parseColumnType 把 "int(10) unsigned" 这样的声明拆成结构化数据。
// 括号不匹配等非法写法返回 ok=false，由调用方决定是报错还是原样透传。
func parseColumnType(raw string) (p parsedColumnType, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return p, false
	}
	head, argsText, tail := s, "", ""
	if i := strings.Index(s, "("); i >= 0 {
		j := strings.LastIndex(s, ")")
		if j <= i {
			return p, false
		}
		head = strings.TrimSpace(s[:i])
		argsText = s[i+1 : j]
		tail = strings.TrimSpace(s[j+1:])
		args, err := splitTypeArgs(argsText)
		if err != nil {
			return p, false
		}
		p.args = args
		p.hasArgs = true
	}

	// head 里也可能写着 unsigned，例如 "int unsigned"
	for _, tok := range strings.Fields(strings.ToLower(head)) {
		switch tok {
		case "unsigned":
			p.unsigned = true
		case "zerofill":
			p.zerofill = true
		default:
			if p.base == "" {
				p.base = tok
			} else {
				p.base += " " + tok
			}
		}
	}
	for _, tok := range strings.Fields(strings.ToLower(tail)) {
		switch tok {
		case "unsigned":
			p.unsigned = true
		case "zerofill":
			p.zerofill = true
		default:
			p.rest = append(p.rest, tok)
		}
	}
	if p.base == "" {
		return p, false
	}
	p.canonical = p.base
	if spec, known := lookupType(p.base); known {
		p.canonical = spec.canonical
	}
	return p, true
}

// 获取数据类型yml对应sql的映射
//
// 注意：本函数的输入既可能是 yml 里声明的类型，也可能是 INFORMATION_SCHEMA.COLUMNS
// 回读的 COLUMN_TYPE，两侧都会经过它，因此同一个类型无论从哪一侧进来都必须得到
// 同一个输出，否则会让已经上线的表在每次执行时都产生无意义的 MODIFY COLUMN。
func getTypeYml2SqlMapping(t string) string {
	trimmed := strings.TrimSpace(t)
	if trimmed == "" {
		return strings.ToLower(t)
	}
	p, ok := parseColumnType(trimmed)
	if !ok {
		// 非法声明交给 validateColumnType 去报错，这里只做原样小写返回
		return strings.ToLower(t)
	}
	if p.hasArgs {
		// 已显式声明参数则原样输出，参数部分不做大小写转换。
		// 整体 ToLower 会把 enum('A') 的取值也改成小写，
		// 等于悄悄改掉了枚举语义，所以这里保留参数原文。
		return trimmed
	}
	spec, known := lookupType(p.base)
	if !known {
		return p.String()
	}
	if p.unsigned && spec.declUnsigned != "" && !p.zerofill && len(p.rest) == 0 {
		return spec.declUnsigned
	}
	if p.unsigned || p.zerofill || len(p.rest) > 0 || spec.decl == "" {
		return p.String()
	}
	return spec.decl
}

// normalizeColumnType 把类型声明归一化成"唯一等价形式"，仅用于结构比对。
//
// yml 声明与数据库回读的写法常常不同（int vs int(11)、integer vs int、
// decimal(10) vs decimal(10,0)、MySQL 5.7 与 8.0 对整数显示宽度的处理也不一样），
// 归一化后两侧一致，才能保证结构比对是幂等的：库结构与 yml 一致时不产生任何 SQL。
func normalizeColumnType(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	p, ok := parseColumnType(trimmed)
	if !ok {
		return strings.ToLower(trimmed)
	}
	spec, known := lookupType(p.base)
	if !known {
		return p.String()
	}
	switch {
	case spec.forceArgs != "":
		// bool 恒定等价于 tinyint(1)
		p.args = []string{spec.forceArgs}
		p.hasArgs = true
	case !p.hasArgs:
		if p.unsigned && len(spec.defaultArgsUnsigned) > 0 {
			p.args = append([]string{}, spec.defaultArgsUnsigned...)
		} else if len(spec.defaultArgs) > 0 {
			p.args = append([]string{}, spec.defaultArgs...)
		}
		p.hasArgs = len(p.args) > 0
	}

	if p.hasArgs {
		switch {
		case spec.argsKind == typeArgsValues:
			// enum/set 的取值大小写有意义，只去除首尾空白
		case spec.dropWidth:
			// 整数显示宽度在 MySQL 8 已废弃，两侧一律丢弃；
			// 但 tinyint(1) 承载 bool 语义，MySQL 会保留，必须区别对待。
			if !(p.canonical == "tinyint" && len(p.args) == 1 && p.args[0] == "1") {
				p.args = nil
				p.hasArgs = false
			}
		case p.canonical == "decimal":
			switch len(p.args) {
			case 1:
				p.args = append(p.args, "0")
			}
		case p.canonical == "datetime" || p.canonical == "timestamp" || p.canonical == "time":
			if len(p.args) == 1 && p.args[0] == "0" {
				p.args = nil
				p.hasArgs = false
			}
		}
	}
	return p.String()
}

// sameColumnType 判断 yml 声明的类型与数据库回读的类型是否等价
func sameColumnType(ymlDecl, dbDecl string) bool {
	return normalizeColumnType(ymlDecl) == normalizeColumnType(dbDecl)
}

// isNoDefaultType 判断该类型是否不允许字面默认值。
// json 与空间类型都不允许字面默认值，否则会被拼成 DEFAULT 'xxx' 这种非法 SQL。
func isNoDefaultType(dataType string) bool {
	p, ok := parseColumnType(dataType)
	if !ok {
		return false
	}
	spec, known := lookupType(p.base)
	if !known {
		return false
	}
	return !spec.allowDefault
}

// isTemporalType 判断是否时间类型（date/datetime/timestamp/time/year）。
// 只有时间类型上的裸 CURRENT_TIMESTAMP 才是表达式默认值，
// varchar 上的 'CURRENT_TIMESTAMP' 只是个普通字符串，两者不能混为一谈。
func isTemporalType(decl string) bool {
	p, ok := parseColumnType(decl)
	if !ok {
		return false
	}
	spec, known := lookupType(p.canonical)
	return known && spec.category == 5
}

// verifyDataType 返回类型的分类：
// 1 整数、2 浮点定点、3 布尔、4 字符串与枚举与 json、5 时间、6 空间、0 未知。
// 分类值取自注册表的 category 字段，json/year/空间类型都有各自的归类，
// 只有注册表认不出来的类型才返回 0。
func verifyDataType(dataType string) int {
	p, ok := parseColumnType(dataType)
	if !ok {
		return 0
	}
	spec, known := lookupType(p.base)
	if !known {
		return 0
	}
	return spec.category
}

// validateColumnType 校验 yml 中声明的字段类型，返回建表用的类型声明。
// 校验放在解析阶段而不是执行阶段，避免把明显写错的类型带到数据库才报错。
func validateColumnType(rawType string) (decl string, warns []string, err error) {
	trimmed := strings.TrimSpace(rawType)
	if trimmed == "" {
		return "", nil, fmt.Errorf("缺少 type 声明")
	}
	p, ok := parseColumnType(trimmed)
	if !ok {
		return "", nil, fmt.Errorf("类型声明 %q 格式不正确（括号或引号未闭合）", rawType)
	}
	decl = getTypeYml2SqlMapping(trimmed)

	spec, known := lookupType(p.base)
	if !known {
		// 未知类型不直接报错：MySQL 方言众多，注册表未必穷尽，
		// 原样透传并给出告警，把判断权留给使用者。
		warns = append(warns, fmt.Sprintf("未知的数据库类型 %q，将原样输出，请自行确认数据库是否支持", rawType))
		return decl, warns, nil
	}

	if p.argsRequiredOr(spec) && !p.hasArgs {
		switch spec.argsKind {
		case typeArgsValues:
			return "", nil, fmt.Errorf("类型 %q 必须声明取值列表，例如 %s('a','b')", rawType, p.canonical)
		case typeArgsLength:
			return "", nil, fmt.Errorf("类型 %q 必须声明长度，例如 %s(16)", rawType, p.canonical)
		default:
			return "", nil, fmt.Errorf("类型 %q 必须声明参数", rawType)
		}
	}
	if spec.argsKind == typeArgsNone && p.hasArgs {
		return "", nil, fmt.Errorf("类型 %q 不接受参数", rawType)
	}
	if !p.hasArgs {
		return decl, warns, nil
	}

	switch spec.argsKind {
	case typeArgsValues:
		for _, a := range p.args {
			if a == "" {
				return "", nil, fmt.Errorf("类型 %q 的取值列表存在空值", rawType)
			}
			if len(a) < 2 || a[0] != '\'' || a[len(a)-1] != '\'' {
				return "", nil, fmt.Errorf("类型 %q 的取值 %s 必须是用单引号包裹的字符串", rawType, a)
			}
		}
	case typeArgsIntWidth, typeArgsLength, typeArgsFsp, typeArgsYear:
		if len(p.args) != 1 {
			return "", nil, fmt.Errorf("类型 %q 只接受一个参数", rawType)
		}
		n, convErr := strconv.Atoi(p.args[0])
		if convErr != nil {
			return "", nil, fmt.Errorf("类型 %q 的参数 %s 必须是整数", rawType, p.args[0])
		}
		if n < spec.argMin || n > spec.argMax {
			return "", nil, fmt.Errorf("类型 %q 的参数 %d 超出允许范围 [%d,%d]", rawType, n, spec.argMin, spec.argMax)
		}
	case typeArgsPrecision:
		if len(p.args) > 2 {
			return "", nil, fmt.Errorf("类型 %q 最多接受两个参数", rawType)
		}
		nums := make([]int, 0, len(p.args))
		for _, a := range p.args {
			n, convErr := strconv.Atoi(a)
			if convErr != nil {
				return "", nil, fmt.Errorf("类型 %q 的参数 %s 必须是整数", rawType, a)
			}
			nums = append(nums, n)
		}
		if nums[0] < spec.argMin || nums[0] > spec.argMax {
			return "", nil, fmt.Errorf("类型 %q 的精度 %d 超出允许范围 [%d,%d]", rawType, nums[0], spec.argMin, spec.argMax)
		}
		if len(nums) == 2 {
			if nums[1] < 0 || nums[1] > spec.arg2Max {
				return "", nil, fmt.Errorf("类型 %q 的标度 %d 超出允许范围 [0,%d]", rawType, nums[1], spec.arg2Max)
			}
			if nums[1] > nums[0] {
				return "", nil, fmt.Errorf("类型 %q 的标度 %d 不能大于精度 %d", rawType, nums[1], nums[0])
			}
		}
	}

	if (p.unsigned || p.zerofill) && spec.category != 1 && spec.category != 2 {
		warns = append(warns, fmt.Sprintf("类型 %q 不是数值类型，unsigned/zerofill 修饰会被数据库忽略", rawType))
	}
	return decl, warns, nil
}

// argsRequiredOr 判断本次声明是否必须带参数
func (p parsedColumnType) argsRequiredOr(spec *sqlTypeSpec) bool {
	if spec == nil {
		return false
	}
	if spec.forceArgs != "" {
		return false
	}
	return spec.argsRequired
}
