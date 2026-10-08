package information_schema

type YmlTable struct {
	TableName string
	Charset   string
	Collate   string
	Comment   string

	UnqIndexes []YmlIndexes
	Indexes    []YmlIndexes
	Columns    []YmlColum
}

type YmlIndexes struct {
	IndexName    string   `json:"index_name"`
	IndexColumns []string `json:"columns"`
}

// type YmlIndexes

type SqlIndexesSerialize struct {
	UnqIndexes      map[string]map[string][]string `json:"unique_indexes"`
	Indexes         map[string]map[string][]string `json:"indexes"`
	FulltextIndexes map[string]map[string][]string `json:"fulltext_indexes"`
	PrimaryIndexes  map[string][]string            `json:"primary_indexes"`
}

type SqlColumnsSerialize map[string]map[string]string

type YmlColum struct {
	ColumnName    string
	ColumnType    string
	ColumnLength  string
	Nullable      bool
	ColumnDefault string
	Unsigned      bool
}

type SqlTable struct {
	TableName    string `gorm:"column:TABLE_NAME"`
	TableComment string `gorm:"column:TABLE_COMMENT"`
	// TableCollation 表的排序规则，用于检测 charset/collate 漂移
	TableCollation string `gorm:"column:TABLE_COLLATION"`
}

type SqlIndexes struct {
	// Table_name 索引所属的表名，批量查询多张表的索引时用于归属
	Table_name   string `gorm:"column:Table_name"`
	Non_unique   int    `gorm:"column:Non_unique"`
	Key_name     string `gorm:"column:Key_name"`
	Seq_in_index int    `gorm:"column:Seq_in_index"`
	Column_name  string `gorm:"column:Column_name"`
	IndexType    string `gorm:"column:Index_type"`
	// Column_name string `gorm:"column:Column_name"`
}

type SqlTableColumns struct {
	TableName     string  `gorm:"column:TABLE_NAME"`
	ColumnName    string  `gorm:"column:COLUMN_NAME"`
	ColumnDefault *string `gorm:"column:COLUMN_DEFAULT"`
	IsNullable    string  `gorm:"column:IS_NULLABLE"`
	DataType      string  `gorm:"column:DATA_TYPE"`
	ColumnType    string  `gorm:"column:COLUMN_TYPE"`
	Length        *string `gorm:"column:CHARACTER_MAXIMUM_LENGTH"`
	ColumnComment string  `gorm:"column:COLUMN_COMMENT"`
	Extra         string  `gorm:"column:EXTRA"`
}
