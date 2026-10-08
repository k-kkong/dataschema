package dataschema

import (
	"fmt"
	"strings"
)

// 本文件只负责"把内存模型翻译成 MySQL 语句"，不做任何比对判断。
// 所有标识符统一用反引号包裹、所有字符串字面量统一转义，
// 避免字段名撞上保留字、或 comment/default 里带单引号时生成非法 SQL。

// quoteIdent 用反引号包裹标识符（表名/列名/索引名）
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// quoteLiteral 生成 SQL 字符串字面量。
// MySQL 默认模式下反斜杠是转义符，因此反斜杠与单引号都要处理。
func quoteLiteral(v string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`'`, `''`,
		"\x00", `\0`,
		"\n", `\n`,
		"\r", `\r`,
		"\x1a", `\Z`,
	)
	return "'" + r.Replace(v) + "'"
}

// quoteIdentList 生成 `a`,`b`,`c` 形式的列清单
func quoteIdentList(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, quoteIdent(n))
	}
	return strings.Join(out, ",")
}

// columnDefOption 生成列定义时的可选项
type columnDefOption struct {
	// position 是新增列的位置声明（FIRST / AFTER `x`），空表示追加到末尾。
	// 只有 ADD COLUMN 支持，MODIFY COLUMN 不做重排（重排会触发表重建，风险太高）。
	position string
}

// generatorHasDefault 判断 generator 里是否已经写了 DEFAULT，避免生成两个 DEFAULT 子句
func generatorHasDefault(generator string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(generator)), "default")
}

// buildColumnDefinition 生成 "列名 类型 [NOT NULL] [DEFAULT x] [generator] COMMENT 'x' [位置]"
//
// CREATE TABLE / ADD COLUMN / MODIFY COLUMN 三处共用，
// 保证同一个字段无论走哪条路径，落到数据库里的定义都完全一致。
func buildColumnDefinition(c *ymlColumn, opt columnDefOption) string {
	parts := []string{quoteIdent(c.Name), c.Decl}
	if !c.Nullable {
		parts = append(parts, "NOT NULL")
	}
	switch {
	case isNoDefaultType(c.Decl):
		// text/blob/json/空间类型不允许字面默认值
	case generatorHasDefault(c.Generator):
		// 默认值已经写在 generator 里（例如 default current_timestamp）
	case c.DefaultSet:
		parts = append(parts, "DEFAULT "+quoteLiteral(c.Default))
	case c.Nullable:
		parts = append(parts, "DEFAULT NULL")
	}
	if g := strings.TrimSpace(c.Generator); g != "" {
		parts = append(parts, g)
	}
	// comment 为空时也输出 COMMENT ''，让列定义的文本形态固定下来
	parts = append(parts, "COMMENT "+quoteLiteral(c.Comment))
	if opt.position != "" {
		parts = append(parts, opt.position)
	}
	return strings.Join(parts, " ")
}

// buildCreateTableSQL 生成建表语句
func buildCreateTableSQL(t *ymlTable) (string, error) {
	if t.Charset == "" {
		return "", fmt.Errorf("表: %s charset 不正确", t.DeclaredName)
	}
	if t.Collate == "" {
		return "", fmt.Errorf("表: %s collate 不正确", t.DeclaredName)
	}

	// 列顺序 = id 区在前、fields 区在后，各自保持 yml 的书写顺序
	clauses := make([]string, 0, len(t.Columns)+len(t.Indexes)+len(t.UniqueIndexes)+len(t.FulltextIndexes)+1)
	for _, c := range t.Columns {
		// 尾部这个空格是刻意保留的，让建表语句的文本形态固定下来，便于逐字比对与归档
		clauses = append(clauses, "\t"+buildColumnDefinition(c, columnDefOption{})+" ")
	}
	for _, idx := range t.Indexes {
		clauses = append(clauses, fmt.Sprintf("\tINDEX %s (%s)", quoteIdent(idx.Name), quoteIdentList(idx.Columns)))
	}
	for _, idx := range t.UniqueIndexes {
		clauses = append(clauses, fmt.Sprintf("\tUNIQUE INDEX %s (%s)", quoteIdent(idx.Name), quoteIdentList(idx.Columns)))
	}
	for _, idx := range t.FulltextIndexes {
		clause := fmt.Sprintf("\tFULLTEXT INDEX %s (%s)", quoteIdent(idx.Name), quoteIdentList(idx.Columns))
		if parser, ok := fulltextParserClause(idx); ok {
			clause += " " + parser
		}
		clauses = append(clauses, clause)
	}
	if len(t.PrimaryColumns) > 0 {
		clauses = append(clauses, fmt.Sprintf("\tPRIMARY KEY(%s)", quoteIdentList(t.PrimaryColumns)))
	}
	if len(clauses) == 0 {
		return "", fmt.Errorf("表 '%s' 没有可建立的内容", t.DeclaredName)
	}

	suffix := fmt.Sprintf(")\nDEFAULT CHARACTER SET %s COLLATE %s ENGINE = InnoDB ", t.Charset, t.Collate)
	if t.Comment != "" {
		suffix = fmt.Sprintf("%s COMMENT = %s ;", suffix, quoteLiteral(t.Comment))
	} else {
		// 没有表注释时也要把语句的分号补上，否则导出成文件后无法直接执行
		suffix += ";"
	}
	return fmt.Sprintf("CREATE TABLE %s(\n%s\n%s\n", quoteIdent(t.Name), strings.Join(clauses, ",\n"), suffix), nil
}

// builtinFulltextParsers MySQL 内置的全文分词器，其它名字需要额外安装插件
var builtinFulltextParsers = map[string]bool{"ngram": true, "mecab": true}

// fulltextParserClause 返回建表语句里的 WITH PARSER 子句。
//
// 只在分词器是 MySQL 内置的时候补上；非内置分词器（例如需要插件的 simple）不写进
// CREATE TABLE，否则建表会直接失败，解析阶段已经对它单独告警。
// 单独的 CREATE FULLTEXT INDEX 语句则按 yml 声明原样输出，见 buildCreateIndexSQL。
func fulltextParserClause(idx *ymlIndex) (string, bool) {
	if idx.WithParser == "" || !builtinFulltextParsers[strings.ToLower(idx.WithParser)] {
		return "", false
	}
	return "WITH PARSER " + idx.WithParser, true
}

// buildAddColumnSQL 生成新增列语句
func buildAddColumnSQL(tableName string, c *ymlColumn, position string) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;\n",
		quoteIdent(tableName), buildColumnDefinition(c, columnDefOption{position: position}))
}

// buildModifyColumnSQL 生成修改列语句
func buildModifyColumnSQL(tableName string, c *ymlColumn) string {
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;\n",
		quoteIdent(tableName), buildColumnDefinition(c, columnDefOption{}))
}

// buildDropColumnSQL 生成删除列语句
func buildDropColumnSQL(tableName, columnName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP %s;\n", quoteIdent(tableName), quoteIdent(columnName))
}

// buildCreateIndexSQL 生成建索引语句
func buildCreateIndexSQL(tableName string, idx *ymlIndex) string {
	cols := quoteIdentList(idx.Columns)
	switch idx.Kind {
	case indexKindUnique:
		return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s(%s);\n", quoteIdent(idx.Name), quoteIdent(tableName), cols)
	case indexKindFulltext:
		// 按 yml 声明原样输出分词器，不做静默替换。
		// 分词器决定了分词方式，静默丢掉会造成难以察觉的搜索行为变化，
		// 不如让数据库报错、由使用者自己确认。非内置分词器已在解析阶段告警。
		// 建表语句里的处理不同：只有内置分词器才会写进去，见 fulltextParserClause。
		parser := idx.WithParser
		if parser == "" {
			parser = "ngram"
		}
		return fmt.Sprintf("CREATE FULLTEXT INDEX %s ON %s(%s) WITH PARSER %s;\n",
			quoteIdent(idx.Name), quoteIdent(tableName), cols, parser)
	default:
		return fmt.Sprintf("CREATE INDEX %s ON %s(%s);\n", quoteIdent(idx.Name), quoteIdent(tableName), cols)
	}
}

// buildDropIndexSQL 生成删索引语句
func buildDropIndexSQL(tableName, indexName string) string {
	return fmt.Sprintf("DROP INDEX %s ON %s;\n", quoteIdent(indexName), quoteIdent(tableName))
}

// buildTableCommentSQL 生成修改表注释语句
func buildTableCommentSQL(tableName, comment string) string {
	return fmt.Sprintf("ALTER TABLE %s comment %s;\n", quoteIdent(tableName), quoteLiteral(comment))
}

// buildTableCharsetSQL 生成同步表字符集语句
func buildTableCharsetSQL(tableName, charset, collate string) string {
	return fmt.Sprintf("ALTER TABLE %s CONVERT TO CHARACTER SET %s COLLATE %s;\n",
		quoteIdent(tableName), charset, collate)
}

// buildAddPrimaryKeySQL 生成添加主键语句
func buildAddPrimaryKeySQL(tableName string, columns []string) string {
	return fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s);\n", quoteIdent(tableName), quoteIdentList(columns))
}

// buildDropPrimaryKeySQL 生成删除主键语句
func buildDropPrimaryKeySQL(tableName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY;\n", quoteIdent(tableName))
}
