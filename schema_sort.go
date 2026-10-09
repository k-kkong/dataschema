package dataschema

import (
	"fmt"
	"strings"
)

// 本文件负责"把数据库里的字段顺序调整成 yml 声明的顺序"。
//
// 排序与结构同步是两件独立的事，入口也分开：
//   - ExecuteSchema / ExecuteSchemaSafeCheck 只同步结构，新增的列按 yml 顺序落位，
//     但已经存在的列一律不挪位置；
//   - SortFieldsWithYaml / SortFieldsWithYamlSafeCheck 只挪位置，不改任何定义。
//
// 之所以分开：挪动已有字段用的是 MODIFY COLUMN，MySQL 会为它重建整表，
// 代价与表的数据量成正比，属于必须由使用者显式决定的操作，
// 不该藏在"同步结构"这个日常动作里顺带发生。
//
// 也正因为只挪位置，排序要求结构与配置已经完全一致：
// 字段多一个少一个都无法确定"严格顺序"，定义有差异时重述列定义还会顺带改掉它。
// 检测到任何差异就列清楚原因并终止，提示先执行 ExecuteSchema。

// columnMove 一次字段位置调整
type columnMove struct {
	Column   *ymlColumn // 要移动的列，MODIFY COLUMN 需要它的完整定义
	Position string     // 目标位置：FIRST 或 AFTER `x`
}

// lcsKeep 找出 dbOrder 与 wantOrder 的最长公共子序列，返回可以原地不动的列名集合。
//
// 不在这个集合里的列都必须移动，所以公共子序列越长、要移动的列越少、重建代价越小。
// 两个入参都是不含重复元素的列名清单（调用前已经校验过字段集合一致）。
func lcsKeep(dbOrder, wantOrder []string) map[string]bool {
	n, m := len(dbOrder), len(wantOrder)
	// dp[i][j] = dbOrder[i:] 与 wantOrder[j:] 的最长公共子序列长度
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case dbOrder[i] == wantOrder[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	keep := make(map[string]bool, dp[0][0])
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case dbOrder[i] == wantOrder[j]:
			keep[dbOrder[i]] = true
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return keep
}

// planColumnSort 计算把库里的列顺序调整成 yml 声明顺序所需的最小移动集合。
// 返回 nil 表示顺序已经一致，不需要任何操作。
//
// 移动子句严格按目标顺序排列，第 i 个移动的位置参照物是目标顺序里的第 i-1 列，
// 而第 i-1 列要么原地不动、要么已经在前面的子句里挪到位了，
// 所以合并进同一条 ALTER TABLE 时按子句顺序依次生效即可得到目标顺序。
func planColumnSort(cols []*ymlColumn, dbOrder []string) []columnMove {
	if len(cols) == 0 {
		return nil
	}
	var (
		wantOrder = make([]string, 0, len(cols))
		byName    = make(map[string]*ymlColumn, len(cols))
	)
	for _, c := range cols {
		wantOrder = append(wantOrder, c.Name)
		byName[c.Name] = c
	}

	keep := lcsKeep(dbOrder, wantOrder)
	var moves []columnMove
	for i, name := range wantOrder {
		if keep[name] {
			continue
		}
		position := "FIRST"
		if i > 0 {
			position = "AFTER " + quoteIdent(wantOrder[i-1])
		}
		moves = append(moves, columnMove{Column: byName[name], Position: position})
	}
	return moves
}

// applyColumnMoves 按顺序执行移动，返回调整后的列顺序。
// 只用于测试与自检：它复刻的就是 MySQL 处理一条 ALTER TABLE 里多个
// MODIFY COLUMN ... FIRST/AFTER 子句的方式。
func applyColumnMoves(order []string, moves []columnMove) []string {
	out := make([]string, 0, len(order)+len(moves))
	out = append(out, order...)
	for _, m := range moves {
		name := m.Column.Name
		// 先摘出来
		at := -1
		for i, n := range out {
			if n == name {
				at = i
				break
			}
		}
		if at < 0 {
			continue
		}
		out = append(out[:at], out[at+1:]...)

		// 再插到目标位置
		insert := 0
		if strings.EqualFold(m.Position, "FIRST") {
			insert = 0
		} else if after := strings.Trim(strings.TrimPrefix(m.Position, "AFTER "), "`"); after != "" {
			for i, n := range out {
				if n == after {
					insert = i + 1
					break
				}
			}
		}
		out = append(out[:insert], append([]string{name}, out[insert:]...)...)
	}
	return out
}

// understoodExtras MODIFY COLUMN 能完整还原的 EXTRA 取值。
//
// 重排字段必须把列定义整条重述一遍，所以只有这些标记敢动：
//   - 空
//   - auto_increment
//   - on update CURRENT_TIMESTAMP
//   - DEFAULT_GENERATED（MySQL 8.0.13 起，表示默认值是 CURRENT_TIMESTAMP 这类表达式）
//
// 出现 VIRTUAL GENERATED / STORED GENERATED / SRID / INVISIBLE 等标记时，
// 生成列表达式、空间参考系、列可见性在 yml 里都没有对应声明，
// 重述定义会把它们抹掉，所以直接拒绝重排并说明原因。
var understoodExtras = map[string]bool{
	"":                            true,
	"auto_increment":              true,
	"on update current_timestamp": true,
	"auto_increment on update current_timestamp": true,
}

// columnExtraUnderstood 判断回读的 EXTRA 是否属于能完整还原的那几种
func columnExtraUnderstood(extra string) bool {
	s := strings.ToLower(strings.TrimSpace(extra))
	// DEFAULT_GENERATED 只是"默认值是表达式"的标记，与位置无关，去掉后再比对
	s = strings.TrimSpace(strings.ReplaceAll(s, "default_generated", ""))
	return understoodExtras[s]
}

// checkSortable 检查一张表能不能直接排序，返回不能排序的全部原因（为空表示可以）。
//
// 一次性把所有问题都收集起来再报，使用者改一轮就能过；
// 只报第一个的话，几十张表要来回试几十次。
func checkSortable(t *ymlTable, st *dbTableState, opt diffOption) ([]string, error) {
	if !st.Exists {
		return []string{"表在数据库里不存在，请先执行 ExecuteSchema 建表"}, nil
	}

	var problems []string

	// ---- 字段集合必须完全一致，多一个少一个都排不出"严格顺序" ----
	var missing, extra []string
	for _, c := range t.Columns {
		if _, ok := st.column(c.Name); !ok {
			missing = append(missing, c.Name)
		}
	}
	for _, dbCol := range st.Columns {
		if _, ok := t.column(dbCol.Name); !ok {
			extra = append(extra, dbCol.Name)
		}
	}
	if len(missing) > 0 {
		problems = append(problems,
			"yml 声明的字段库里没有："+briefList(missing)+"，请先执行 ExecuteSchema 同步结构")
	}
	if len(extra) > 0 {
		problems = append(problems,
			"库里有 yml 未声明的字段："+briefList(extra)+
				"，无法确定它们该排在哪里；请先在 yml 中补齐声明，或执行 ExecuteSchema 同步结构")
	}

	// ---- 其余差异也要求先同步：排序只挪位置，不改定义 ----
	changes, _, err := diffTable(t, st, opt)
	if err != nil {
		return nil, err
	}
	for _, c := range changes {
		// Skipped 的差异 ExecuteSchema 同样不会处理（未开启字符集同步、DropPolicyNever、
		// 不维护的索引类型），拿它们当拦路条件会让使用者陷入死循环，所以放过
		if c.Skipped {
			continue
		}
		switch c.Kind {
		case ChangeAddColumn, ChangeDropColumn:
			// 上面已经用更明确的话说过字段集合的问题了
			continue
		}
		problems = append(problems,
			fmt.Sprintf("%s %s：%s，请先执行 ExecuteSchema 同步结构", c.Kind, c.Object, c.Reason))
	}

	// ---- 带无法还原属性的字段一律不动 ----
	for _, dbCol := range st.Columns {
		if columnExtraUnderstood(dbCol.Extra) {
			continue
		}
		problems = append(problems,
			fmt.Sprintf("字段 %s 带有无法从配置还原的属性(%s)，重排会丢失它，请手动处理",
				dbCol.Name, dbCol.Extra))
	}
	return problems, nil
}

// sortReason 生成排序变更的原因说明：现状顺序、目标顺序、需要移动哪些列
func sortReason(dbOrder []string, cols []*ymlColumn, moves []columnMove) string {
	want := make([]string, 0, len(cols))
	for _, c := range cols {
		want = append(want, c.Name)
	}
	moved := make([]string, 0, len(moves))
	for _, m := range moves {
		moved = append(moved, m.Column.Name)
	}
	return fmt.Sprintf("[%s] -> [%s]，需移动 %d 个字段(%s)，MODIFY COLUMN 会重建整表",
		briefList(dbOrder), briefList(want), len(moves), briefList(moved))
}

// briefList 把列名清单压成一行，超过 8 个就省略中间部分，
// 免得几十列的宽表把变更报告刷满屏
func briefList(names []string) string {
	const max = 8
	if len(names) <= max {
		return strings.Join(names, ",")
	}
	return fmt.Sprintf("%s,...共%d个", strings.Join(names[:max], ","), len(names))
}
