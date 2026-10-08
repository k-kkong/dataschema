package main

import (
	"fmt"
	"os"
	"strings"

	dataschema "github.com/k-kkong/dataschema"
)

// tcase 一个用例。
//
// 断言全部用"期望值"的方式写死在这里，跑完对不上就是回归失败。
// 变更清单的格式见 main.go 的 summarize：表名/变更类型:对象(高危)(跳过)。
type tcase struct {
	name string // 用例名，编号与 etc/ 下配置文件头部注释里的编号一致
	dir  string // etc/ 下的配置目录，一个目录就是"表结构的一个版本"
	desc string // 一句话说明

	config    func(e *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler // 需要额外开关时用
	want      []string                                                                  // 期望的变更清单
	wantWarn  []string                                                                  // 期望出现的告警关键字
	sqlHas    []string                                                                  // 生成的 SQL 里应该有的片段
	sqlNot    []string                                                                  // 生成的 SQL 里不该有的片段
	wantPanic string                                                                    // 期望解析阶段直接终止，且输出里含该关键字

	dryOnly bool               // 只预览不执行
	noIdem  bool               // 跳过"再跑一次应为 0 变更"的幂等复检
	check   func(e *env) error // 执行完复查数据库真实状态
	extra   func(e *env) error // 附加流程（例如编译产物回读）
}

// cases 用例清单，顺序就是执行顺序。
//
// ds_case_demo 这张表从 01 到 09 是一条演进链：后一个版本是在前一个版本的基础上改的，
// 所以用例之间有先后依赖，不能单独乱序跑（-only 参数只适合已经跑过一整轮之后重复观察）。
// 23 与 24 共用 filter_multi 目录，24 依赖 23 已经把 alpha/beta 建好，同样有顺序要求。
func cases() []tcase {
	return []tcase{
		// ---------------------------------------------------------------
		// ds_case_demo 演进链：建表 -> 加字段 -> 改类型 -> 改注释
		// -> 改可空性与默认值 -> 删字段 -> 索引增删改 -> 全文索引 -> 删索引
		// ---------------------------------------------------------------
		{
			name: "01 建表",
			dir:  "demo_create",
			desc: "库里没有这张表，整张表按 yml 声明创建；列顺序 = id 区在前 + fields 区按书写顺序",
			want: []string{"ds_case_demo/建表"},
			sqlHas: []string{
				"CREATE TABLE `ds_case_demo`(",
				"`id` int(10) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键'",
				"`name` varchar(32) NOT NULL DEFAULT '' COMMENT '名称'",
				"`age` int(11) NOT NULL DEFAULT '0' COMMENT '年龄'",
				"`created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'",
				"INDEX `idx_name` (`name`)",
				"UNIQUE INDEX `unq_name_age` (`name`,`age`)",
				"PRIMARY KEY(`id`)",
				"DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci ENGINE = InnoDB  COMMENT = '用例主表' ;",
			},
			check: func(e *env) error {
				cols, err := columnOrder(e, "ds_case_demo")
				if err != nil {
					return err
				}
				return eqList("建表后的列顺序", cols,
					[]string{"id", "name", "age", "intro", "created_at", "updated_at"})
			},
		},
		{
			name: "01b 重复执行（幂等）",
			dir:  "demo_create",
			desc: "同一份配置再跑一次，必须一条变更都没有——这是整套比对逻辑最基本的要求",
			want: nil,
			check: func(e *env) error {
				return expectExists(e, "ds_case_demo")
			},
		},
		{
			name: "02 新增字段",
			dir:  "demo_add_column",
			desc: "新增两个字段：插在中间的走 AFTER，写在末尾的也要落位到最后，而不是随便追加",
			want: []string{"ds_case_demo/新增列:nickname", "ds_case_demo/新增列:remark"},
			sqlHas: []string{
				"ADD COLUMN `nickname` varchar(64) NOT NULL DEFAULT '' COMMENT '昵称' AFTER `name`",
				"ADD COLUMN `remark` varchar(255) DEFAULT NULL COMMENT '备注' AFTER `updated_at`",
			},
			check: func(e *env) error {
				cols, err := columnOrder(e, "ds_case_demo")
				if err != nil {
					return err
				}
				return eqList("新增后的列顺序", cols,
					[]string{"id", "name", "nickname", "age", "intro", "created_at", "updated_at", "remark"})
			},
		},
		{
			name: "03 改类型",
			dir:  "demo_change_type",
			desc: "name 由 varchar(32) 改宽到 varchar(64)，age 由 integer 改成 bigint",
			want: []string{"ds_case_demo/修改列:name", "ds_case_demo/修改列:age"},
			sqlHas: []string{
				"MODIFY COLUMN `name` varchar(64) NOT NULL DEFAULT '' COMMENT '名称'",
				"MODIFY COLUMN `age` bigint(20) NOT NULL DEFAULT '0' COMMENT '年龄'",
			},
			check: func(e *env) error {
				decl, _, _, _, err := columnInfo(e, "ds_case_demo", "name")
				if err != nil {
					return err
				}
				if err := eq("name 的类型", decl, "varchar(64)"); err != nil {
					return err
				}
				decl, _, _, _, err = columnInfo(e, "ds_case_demo", "age")
				if err != nil {
					return err
				}
				// MySQL 5.7 回读 bigint(20)、8.0 回读 bigint，去掉显示宽度后再比
				return eq("age 的类型", trimWidth(decl), "bigint")
			},
		},
		{
			name: "04 改注释",
			dir:  "demo_change_comment",
			desc: "只改表注释与列注释，不动数据；两者在报告里是独立的两条记录",
			want: []string{"ds_case_demo/表注释", "ds_case_demo/修改列:name"},
			sqlHas: []string{
				"ALTER TABLE `ds_case_demo` comment '用例主表（改过注释）'",
				"MODIFY COLUMN `name` varchar(64) NOT NULL DEFAULT '' COMMENT '名称（改过注释）'",
			},
			check: func(e *env) error {
				comment, _, err := tableInfo(e, "ds_case_demo")
				if err != nil {
					return err
				}
				if err := eq("表注释", comment, "用例主表（改过注释）"); err != nil {
					return err
				}
				_, colComment, _, _, err := columnInfo(e, "ds_case_demo", "name")
				if err != nil {
					return err
				}
				return eq("name 的注释", colComment, "名称（改过注释）")
			},
		},
		{
			name: "05 改可空性与默认值",
			dir:  "demo_change_nullable_default",
			desc: "name 由 NOT NULL 放开为可空，age 的默认值由 0 改成 18",
			want: []string{"ds_case_demo/修改列:name", "ds_case_demo/修改列:age"},
			sqlHas: []string{
				"MODIFY COLUMN `name` varchar(64) DEFAULT '' COMMENT '名称（改过注释）'",
				"MODIFY COLUMN `age` bigint(20) NOT NULL DEFAULT '18' COMMENT '年龄'",
			},
			check: func(e *env) error {
				_, _, nullable, _, err := columnInfo(e, "ds_case_demo", "name")
				if err != nil {
					return err
				}
				if err := eq("name 是否可空", nullable, "YES"); err != nil {
					return err
				}
				_, _, _, def, err := columnInfo(e, "ds_case_demo", "age")
				if err != nil {
					return err
				}
				return eq("age 的默认值", def, "18")
			},
		},
		{
			name:   "06 删除字段",
			dir:    "demo_drop_column",
			desc:   "yml 里删掉 remark，默认 DropPolicyAlways 会真的 DROP",
			want:   []string{"ds_case_demo/删除列:remark(高危)"},
			sqlHas: []string{"ALTER TABLE `ds_case_demo` DROP `remark`;"},
			check: func(e *env) error {
				return expectNoColumn(e, "ds_case_demo", "remark")
			},
		},
		{
			name: "07 索引增删改",
			dir:  "demo_change_index",
			desc: "一次覆盖三种索引变更：删掉唯一索引、改索引列（重建）、新增普通索引",
			want: []string{
				"ds_case_demo/删除索引:unq_name_age(高危)",
				"ds_case_demo/重建索引:idx_name(高危)",
				"ds_case_demo/新增索引:idx_created_at",
			},
			sqlHas: []string{
				"DROP INDEX `idx_name` ON `ds_case_demo`",
				"CREATE INDEX `idx_name` ON `ds_case_demo`(`name`,`age`)",
				"CREATE INDEX `idx_created_at` ON `ds_case_demo`(`created_at`)",
				"DROP INDEX `unq_name_age` ON `ds_case_demo`",
			},
			check: func(e *env) error {
				cols, err := indexColumns(e, "ds_case_demo", "idx_name")
				if err != nil {
					return err
				}
				if err := eqList("idx_name 的列", cols, []string{"name", "age"}); err != nil {
					return err
				}
				if err := expectNoIndex(e, "ds_case_demo", "unq_name_age"); err != nil {
					return err
				}
				return expectIndex(e, "ds_case_demo", "idx_created_at")
			},
		},
		{
			name:   "08 全文索引",
			dir:    "demo_add_fulltext",
			desc:   "新增 ngram 分词的全文索引；with_parser 只有全文索引能写",
			want:   []string{"ds_case_demo/新增索引:ft_intro"},
			sqlHas: []string{"CREATE FULLTEXT INDEX `ft_intro` ON `ds_case_demo`(`intro`) WITH PARSER ngram"},
			check: func(e *env) error {
				return expectIndex(e, "ds_case_demo", "ft_intro")
			},
		},
		{
			name: "09 删除索引",
			dir:  "demo_drop_index",
			desc: "删掉全文索引 ft_intro 与普通索引 idx_created_at，只留 idx_name",
			want: []string{
				"ds_case_demo/删除索引:ft_intro(高危)",
				"ds_case_demo/删除索引:idx_created_at(高危)",
			},
			sqlHas: []string{
				"DROP INDEX `ft_intro` ON `ds_case_demo`",
				"DROP INDEX `idx_created_at` ON `ds_case_demo`",
			},
			check: func(e *env) error {
				if err := expectNoIndex(e, "ds_case_demo", "ft_intro"); err != nil {
					return err
				}
				if err := expectNoIndex(e, "ds_case_demo", "idx_created_at"); err != nil {
					return err
				}
				return expectIndex(e, "ds_case_demo", "idx_name")
			},
		},
		{
			name: "09b 演进链终态（幂等）",
			dir:  "demo_drop_index",
			desc: "整条演进链跑完后再比对一次，列顺序与索引都应与配置完全一致，0 条变更",
			want: nil,
			check: func(e *env) error {
				cols, err := columnOrder(e, "ds_case_demo")
				if err != nil {
					return err
				}
				return eqList("终态列顺序", cols,
					[]string{"id", "name", "nickname", "age", "intro", "created_at", "updated_at"})
			},
		},

		// ---------------------------------------------------------------
		// ds_case_pk：主键的三种变更
		// ---------------------------------------------------------------
		{
			name:   "10 主键建表",
			dir:    "pk_create",
			desc:   "显式声明 primary_indexes，建表时就按 (tenant_id, id) 建主键",
			want:   []string{"ds_case_pk/建表"},
			sqlHas: []string{"PRIMARY KEY(`tenant_id`,`id`)"},
			check: func(e *env) error {
				cols, err := indexColumns(e, "ds_case_pk", "PRIMARY")
				if err != nil {
					return err
				}
				return eqList("主键列", cols, []string{"tenant_id", "id"})
			},
		},
		{
			name: "11 改主键顺序",
			dir:  "pk_reorder",
			desc: "主键列顺序由 (tenant_id,id) 改成 (id,tenant_id)，先 DROP PRIMARY KEY 再 ADD",
			want: []string{"ds_case_pk/主键(高危)"},
			sqlHas: []string{
				"ALTER TABLE `ds_case_pk` DROP PRIMARY KEY;",
				"ALTER TABLE `ds_case_pk` ADD PRIMARY KEY (`id`,`tenant_id`);",
			},
			check: func(e *env) error {
				cols, err := indexColumns(e, "ds_case_pk", "PRIMARY")
				if err != nil {
					return err
				}
				return eqList("主键列", cols, []string{"id", "tenant_id"})
			},
		},
		{
			name:     "12 删除主键",
			dir:      "pk_drop",
			desc:     "primary_indexes 的 columns 写成空数组，表示这张表不要主键；同时会有一条告警",
			want:     []string{"ds_case_pk/主键(高危)"},
			wantWarn: []string{"声明了空的 primary_indexes"},
			sqlHas:   []string{"ALTER TABLE `ds_case_pk` DROP PRIMARY KEY;"},
			check: func(e *env) error {
				cols, err := indexColumns(e, "ds_case_pk", "PRIMARY")
				if err != nil {
					return err
				}
				return eqList("主键列", cols, nil)
			},
		},

		// ---------------------------------------------------------------
		// ds_case_charset：字符集漂移，默认只报告，显式开启才同步
		// ---------------------------------------------------------------
		{
			name: "13 字符集建表",
			dir:  "charset_create",
			desc: "按 utf8mb4_general_ci 建表，为下一步的漂移检测准备基线",
			want: []string{"ds_case_charset/建表"},
			check: func(e *env) error {
				_, collation, err := tableInfo(e, "ds_case_charset")
				if err != nil {
					return err
				}
				return eq("表排序规则", collation, "utf8mb4_general_ci")
			},
		},
		{
			name:   "14 字符集漂移（默认只报告）",
			dir:    "charset_drift",
			desc:   "collate 与库里不一致，默认不执行——CONVERT TO CHARACTER SET 会重写整表数据",
			want:   []string{"ds_case_charset/表字符集(跳过)"},
			sqlNot: []string{"CONVERT TO CHARACTER SET"},
			check: func(e *env) error {
				_, collation, err := tableInfo(e, "ds_case_charset")
				if err != nil {
					return err
				}
				return eq("表排序规则应保持不变", collation, "utf8mb4_general_ci")
			},
		},
		{
			name: "15 字符集漂移（显式同步）",
			dir:  "charset_drift",
			desc: "同一份配置，打开 SetSyncTableCharset(true) 后就会真的执行同步",
			config: func(_ *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetSyncTableCharset(true)
			},
			want:   []string{"ds_case_charset/表字符集"},
			sqlHas: []string{"ALTER TABLE `ds_case_charset` CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"},
			check: func(e *env) error {
				_, collation, err := tableInfo(e, "ds_case_charset")
				if err != nil {
					return err
				}
				return eq("表排序规则", collation, "utf8mb4_unicode_ci")
			},
		},

		// ---------------------------------------------------------------
		// ds_case_keep：DropPolicyNever，删掉的列与索引只报告不执行
		// ---------------------------------------------------------------
		{
			name: "16 DropPolicy 基线建表",
			dir:  "keep_create",
			desc: "建一张带 col_a/col_b/col_c 与两个索引的表，为下一步的 DropPolicy 对比做准备",
			want: []string{"ds_case_keep/建表"},
		},
		{
			name: "17 DropPolicyNever（只报告不删）",
			dir:  "keep_drop_less",
			desc: "yml 里删掉了 col_c 与 idx_c，但策略是 never，所以一条 SQL 都不该生成",
			config: func(_ *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetDropPolicy(dataschema.DropPolicyNever)
			},
			want: []string{
				"ds_case_keep/删除列:col_c(高危)(跳过)",
				"ds_case_keep/删除索引:idx_c(高危)(跳过)",
			},
			sqlNot: []string{"DROP"},
			check: func(e *env) error {
				if err := expectColumn(e, "ds_case_keep", "col_c"); err != nil {
					return err
				}
				return expectIndex(e, "ds_case_keep", "idx_c")
			},
		},

		// ---------------------------------------------------------------
		// 其它能力：分表、告警、DryRun 导出、编译产物、配置报错
		// ---------------------------------------------------------------
		{
			name: "18 分表",
			dir:  "shard_create",
			desc: "一份定义展开成 ds_case_shard_01/02 两张物理表，声明名本身不建表",
			want: []string{"ds_case_shard_01/建表", "ds_case_shard_02/建表"},
			check: func(e *env) error {
				if err := expectExists(e, "ds_case_shard_01"); err != nil {
					return err
				}
				if err := expectExists(e, "ds_case_shard_02"); err != nil {
					return err
				}
				// 分表场景下，yml 里写的声明名只是模板，不该被当成物理表建出来
				return expectNotExists(e, "ds_case_shard")
			},
		},
		{
			name: "19 配置写错只告警",
			dir:  "warn_typo",
			desc: "配置项拼错、default 漏填值、非内置分词器——逐条告警但不中断，表照常建出来",
			want: []string{"ds_case_warn/建表"},
			wantWarn: []string{
				"engin",
				"defalut",
				"default 没写值",
				"不是 MySQL 内置",
			},
			check: func(e *env) error {
				return expectExists(e, "ds_case_warn")
			},
		},
		{
			name:    "20 DryRun + 导出 SQL",
			dir:     "dryrun_create",
			desc:    "SetDryRun(true) 一条都不执行，同时把 SQL 导出到文件交给 DBA 审核",
			dryOnly: true,
			config: func(e *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetSqlExportPath(e.outPath("dryrun.sql"))
			},
			want:   []string{"ds_case_dry/建表"},
			sqlHas: []string{"`status` enum('draft','published') NOT NULL DEFAULT 'draft' COMMENT '状态'"},
			check: func(e *env) error {
				if err := expectNotExists(e, "ds_case_dry"); err != nil {
					return err
				}
				raw, err := os.ReadFile(e.outPath("dryrun.sql"))
				if err != nil {
					return fmt.Errorf("导出的 SQL 文件读不到：%v", err)
				}
				if !strings.Contains(string(raw), "CREATE TABLE `ds_case_dry`(") {
					return fmt.Errorf("导出的 SQL 文件内容不对：%s", head(string(raw), 200))
				}
				fmt.Printf("  导出文件：%s（%d 字节）\n", e.outPath("dryrun.sql"), len(raw))
				return nil
			},
		},
		{
			name: "21 编译产物 + LoadSchema 回读",
			dir:  "build_create",
			desc: "把 yml 编译成产物，再用产物回读比对，结构必须与库里完全一致",
			config: func(e *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetIsOutputBuildSchema(true, false, "").
					SetBuildSchemaDest(e.outPath("ds_case_build.value"))
			},
			want:   []string{"ds_case_build/建表"},
			sqlHas: []string{"DEFAULT '007'", "DEFAULT '1.10'"},
			check: func(e *env) error {
				cols, err := columnOrder(e, "ds_case_build")
				if err != nil {
					return err
				}
				return eqList("建表后的列顺序", cols, []string{"id", "zulu", "alpha"})
			},
			extra: checkBuildSchema,
		},
		{
			name:      "22 配置写错直接终止",
			dir:       "bad_config",
			desc:      "索引引用了不存在的列，必须在解析阶段就带着明确原因终止，不能把非法 SQL 丢给数据库",
			wantPanic: "is not find",
		},

		// ---------------------------------------------------------------
		// 发布控制：表过滤、递归扫描、迁移历史
		// ---------------------------------------------------------------
		{
			name: "23 表过滤与排除",
			dir:  "filter_multi",
			desc: "一个目录里放了 4 份配置，用 filter 选中一批、再用 exclude 剔掉一张，只有剩下的会建",
			config: func(_ *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetTableFilter("ds_case_f_*").
					SetTableExclude("ds_case_f_gamma")
			},
			want: []string{"ds_case_f_alpha/建表", "ds_case_f_beta/建表"},
			check: func(e *env) error {
				if err := expectExists(e, "ds_case_f_alpha"); err != nil {
					return err
				}
				if err := expectExists(e, "ds_case_f_beta"); err != nil {
					return err
				}
				// 被 exclude 剔掉的不该建出来
				if err := expectNotExists(e, "ds_case_f_gamma"); err != nil {
					return err
				}
				// 子目录里的也不该建出来：默认只扫 YamlPath 本层
				return expectNotExists(e, "ds_case_f_deep")
			},
		},
		{
			name: "24 递归扫描子目录",
			dir:  "filter_multi",
			desc: "同一份目录打开 SetRecursive(true)，nested/ 下的配置才会被扫到；alpha/beta 已存在，所以只剩 deep 要建",
			config: func(_ *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetTableFilter("ds_case_f_*").
					SetTableExclude("ds_case_f_gamma").
					SetRecursive(true)
			},
			want: []string{"ds_case_f_deep/建表"},
			check: func(e *env) error {
				if err := expectExists(e, "ds_case_f_deep"); err != nil {
					return err
				}
				// exclude 在递归模式下同样生效
				return expectNotExists(e, "ds_case_f_gamma")
			},
		},
		{
			name: "25 迁移历史（断点续跑）",
			dir:  "dryrun_create",
			desc: "DDL 无法回滚，执行到一半报错时前面的语句已经生效；开启迁移历史后每条成功的语句按指纹入库，重跑会自动跳过",
			config: func(_ *env, h *dataschema.YamlToSqlHandler) *dataschema.YamlToSqlHandler {
				return h.SetMigrationHistory(true, "ds_case_migration")
			},
			want: []string{"ds_case_dry/建表"},
			check: func(e *env) error {
				if err := expectExists(e, "ds_case_dry"); err != nil {
					return err
				}
				// 历史表要自动建出来，并且记下本次执行的语句与它归属的表
				if err := expectExists(e, "ds_case_migration"); err != nil {
					return err
				}
				var n int64
				if err := e.db.Raw("SELECT COUNT(*) FROM `ds_case_migration`").Scan(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					return fmt.Errorf("历史表里一条记录都没有，迁移历史没生效")
				}
				var owner string
				if err := e.db.Raw("SELECT `table_name` FROM `ds_case_migration` ORDER BY `id` LIMIT 1").Scan(&owner).Error; err != nil {
					return err
				}
				fmt.Printf("  历史表已记录 %d 条语句\n", n)
				return eq("历史记录归属的表", owner, "ds_case_dry")
			},
		},
	}
}

// checkBuildSchema 校验编译产物：顺序、标量原文、以及从产物回读后与库结构一致
func checkBuildSchema(e *env) error {
	dest := e.outPath("ds_case_build.value")
	raw, err := os.ReadFile(dest)
	if err != nil {
		return fmt.Errorf("编译产物没写出来：%v", err)
	}
	content := string(raw)

	// 产物里字段顺序必须与 yml 一致。
	// 一旦被排成字母序，alpha 就会跑到 zulu 前面。
	iz, ia := strings.Index(content, `"zulu"`), strings.Index(content, `"alpha"`)
	if iz < 0 || ia < 0 || iz > ia {
		return fmt.Errorf("编译产物里的字段顺序被改成了字母序：%s", head(content, 200))
	}
	// 默认值的前导零与末尾零必须原样保留，不能被 yaml 当成数字改写
	if !strings.Contains(content, `"007"`) || !strings.Contains(content, `"1.10"`) {
		return fmt.Errorf("编译产物里的默认值被改写过：%s", head(content, 300))
	}

	// 用产物回读：线上只发布产物、不发布 yml 时走的就是这条路
	var clean bool
	out, rec := capture(func() {
		h := dataschema.NewYamlToSqlHandler().
			SetDB(e.db).
			SetBuildSchemaDest(dest).
			SetDryRun(true)
		h.LoadSchema()
		clean = h.VerifyIsCleanSchema()
	})
	if rec != nil {
		return fmt.Errorf("LoadSchema 异常终止：%v\n%s", rec, out)
	}
	if !clean {
		return fmt.Errorf("从编译产物回读后仍检测到结构差异：\n%s", out)
	}
	fmt.Printf("  编译产物：%s（%d 字节），回读后与库结构一致\n", dest, len(raw))
	return nil
}

// head 取前 n 个字符，报错信息用
func head(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// trimWidth 去掉整数类型的显示宽度，
// 让断言在 MySQL 5.7（bigint(20)）与 8.0（bigint）上都成立
func trimWidth(decl string) string {
	for _, t := range []string{"tinyint", "smallint", "mediumint", "bigint", "int"} {
		if !strings.HasPrefix(decl, t+"(") {
			continue
		}
		if i := strings.Index(decl, ")"); i > 0 {
			return t + decl[i+1:]
		}
	}
	return decl
}
