package dataschema

import (
	"strings"
	"testing"
)

// colsOf 按名字造一批列，入参顺序就是期望的字段顺序
func colsOf(names ...string) []*ymlColumn {
	out := make([]*ymlColumn, 0, len(names))
	for _, n := range names {
		out = append(out, &ymlColumn{Name: n, Decl: "int(11)", Comment: n})
	}
	return out
}

// permutations 返回 0..n-1 的全部排列，用于穷举验证排序算法
func permutations(n int) [][]int {
	if n <= 1 {
		return [][]int{{0}}
	}
	var out [][]int
	for _, sub := range permutations(n - 1) {
		// 插入位置要包含末尾，否则少算一半排列
		for i := 0; i <= len(sub); i++ {
			p := make([]int, 0, n)
			p = append(p, sub[:i]...)
			p = append(p, n-1)
			p = append(p, sub[i:]...)
			out = append(out, p)
		}
	}
	return out
}

// indexOfString 列名在清单里的下标
func indexOfString(list []string, name string) int {
	for i, n := range list {
		if n == name {
			return i
		}
	}
	return -1
}

// TestPlanColumnSortExhaustive 穷举 1~6 个字段的全部排列组合。
//
// 两条硬性要求：
//  1. 按算出来的移动子句依次执行后，顺序必须正好等于 yml 声明的顺序；
//  2. 移动的字段数必须等于理论最小值（总数 - 最长公共子序列长度），
//     因为每移动一个字段都要重述一次列定义，移动的字段越少越安全。
func TestPlanColumnSortExhaustive(t *testing.T) {
	names := []string{"c0", "c1", "c2", "c3", "c4", "c5"}
	all := colsOf(names...)

	for n := 1; n <= len(names); n++ {
		want := names[:n]
		cols := all[:n]
		for _, p := range permutations(n) {
			dbOrder := make([]string, 0, n)
			for _, i := range p {
				dbOrder = append(dbOrder, names[i])
			}

			moves := planColumnSort(cols, dbOrder)
			if got := applyColumnMoves(dbOrder, moves); !sameStringSlice(got, want) {
				t.Fatalf("n=%d 库里[%s] 目标[%s]：执行完得到[%s]，移动子句=%s",
					n, strings.Join(dbOrder, ","), strings.Join(want, ","),
					strings.Join(got, ","), describeMoves(moves))
			}
			if min := n - len(lcsKeep(dbOrder, want)); len(moves) != min {
				t.Fatalf("n=%d 库里[%s]：移动了 %d 个字段，理论最小值是 %d",
					n, strings.Join(dbOrder, ","), len(moves), min)
			}
		}
	}
}

// describeMoves 把移动子句压成一行，失败时方便看清是哪一步挪错了
func describeMoves(moves []columnMove) string {
	out := make([]string, 0, len(moves))
	for _, m := range moves {
		out = append(out, m.Column.Name+"->"+m.Position)
	}
	return strings.Join(out, " | ")
}

func TestPlanColumnSortAlreadySorted(t *testing.T) {
	names := []string{"id", "a", "b", "c"}
	if moves := planColumnSort(colsOf(names...), names); moves != nil {
		t.Errorf("顺序已一致时不该产生任何移动，实际：%s", describeMoves(moves))
	}
}

// TestPlanColumnSortMovesFewest 只需要挪一个字段时，不能把其它字段也一起挪了。
//
// 这是最常见的场景：后来补的字段被追加到了表尾，而配置里它写在中间。
func TestPlanColumnSortMovesFewest(t *testing.T) {
	want := []string{"id", "a", "b", "c"}
	db := []string{"id", "a", "c", "b"} // b 被追加到了 c 后面

	moves := planColumnSort(colsOf(want...), db)
	if len(moves) != 1 {
		t.Fatalf("只需要挪 1 个字段，实际 %d 个：%s", len(moves), describeMoves(moves))
	}
	if got := applyColumnMoves(db, moves); !sameStringSlice(got, want) {
		t.Errorf("执行完得到[%s]，期望[%s]", strings.Join(got, ","), strings.Join(want, ","))
	}
}

// TestPlanColumnSortAnchorIsPreviousTarget 每个移动的参照物必须是目标顺序里
// 紧邻它前面的那一列，而移动子句又是按目标顺序依次发出的，
// 所以按子句顺序执行就能收敛到目标顺序——
// 这正是能把多个 MODIFY 合并进同一条 ALTER TABLE 的依据。
func TestPlanColumnSortAnchorIsPreviousTarget(t *testing.T) {
	names := []string{"c0", "c1", "c2", "c3", "c4", "c5"}
	all := colsOf(names...)

	for n := 1; n <= len(names); n++ {
		want := names[:n]
		for _, p := range permutations(n) {
			dbOrder := make([]string, 0, n)
			for _, i := range p {
				dbOrder = append(dbOrder, names[i])
			}
			moves := planColumnSort(all[:n], dbOrder)

			prev := -1
			for _, m := range moves {
				idx := indexOfString(want, m.Column.Name)
				expect := "FIRST"
				if idx > 0 {
					expect = "AFTER `" + want[idx-1] + "`"
				}
				if m.Position != expect {
					t.Fatalf("n=%d 库里[%s]：字段 %s 的位置应为 %q，实际 %q",
						n, strings.Join(dbOrder, ","), m.Column.Name, expect, m.Position)
				}
				// 子句必须按目标顺序递增发出，否则后面的 AFTER 会指向还没就位的列
				if idx <= prev {
					t.Fatalf("n=%d 库里[%s]：移动子句没按目标顺序排列：%s",
						n, strings.Join(dbOrder, ","), describeMoves(moves))
				}
				prev = idx
			}
		}
	}
}

func TestBuildSortColumnsSQL(t *testing.T) {
	cols := colsOf("id", "a", "b")
	got := buildSortColumnsSQL("demo", []columnMove{
		{Column: cols[1], Position: "FIRST"},
		{Column: cols[2], Position: "AFTER `a`"},
	})

	// 所有移动必须合并进同一条 ALTER TABLE：MySQL 对一条 ALTER 只重建一次整表
	if n := strings.Count(got, "ALTER TABLE"); n != 1 {
		t.Errorf("应该只有一条 ALTER TABLE，实际 %d 条：%s", n, got)
	}
	if n := strings.Count(got, "MODIFY COLUMN"); n != 2 {
		t.Errorf("应该有两个 MODIFY COLUMN 子句，实际 %d 个：%s", n, got)
	}
	for _, want := range []string{
		"ALTER TABLE `demo` ",
		"MODIFY COLUMN `a` int(11) NOT NULL COMMENT 'a' FIRST",
		"MODIFY COLUMN `b` int(11) NOT NULL COMMENT 'b' AFTER `a`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("语句里缺少 %q：\n%s", want, got)
		}
	}
}

// TestColumnExtraUnderstood 只有能完整重述的列才允许重排。
//
// MODIFY COLUMN 会把列定义整条重写，生成列表达式、空间参考系、列可见性
// 这些在配置里没有对应声明，一旦重写就会被抹掉，所以必须先拦住。
func TestColumnExtraUnderstood(t *testing.T) {
	allowed := []string{
		"",
		"auto_increment",
		"AUTO_INCREMENT",
		"on update CURRENT_TIMESTAMP",
		"ON UPDATE CURRENT_TIMESTAMP",
		"DEFAULT_GENERATED",
		"DEFAULT_GENERATED on update CURRENT_TIMESTAMP",
	}
	for _, e := range allowed {
		if !columnExtraUnderstood(e) {
			t.Errorf("EXTRA=%q 应该允许重排", e)
		}
	}

	blocked := []string{
		"VIRTUAL GENERATED",
		"STORED GENERATED",
		"DEFAULT_GENERATED extra",
		"SRID 4326",
		"INVISIBLE",
		"PRI",
	}
	for _, e := range blocked {
		if columnExtraUnderstood(e) {
			t.Errorf("EXTRA=%q 不该允许重排", e)
		}
	}
}

// dbColumnsFromYml 由 yml 声明反推出数据库里的列。
//
// 手工攒 dbColumn 很容易漏一个属性（尤其是 DefaultSet 与 Extra），
// 那样测的就不是"只有顺序不同"而是"顺序 + 定义都不同"了。
func dbColumnsFromYml(tbl *ymlTable) []*dbColumn {
	out := make([]*dbColumn, 0, len(tbl.Columns))
	for _, c := range tbl.Columns {
		extra := ""
		if strings.Contains(strings.ToLower(c.Generator), "auto_increment") {
			extra = "auto_increment"
		}
		out = append(out, &dbColumn{
			Name:       c.Name,
			Decl:       getTypeYml2SqlMapping(c.Decl),
			Nullable:   c.Nullable,
			Default:    c.Default,
			DefaultSet: c.DefaultSet,
			Comment:    c.Comment,
			Extra:      extra,
		})
	}
	return out
}

// sortableFixture 造一张表用于前置检查测试。
// order 为空时按 yml 声明的顺序摆，否则按 order 摆（用来制造纯顺序差异）。
func sortableFixture(t *testing.T, yml string, order ...string) (*ymlTable, *dbTableState) {
	t.Helper()
	tbl := parseYmlForTest(t, yml)

	byName := map[string]*dbColumn{}
	for _, c := range dbColumnsFromYml(tbl) {
		byName[c.Name] = c
	}
	names := order
	if len(names) == 0 {
		for _, c := range tbl.Columns {
			names = append(names, c.Name)
		}
	}

	st := newDBTableState(tbl.Name)
	st.Exists = true
	st.Comment = tbl.Comment
	st.Collation = tbl.Collate
	for i, n := range names {
		c, ok := byName[n]
		if !ok {
			t.Fatalf("顺序里出现了配置中没有的字段 %s", n)
		}
		c.Position = i
		st.addColumn(c)
	}
	return tbl, st
}

const sortableYml = `
Table:
  table: demo
  options:
    charset: utf8mb4
    collate: utf8mb4_general_ci
    comment: 排序用例表
  id:
    id:
      type: integer unsigned
      nullable: false
      generator: AUTO_INCREMENT
      comment: 主键
  fields:
    a:
      type: varchar(32)
      nullable: false
      default: ''
      comment: A
    b:
      type: varchar(32)
      nullable: false
      default: ''
      comment: B
`

func TestCheckSortable(t *testing.T) {
	sortOpt := diffOption{dropPolicy: DropPolicyAlways, keepColumnOrder: true, syncCharset: false}

	// mutate 把库里某一列改出一个定义差异
	mutate := func(st *dbTableState, column string, fn func(*dbColumn)) {
		t.Helper()
		c, ok := st.column(column)
		if !ok {
			t.Fatalf("库里没有列 %s", column)
		}
		fn(c)
	}

	t.Run("结构一致只是顺序不同，可以排序", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml, "id", "b", "a")
		problems, err := checkSortable(tbl, st, sortOpt)
		if err != nil {
			t.Fatalf("不该报错：%v", err)
		}
		if len(problems) != 0 {
			t.Errorf("只有顺序不同不该拦着排序，实际：%v", problems)
		}
	})

	t.Run("表不存在", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml)
		st.Exists = false
		assertProblem(t, checkSortableProblems(tbl, st, sortOpt), "表在数据库里不存在")
	})

	t.Run("配置里多声明了一个字段", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml, "id", "a")
		assertProblem(t, checkSortableProblems(tbl, st, sortOpt), "yml 声明的字段库里没有：b")
	})

	t.Run("库里多了一个配置没声明的字段", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml)
		st.addColumn(&dbColumn{Name: "legacy", Decl: "varchar(32)", Position: len(st.Columns)})
		assertProblem(t, checkSortableProblems(tbl, st, sortOpt), "库里有 yml 未声明的字段：legacy")
	})

	t.Run("字段类型有差异", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml, "id", "b", "a")
		mutate(st, "a", func(c *dbColumn) { c.Decl = "varchar(64)" })
		problems := checkSortableProblems(tbl, st, sortOpt)
		assertProblem(t, problems, "修改列 a")
		assertProblem(t, problems, "请先执行 ExecuteSchema 同步结构")
	})

	t.Run("字段注释有差异", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml)
		mutate(st, "b", func(c *dbColumn) { c.Comment = "改了注释" })
		assertProblem(t, checkSortableProblems(tbl, st, sortOpt), "修改列 b")
	})

	t.Run("字段默认值有差异", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml)
		mutate(st, "a", func(c *dbColumn) { c.Default, c.DefaultSet = "x", true })
		assertProblem(t, checkSortableProblems(tbl, st, sortOpt), "修改列 a")
	})

	// MODIFY COLUMN 会把列定义整条重写，生成列表达式在配置里没有对应声明，
	// 重写就会把它抹掉，所以必须先拦住
	t.Run("带生成列的表不允许重排", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml)
		mutate(st, "b", func(c *dbColumn) { c.Extra = "VIRTUAL GENERATED" })
		problems := checkSortableProblems(tbl, st, sortOpt)
		assertProblem(t, problems, "无法从配置还原的属性(VIRTUAL GENERATED)")
	})

	// 字符集漂移默认是 Skipped，ExecuteSchema 也不会处理它，
	// 拿它当拦路条件会让使用者陷入"怎么同步都排不了序"的死循环
	t.Run("字符集漂移不拦排序", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml, "id", "b", "a")
		st.Collation = "utf8mb4_unicode_ci"
		if problems := checkSortableProblems(tbl, st, sortOpt); len(problems) != 0 {
			t.Errorf("字符集漂移不该拦着排序，实际：%v", problems)
		}
	})

	// DropPolicyNever 会让"库里多出来的字段"变成 Skipped，
	// 但字段集合对不上就是排不出严格顺序，这种必须拦
	t.Run("DropPolicyNever 下的多余字段仍然拦", func(t *testing.T) {
		tbl, st := sortableFixture(t, sortableYml)
		st.addColumn(&dbColumn{Name: "legacy", Decl: "varchar(32)", Position: len(st.Columns)})
		never := sortOpt
		never.dropPolicy = DropPolicyNever
		assertProblem(t, checkSortableProblems(tbl, st, never), "库里有 yml 未声明的字段：legacy")
	})
}

// checkSortableProblems 只取问题清单，忽略配置解析错误（其它用例已覆盖）
func checkSortableProblems(tbl *ymlTable, st *dbTableState, opt diffOption) []string {
	problems, _ := checkSortable(tbl, st, opt)
	return problems
}

// assertProblem 断言问题清单里有一条包含指定内容
func assertProblem(t *testing.T, problems []string, wantIn string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, wantIn) {
			return
		}
	}
	t.Errorf("应该报出包含 %q 的问题，实际问题清单：%v", wantIn, problems)
}

func TestSortReasonAndBriefList(t *testing.T) {
	if got := briefList([]string{"a", "b", "c"}); got != "a,b,c" {
		t.Errorf("短清单不该被截断，实际 %q", got)
	}
	long := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10"}
	got := briefList(long)
	if !strings.Contains(got, "共10个") || strings.Contains(got, "c9") {
		t.Errorf("超过 8 个应该截断并给出总数，实际 %q", got)
	}

	reason := sortReason([]string{"id", "b", "a"}, colsOf("id", "a", "b"),
		[]columnMove{{Column: &ymlColumn{Name: "a"}, Position: "AFTER `id`"}})
	for _, want := range []string{"[id,b,a]", "[id,a,b]", "需移动 1 个字段(a)", "重建整表"} {
		if !strings.Contains(reason, want) {
			t.Errorf("变更原因里缺少 %q，实际：%s", want, reason)
		}
	}
}
