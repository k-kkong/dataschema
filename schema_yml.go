package dataschema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"
)

// 本文件负责把 yaml 表结构配置解析成内存模型。
//
// 解析走的是 yaml.Node -> 保序 JSON -> 一次性解析成结构体 这条路，
// 解析完成后所有取值都走 Go map/slice，不拼 gjson 字符串路径。
// 之所以中间要经过一份自己拼的 JSON，而不是直接 yaml.Unmarshal 到 map，
// 是因为下面三件事必须同时成立：
//  1. 保留 yml 里的书写顺序，建表语句的列顺序 = id 区在前 + fields 区按书写顺序
//     （json.Marshal 会对 map 的 key 排序，所以中间产物必须是保序的 JSON 文本）；
//  2. 保留标量原文，default: 007 不会被改写成 7、default: 1.10 不会被改写成 1.1；
//  3. 字段名里含 . * ? # @ 这些字符时也能取到正确的值。

// indexKind 索引类型
type indexKind int

const (
	indexKindNormal   indexKind = iota // indexes
	indexKindUnique                    // unique_indexes
	indexKindFulltext                  // fulltext_indexes
)

// String 索引类型的可读名字，用于变更报告
func (k indexKind) String() string {
	switch k {
	case indexKindUnique:
		return "唯一索引"
	case indexKindFulltext:
		return "全文索引"
	default:
		return "索引"
	}
}

// ymlColumn yml 中一个字段（列）的定义
type ymlColumn struct {
	Name        string // 字段名
	Type        string // yml 中的原始类型声明
	Decl        string // 建表实际使用的类型声明
	Nullable    bool   // 是否可空
	Default     string // 默认值原文
	DefaultSet  bool   // 是否显式声明了 default（声明为空串与未声明是两回事）
	DefaultNull bool   // default 写成了 yaml 空值（"default:" 后面什么都不写），语义有歧义
	Comment     string // 备注
	Generator   string // 自动生成声明，例如 AUTO_INCREMENT
	FromID      bool   // 是否来自主键区(id)
	Line        int    // 源文件行号，报错定位用
}

// ymlIndex yml 中一条索引的定义
type ymlIndex struct {
	Name       string
	Columns    []string
	WithParser string // 全文索引分词器
	Kind       indexKind
	Line       int
}

// ymlTable 一张表的完整定义
type ymlTable struct {
	SourceFile      string // 来源配置文件，分表展开后每个分表都指向同一个文件
	Type            string // Table.type
	DeclaredName    string // yml 中声明的表名
	Name            string // 实际参与建表的表名（分表展开后的名字）
	ShardingTables  []string
	Charset         string
	Collate         string
	Comment         string
	Columns         []*ymlColumn // 有序：id 区在前，fields 区在后，各自保持 yml 书写顺序
	columnMap       map[string]*ymlColumn
	PrimaryColumns  []string // 主键列，有序
	HasPrimaryDecl  bool     // 是否显式声明了 primary_indexes
	Indexes         []*ymlIndex
	UniqueIndexes   []*ymlIndex
	FulltextIndexes []*ymlIndex
	Raw             string // 保序 JSON 原文，写编译产物时使用
	Line            int
}

// column 按名字取字段定义
func (t *ymlTable) column(name string) (*ymlColumn, bool) {
	if t.columnMap == nil {
		return nil, false
	}
	c, ok := t.columnMap[name]
	return c, ok
}

// allIndexes 按 普通索引、唯一索引、全文索引 的顺序返回全部索引
func (t *ymlTable) allIndexes() []*ymlIndex {
	out := make([]*ymlIndex, 0, len(t.Indexes)+len(t.UniqueIndexes)+len(t.FulltextIndexes))
	out = append(out, t.Indexes...)
	out = append(out, t.UniqueIndexes...)
	out = append(out, t.FulltextIndexes...)
	return out
}

// yamlKV 一个保序的键值对
type yamlKV struct {
	key   string
	value *yaml.Node
	line  int
}

// yamlFileToOrderedJSON 把 yaml 文件内容转成"保持书写顺序"的 JSON 文本。
// 所有标量一律按原文输出为 JSON 字符串，避免 yaml 的隐式类型转换改写默认值。
func yamlFileToOrderedJSON(data []byte) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := orderedJSONFromNode(&root, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

func orderedJSONFromNode(n *yaml.Node, b *strings.Builder) error {
	if n == nil {
		b.WriteString("null")
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			b.WriteString("null")
			return nil
		}
		return orderedJSONFromNode(n.Content[0], b)
	case yaml.AliasNode:
		return orderedJSONFromNode(n.Alias, b)
	case yaml.MappingNode:
		kvs, err := flattenMappingNode(n)
		if err != nil {
			return err
		}
		b.WriteByte('{')
		for i, kv := range kvs {
			if i > 0 {
				b.WriteByte(',')
			}
			kj, err := json.Marshal(kv.key)
			if err != nil {
				return err
			}
			b.Write(kj)
			b.WriteByte(':')
			if err := orderedJSONFromNode(kv.value, b); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		b.WriteByte('[')
		for i, item := range n.Content {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := orderedJSONFromNode(item, b); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			// 空值（key: 后面什么都不写）输出 JSON null，
			// 下游据此区分"声明了但没给值"与"根本没声明这个 key"。
			b.WriteString("null")
			return nil
		}
		sj, err := json.Marshal(n.Value)
		if err != nil {
			return err
		}
		b.Write(sj)
		return nil
	default:
		return fmt.Errorf("第 %d 行存在不支持的 yaml 节点", n.Line)
	}
}

func isMergeKey(n *yaml.Node) bool {
	return n.Tag == "!!merge" || n.Value == "<<"
}

// flattenMappingNode 展开映射节点，处理 yaml 的合并键(<<)并检测重复键
func flattenMappingNode(n *yaml.Node) ([]yamlKV, error) {
	var (
		out       []yamlKV
		indexOf   = map[string]int{}
		fromMerge = map[string]bool{}
	)
	put := func(k string, v *yaml.Node, line int) error {
		if i, ok := indexOf[k]; ok {
			return fmt.Errorf("第 %d 行重复定义了配置项 '%s'（首次定义在第 %d 行）", line, k, out[i].line)
		}
		indexOf[k] = len(out)
		out = append(out, yamlKV{key: k, value: v, line: line})
		return nil
	}

	// 先放合并键带进来的内容，再放显式声明的键，保证显式声明的优先级更高
	for i := 0; i+1 < len(n.Content); i += 2 {
		kn, vn := n.Content[i], n.Content[i+1]
		if !isMergeKey(kn) {
			continue
		}
		merged, err := mergeKeyValues(vn)
		if err != nil {
			return nil, err
		}
		for _, kv := range merged {
			if _, ok := indexOf[kv.key]; ok {
				out[indexOf[kv.key]].value = kv.value
				continue
			}
			if err := put(kv.key, kv.value, kv.line); err != nil {
				return nil, err
			}
			fromMerge[kv.key] = true
		}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		kn, vn := n.Content[i], n.Content[i+1]
		if isMergeKey(kn) {
			continue
		}
		if kn.Kind == yaml.AliasNode && kn.Alias != nil {
			kn = kn.Alias
		}
		// 显式声明覆盖合并键带进来的同名项，这是 yaml 合并键的标准语义，
		// 不能当成重复定义报错；只有两个显式声明撞在一起才算重复。
		if idx, ok := indexOf[kn.Value]; ok && fromMerge[kn.Value] {
			out[idx].value = vn
			fromMerge[kn.Value] = false
			continue
		}
		if err := put(kn.Value, vn, kn.Line); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// mergeKeyValues 取出合并键引用的映射内容
func mergeKeyValues(n *yaml.Node) ([]yamlKV, error) {
	if n == nil {
		return nil, nil
	}
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n == nil {
		return nil, nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		return flattenMappingNode(n)
	case yaml.SequenceNode:
		var (
			out  []yamlKV
			seen = map[string]bool{}
		)
		for _, item := range n.Content {
			kvs, err := mergeKeyValues(item)
			if err != nil {
				return nil, err
			}
			for _, kv := range kvs {
				if seen[kv.key] {
					continue
				}
				seen[kv.key] = true
				out = append(out, kv)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("第 %d 行的合并键 << 只能引用映射", n.Line)
	}
}

// rawTextOf 取出节点的原始文本。
// 编译产物里所有标量都写成字符串；这里同时接受原生的 bool/number，
// 所以产物文件即使被别的工具改写过，回读也不会出错。
func rawTextOf(r gjson.Result) string {
	switch r.Type {
	case gjson.String:
		return r.Str
	case gjson.True:
		return "true"
	case gjson.False:
		return "false"
	case gjson.Number:
		return strings.TrimSpace(r.Raw)
	default:
		return ""
	}
}

// parseBoolText 解析布尔配置。
// 兼容 gjson 的 Bool() 行为（内部就是 strconv.ParseBool），并额外接受 yes/no/on/off。
func parseBoolText(raw string) (bool, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "":
		return false, nil
	case "yes", "y", "on":
		return true, nil
	case "no", "n", "off":
		return false, nil
	}
	b, err := parseBoolStrict(s)
	if err != nil {
		return false, fmt.Errorf("'%s' 不是合法的布尔值，应写 true 或 false", raw)
	}
	return b, nil
}

func parseBoolStrict(s string) (bool, error) {
	switch s {
	case "1", "t", "true":
		return true, nil
	case "0", "f", "false":
		return false, nil
	}
	return false, fmt.Errorf("invalid bool %q", s)
}

// 各类节点允许出现的配置项，写错名字时给出告警（例如把 indexes 写成 index 会静默失效）
var (
	allowedTableKeys   = []string{"type", "table", "sharding_tables", "options", "id", "fields", "indexes", "unique_indexes", "fulltext_indexes", "primary_indexes"}
	allowedOptionKeys  = []string{"charset", "collate", "comment"}
	allowedColumnKeys  = []string{"type", "nullable", "default", "comment", "generator"}
	allowedIndexKeys   = []string{"columns", "with_parser"}
	allowedPrimaryKeys = []string{"columns", "name"}
)

func unknownKeys(node gjson.Result, allowed []string) []string {
	allow := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allow[k] = true
	}
	var out []string
	node.ForEach(func(key, _ gjson.Result) bool {
		if !allow[key.String()] {
			out = append(out, key.String())
		}
		return true
	})
	return out
}

// parseTableDoc 把一份文档 JSON 解析成表模型。
// 这里只做解析与静态校验，不接触数据库。
func parseTableDoc(docJSON, source string) (*ymlTable, []string, error) {
	root := gjson.Parse(docJSON)
	tblNode := root.Get("Table")
	if !tblNode.Exists() || !tblNode.IsObject() {
		return nil, nil, fmt.Errorf("配置文件不正确：缺少 Table 节点")
	}

	var warns []string
	warn := func(format string, args ...interface{}) {
		warns = append(warns, fmt.Sprintf(format, args...))
	}
	if ks := unknownKeys(tblNode, allowedTableKeys); len(ks) > 0 {
		warn("Table 下存在无法识别的配置项 %v，会被忽略（可用项：%s）", ks, strings.Join(allowedTableKeys, "/"))
	}

	t := &ymlTable{
		SourceFile: source,
		Raw:        docJSON,
		Type:       rawTextOf(tblNode.Get("type")),
		// Name 先等于声明的表名，分表展开时再覆盖。
		// 不先赋值的话，任何早于 expandSharding 使用 t.Name 的地方都会静默拼出空表名的 SQL。
		Name:         strings.TrimSpace(rawTextOf(tblNode.Get("table"))),
		DeclaredName: strings.TrimSpace(rawTextOf(tblNode.Get("table"))),
		columnMap:    map[string]*ymlColumn{},
	}

	if opt := tblNode.Get("options"); opt.Exists() && opt.IsObject() {
		if ks := unknownKeys(opt, allowedOptionKeys); len(ks) > 0 {
			warn("options 下存在无法识别的配置项 %v，会被忽略（可用项：%s）", ks, strings.Join(allowedOptionKeys, "/"))
		}
		t.Charset = strings.TrimSpace(rawTextOf(opt.Get("charset")))
		t.Collate = strings.TrimSpace(rawTextOf(opt.Get("collate")))
		t.Comment = rawTextOf(opt.Get("comment"))
	}

	for _, name := range strings.Split(rawTextOf(tblNode.Get("sharding_tables")), ",") {
		if name = strings.TrimSpace(name); name != "" {
			t.ShardingTables = append(t.ShardingTables, name)
		}
	}

	// ---- 主键区 id：顺序即建表顺序，且排在 fields 之前 ----
	var parseErr error
	idNames := []string{}
	if idNode := tblNode.Get("id"); idNode.Exists() && idNode.IsObject() {
		idNode.ForEach(func(key, value gjson.Result) bool {
			name := key.String()
			if _, dup := t.column(name); dup {
				parseErr = fmt.Errorf("主键字段和普通字段重名: %s", name)
				return false
			}
			col, err := parseColumnNode(name, value, true, warn)
			if err != nil {
				parseErr = err
				return false
			}
			t.addColumn(col)
			idNames = append(idNames, name)
			return true
		})
	}
	if parseErr != nil {
		return nil, nil, parseErr
	}

	// ---- 普通字段区 fields ----
	if fieldsNode := tblNode.Get("fields"); fieldsNode.Exists() && fieldsNode.IsObject() {
		fieldsNode.ForEach(func(key, value gjson.Result) bool {
			name := key.String()
			if _, dup := t.column(name); dup {
				parseErr = fmt.Errorf("主键字段和普通字段重名: %s", name)
				return false
			}
			col, err := parseColumnNode(name, value, false, warn)
			if err != nil {
				parseErr = err
				return false
			}
			t.addColumn(col)
			return true
		})
	}
	if parseErr != nil {
		return nil, nil, parseErr
	}

	// ---- 主键列 ----
	if pk := tblNode.Get("primary_indexes"); pk.Exists() {
		t.HasPrimaryDecl = true
		if ks := unknownKeys(pk, allowedPrimaryKeys); len(ks) > 0 {
			warn("primary_indexes 下存在无法识别的配置项 %v，会被忽略", ks)
		}
		for _, c := range pk.Get("columns").Array() {
			if name := strings.TrimSpace(rawTextOf(c)); name != "" {
				t.PrimaryColumns = append(t.PrimaryColumns, name)
			}
		}
	} else {
		t.PrimaryColumns = append(t.PrimaryColumns, idNames...)
	}

	// ---- 索引 ----
	indexNameSeen := map[string]string{}
	for _, group := range []struct {
		key  string
		kind indexKind
		dest *[]*ymlIndex
	}{
		{"indexes", indexKindNormal, &t.Indexes},
		{"unique_indexes", indexKindUnique, &t.UniqueIndexes},
		{"fulltext_indexes", indexKindFulltext, &t.FulltextIndexes},
	} {
		node := tblNode.Get(group.key)
		if !node.Exists() || !node.IsObject() {
			continue
		}
		node.ForEach(func(key, value gjson.Result) bool {
			idx, err := parseIndexNode(key.String(), value, group.kind)
			if err != nil {
				parseErr = err
				return false
			}
			if prev, dup := indexNameSeen[idx.Name]; dup {
				parseErr = fmt.Errorf("索引名 '%s' 重复定义（已出现在 %s 中）", idx.Name, prev)
				return false
			}
			indexNameSeen[idx.Name] = group.key
			*group.dest = append(*group.dest, idx)
			return true
		})
		if parseErr != nil {
			return nil, nil, parseErr
		}
	}

	if err := t.validate(warn); err != nil {
		return nil, nil, err
	}
	return t, warns, nil
}

func (t *ymlTable) addColumn(c *ymlColumn) {
	t.Columns = append(t.Columns, c)
	if t.columnMap == nil {
		t.columnMap = map[string]*ymlColumn{}
	}
	t.columnMap[c.Name] = c
}

func parseColumnNode(name string, node gjson.Result, fromID bool, warn func(format string, args ...interface{})) (*ymlColumn, error) {
	if name == "" {
		return nil, fmt.Errorf("存在未命名的字段定义")
	}
	if !node.IsObject() {
		return nil, fmt.Errorf("字段 '%s' 的定义不正确：必须是一个包含 type 等配置的对象", name)
	}
	if ks := unknownKeys(node, allowedColumnKeys); len(ks) > 0 {
		// 字段级写错配置名（例如把 default 写成 defalut）会静默失效，这里给出告警
		warn("字段 '%s' 下存在无法识别的配置项 %v，会被忽略（可用项：%s）", name, ks, strings.Join(allowedColumnKeys, "/"))
	}
	col := &ymlColumn{
		Name:      name,
		Type:      strings.TrimSpace(rawTextOf(node.Get("type"))),
		Comment:   rawTextOf(node.Get("comment")),
		Generator: rawTextOf(node.Get("generator")),
		FromID:    fromID,
	}
	decl, declWarns, err := validateColumnType(col.Type)
	if err != nil {
		return nil, fmt.Errorf("字段 '%s' %v", name, err)
	}
	for _, w := range declWarns {
		warn("字段 '%s' %s", name, w)
	}
	col.Decl = decl

	if n := node.Get("nullable"); n.Exists() {
		b, err := parseBoolText(rawTextOf(n))
		if err != nil {
			return nil, fmt.Errorf("字段 '%s' 的 nullable %v", name, err)
		}
		col.Nullable = b
	}
	// default 声明为空串与完全没有声明是两回事，必须区分记录
	if d := node.Get("default"); d.Exists() {
		col.DefaultSet = true
		col.Default = rawTextOf(d)
		// "default:" 后面什么都不写时 yaml 得到的是空值，
		// 与显式写 "default: ''" 生成的 SQL 一样，但前者很可能只是漏填了值
		col.DefaultNull = d.Type == gjson.Null
	}
	return col, nil
}

func parseIndexNode(name string, node gjson.Result, kind indexKind) (*ymlIndex, error) {
	if name == "" {
		return nil, fmt.Errorf("%s 存在未命名的索引", kind)
	}
	if !node.IsObject() {
		return nil, fmt.Errorf("%s:'%s' 的定义不正确：必须是一个包含 columns 的对象", kind, name)
	}
	colsNode := node.Get("columns")
	if !colsNode.IsArray() {
		return nil, fmt.Errorf("%s:'%s' columns is not array", kind, name)
	}
	idx := &ymlIndex{Name: name, Kind: kind, WithParser: strings.TrimSpace(rawTextOf(node.Get("with_parser")))}
	for _, c := range colsNode.Array() {
		colName := strings.TrimSpace(rawTextOf(c))
		if colName == "" {
			return nil, fmt.Errorf("%s:'%s' columns 中存在空的列名", kind, name)
		}
		idx.Columns = append(idx.Columns, colName)
	}
	if len(idx.Columns) == 0 {
		return nil, fmt.Errorf("%s:'%s' columns 不能为空", kind, name)
	}
	if kind == indexKindFulltext && idx.WithParser == "" {
		idx.WithParser = "ngram"
	}
	if kind != indexKindFulltext && idx.WithParser != "" {
		return nil, fmt.Errorf("%s:'%s' 只有全文索引才支持 with_parser", kind, name)
	}
	return idx, nil
}

// validate 做不依赖数据库的静态校验
func (t *ymlTable) validate(warn func(format string, args ...interface{})) error {
	if t.DeclaredName == "" {
		return fmt.Errorf("缺少表名")
	}
	if err := checkIdent(t.DeclaredName, "表名"); err != nil {
		return err
	}
	for _, s := range t.ShardingTables {
		if err := checkIdent(s, "分表名"); err != nil {
			return err
		}
		if s == t.DeclaredName {
			warn("分表名 '%s' 与主表名相同，会重复建表", s)
		}
	}
	if len(t.Columns) == 0 {
		return fmt.Errorf("表 '%s' 没有声明任何字段（id 与 fields 至少要有一个字段）", t.DeclaredName)
	}

	// 主键列必须存在
	for _, pk := range t.PrimaryColumns {
		if _, ok := t.column(pk); !ok {
			return fmt.Errorf("表 '%s' 的主键列 '%s' 未在 id/fields 中定义", t.DeclaredName, pk)
		}
	}
	if t.HasPrimaryDecl && len(t.PrimaryColumns) == 0 {
		warn("表 '%s' 声明了空的 primary_indexes，将删除数据库中已有的主键", t.DeclaredName)
	}

	// 索引列必须存在
	for _, idx := range t.allIndexes() {
		if err := checkIdent(idx.Name, "索引名"); err != nil {
			return fmt.Errorf("表 '%s' %v", t.DeclaredName, err)
		}
		seen := map[string]bool{}
		for _, c := range idx.Columns {
			if _, ok := t.column(c); !ok {
				return fmt.Errorf("表 '%s' %s:'%s' columns:'%s' is not find", t.DeclaredName, idx.Kind, idx.Name, c)
			}
			if seen[c] {
				warn("表 '%s' %s:'%s' 的列 '%s' 重复出现", t.DeclaredName, idx.Kind, idx.Name, c)
			}
			seen[c] = true
		}
		if idx.Kind == indexKindFulltext {
			if !builtinFulltextParsers[strings.ToLower(idx.WithParser)] {
				warn("表 '%s' 全文索引 '%s' 的分词器 '%s' 不是 MySQL 内置的（内置只有 ngram/mecab），"+
					"需要额外安装插件，否则建索引会被数据库拒绝；同时建表语句里不会写 WITH PARSER",
					t.DeclaredName, idx.Name, idx.WithParser)
			}
			for _, c := range idx.Columns {
				col, _ := t.column(c)
				if col != nil && !isFulltextableType(col.Decl) {
					warn("表 '%s' 全文索引 '%s' 的列 '%s' 类型为 %s，MySQL 仅支持 CHAR/VARCHAR/TEXT 类列",
						t.DeclaredName, idx.Name, c, col.Decl)
				}
			}
		}
	}

	// 自增列必须是某个索引的首列，MySQL 的硬性要求
	var autoInc string
	for _, c := range t.Columns {
		if strings.Contains(strings.ToLower(c.Generator), "auto_increment") {
			autoInc = c.Name
			break
		}
	}
	if autoInc != "" {
		first := ""
		if len(t.PrimaryColumns) > 0 {
			first = t.PrimaryColumns[0]
		}
		ok := first == autoInc
		if !ok {
			for _, idx := range t.allIndexes() {
				if len(idx.Columns) > 0 && idx.Columns[0] == autoInc {
					ok = true
					break
				}
			}
		}
		if !ok {
			warn("表 '%s' 的自增列 '%s' 不是主键或任何索引的首列，MySQL 会拒绝建表", t.DeclaredName, autoInc)
		}
	}

	// 字符集与排序规则的一致性
	if t.Charset != "" && t.Collate != "" && !strings.HasPrefix(strings.ToLower(t.Collate), strings.ToLower(t.Charset)+"_") {
		warn("表 '%s' 的 collate '%s' 看起来不属于 charset '%s'", t.DeclaredName, t.Collate, t.Charset)
	}

	// 字段级告警：默认值与类型不匹配
	for _, c := range t.Columns {
		if c.DefaultSet && isNoDefaultType(c.Decl) {
			warn("表 '%s' 字段 '%s' 的类型 %s 不支持字面默认值，default 配置会被忽略", t.DeclaredName, c.Name, c.Decl)
		}
		if c.DefaultSet && c.DefaultNull && !c.Nullable {
			// 只在写成了 yaml 空值时提醒；显式写 default: '' 是正常用法，不该噪音
			warn("表 '%s' 字段 '%s' 的 default 没写值，将按 DEFAULT '' 处理；如果本意是不设默认值，请删掉这一行", t.DeclaredName, c.Name)
		}
		if g := strings.ToLower(strings.TrimSpace(c.Generator)); g != "" && !knownGenerator(g) {
			warn("表 '%s' 字段 '%s' 的 generator '%s' 不是常见写法，将原样拼进 SQL，请自行确认",
				t.DeclaredName, c.Name, c.Generator)
		}
		if err := checkIdent(c.Name, "字段名"); err != nil {
			return fmt.Errorf("表 '%s' %v", t.DeclaredName, err)
		}
	}
	return nil
}

// isFulltextableType 判断类型能否建全文索引
func isFulltextableType(decl string) bool {
	p, ok := parseColumnType(decl)
	if !ok {
		return false
	}
	switch p.canonical {
	case "char", "varchar", "tinytext", "text", "mediumtext", "longtext":
		return true
	}
	return false
}

// knownGenerator 判断 generator 是否是 MySQL 认识的写法
func knownGenerator(g string) bool {
	g = strings.ToLower(strings.TrimSpace(g))
	g = strings.ReplaceAll(g, "  ", " ")
	switch g {
	case "auto_increment", "default_generated", "default current_timestamp",
		"on update current_timestamp", "default current_timestamp on update current_timestamp",
		"current_timestamp", "current_timestamp()", "default current_timestamp()",
		"on update current_timestamp()", "default current_timestamp() on update current_timestamp()":
		return true
	}
	// 带小数秒精度的写法，例如 DEFAULT CURRENT_TIMESTAMP(3)
	trimmed := strings.TrimSuffix(strings.TrimPrefix(g, "default "), "()")
	for _, base := range []string{"current_timestamp", "on update current_timestamp"} {
		if trimmed == base {
			return true
		}
	}
	if strings.HasPrefix(g, "default current_timestamp(") || strings.HasPrefix(g, "on update current_timestamp(") {
		return true
	}
	return false
}

// checkIdent 校验表名/字段名/索引名是否可以安全地写进 SQL
func checkIdent(name, label string) error {
	if name == "" {
		return fmt.Errorf("%s不能为空", label)
	}
	if len(name) > 64 {
		return fmt.Errorf("%s '%s' 超过 MySQL 允许的 64 个字符", label, name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("%s '%s' 含有非法字符", label, name)
	}
	return nil
}

// expandSharding 按 sharding_tables 展开成多张表；未配置分表时返回自身
func (t *ymlTable) expandSharding() []*ymlTable {
	if len(t.ShardingTables) == 0 {
		t.Name = t.DeclaredName
		return []*ymlTable{t}
	}
	out := make([]*ymlTable, 0, len(t.ShardingTables))
	for _, name := range t.ShardingTables {
		cp := *t
		cp.Name = name
		// 切片与 map 是共享的，这里只做只读使用，不会被修改
		out = append(out, &cp)
	}
	return out
}

// buildSchemaJSON 拼装编译产物：{"表名": 文档JSON, ...}
// 顶层是"声明的表名 -> 该表整份文档"，loadFromBuildSchema 按这个形状回读；
// 文档内部的键保持 yml 的书写顺序，所以字段顺序能被稳定还原。
func buildSchemaJSON(tables []*ymlTable) (string, error) {
	var b strings.Builder
	b.WriteByte('{')
	written := 0
	for _, t := range tables {
		kj, err := json.Marshal(t.DeclaredName)
		if err != nil {
			return "", err
		}
		if written > 0 {
			b.WriteByte(',')
		}
		b.Write(kj)
		b.WriteByte(':')
		b.WriteString(t.Raw)
		written++
	}
	b.WriteByte('}')
	return b.String(), nil
}
