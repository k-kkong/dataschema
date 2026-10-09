# 主要内容说明
致力于为基础数据提供高效而精准的处理方案。
## 
使用yaml配置文件，快速简便的管理数据库的表结构，提升开发效率：

[参考文档：ExecuteSchemaSafeCheck](https://pkg.go.dev/github.com/k-kkong/dataschema#example-YamlToSqlHandler.ExecuteSchemaSafeCheck)

### 表结构同步的案例在哪里看

三个入口，想看什么就去哪里：

| 想看什么 | 去哪里 |
| --- | --- |
| 每种能力怎么调（代码 + 注释） | [`example_test.go`](./example_test.go)，搜 `ExampleYamlToSqlHandler_` |
| 每种情况跑起来到底是什么样 | [`cmd/test_schema_cases`](./cmd/test_schema_cases)，一条命令跑完 30 个用例并自带断言 |
| yml 到底怎么写 | [`cmd/test_schema_cases/etc/`](./cmd/test_schema_cases/etc)，每个子目录就是“同一张表的一个版本”，文件头有注释 |

`cmd/test_schema_cases` 覆盖的情况：

建表、幂等复检、新增字段、改类型、改注释、改可空性与默认值、删除字段、
索引的增删改、全文索引、删除索引、主键（建表 / 改顺序 / 删除）、
字符集漂移、DropPolicy、分表、配置告警、配置报错、
DryRun 导出 SQL、编译产物回读、表过滤与排除、递归扫描、迁移历史、
字段排序（预览 / 执行 / 结构未同步时拒绝）。

每个用例都会走四步：

1. 先 DryRun 预览，把变更清单与期望值逐条比对（含“高危”与“跳过”标记）；
2. 真的执行；
3. 再比对一次，确认结构与配置一致时不会产生任何 SQL（幂等）；
4. 去 `information_schema` 复查数据库的真实状态（列顺序、索引列、注释、字符集……）。

跑之前先配好数据库连接，连接串带账号密码，不要写进代码：

```bash
export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
go run ./cmd/test_schema_cases
```

程序只会创建与清理 `ds_case_` 开头的表，库里的其它表一律不碰。
MySQL 5.7 与 8.0 都验证过（两个版本回读的元数据形态不同，断言已经兼容）。

常用参数：

```bash
go run ./cmd/test_schema_cases -v               # 打印每个用例的完整过程输出（默认只在失败时打印）
go run ./cmd/test_schema_cases -only 索引        # 只跑名字里带“索引”的用例
go run ./cmd/test_schema_cases -from "10 主键"   # 从某个用例开始跑到最后
go run ./cmd/test_schema_cases -no-reset        # 开始前不清理上一轮留下的用例表
```

> 用例之间有先后依赖（`ds_case_demo` 从 01 到 09 是一条演进链，24 依赖 23 已经建好表），
> 所以 `-only` 只适合已经跑过一整轮之后重复观察某个用例。

不想连数据库也能看的部分：解析、类型归一化、结构化比对、排序算法这四层都是纯函数，
直接 `go test .` 就能跑，不需要任何数据库。
其中 `schema_sort_test.go` 会穷举 1~6 个字段的全部 873 种排列，
验证排序结果正确且移动的字段数是最小的。

### 字段顺序：ExecuteSchema 与 SortFieldsWithYaml 的分工

这两个动作是分开的，因为代价完全不同：

| 方法 | 做什么 | 代价 |
| --- | --- | --- |
| `ExecuteSchema` / `ExecuteSchemaSafeCheck` | 同步结构：建表、增删改字段、索引、主键、表注释 | 新增的列按 yml 顺序落位；**已经存在的列一律不挪位置** |
| `SortFieldsWithYaml` / `SortFieldsWithYamlSafeCheck` | 只把字段顺序调成 yml 声明的顺序，不改任何列定义 | 用 `MODIFY COLUMN`，**MySQL 会重建整表**，耗时与磁盘占用跟数据量成正比 |

一般用法：平时只跑 `ExecuteSchema`；确实需要把存量表的字段顺序对齐时，
再单独接入 `SortFieldsWithYaml`，由使用者自己决定什么时机做。

排序的几个要点：

- **前提是结构已经与配置一致**。字段多一个少一个、类型/注释/默认值/索引有差异，
  会把问题一次全部列出来并终止，提示先执行 `ExecuteSchema`。
  这是因为排序要重述列定义，带着差异排序会顺带改掉它。
- **移动的字段数取最小值**：等于总字段数减去两个顺序的最长公共子序列长度。
  典型场景（后补的字段被追加到了表尾）往往只需要挪 1 个。
- **全部移动合并进同一条 `ALTER TABLE`**，MySQL 只重建一次整表；
  拆成多条语句就变成挪几列重建几次。
- **带生成列、空间参考系、列不可见这类无法从配置还原的属性时拒绝重排**，
  因为 `MODIFY COLUMN` 会把它们抹掉。
- 大表建议先 `SetDryRun(true)` + `SetSqlExportPath` 把语句导出走 DBA 审核，
  或改用 gh-ost / pt-online-schema-change 执行导出的语句。

对应的可运行用例是 `cmd/test_schema_cases` 里的 26 / 27 / 28，
配置在 [`etc/sort_fields`](./cmd/test_schema_cases/etc/sort_fields)、
[`etc/sort_scrambled`](./cmd/test_schema_cases/etc/sort_scrambled)、
[`etc/sort_dirty`](./cmd/test_schema_cases/etc/sort_dirty) 三个目录。


## 
可以将数据库的表结构，一键翻译成go语言的结构体，避免繁琐的手写字段，提升开发效率

[参考文档：GenerateAllTblStruct](https://pkg.go.dev/github.com/k-kkong/dataschema#example-TblToStructHandler.GenerateAllTblStruct)

## 
使用dvap2 ，可快捷加载嵌套的数据结构，实现动态的结构加载，可省去定义不同的结构体的内外键，解藕结构体防止互相引用，提升开发效率

[参考文档：NewRelationLoader](https://pkg.go.dev/github.com/k-kkong/dataschema/dvap2#NewRelationLoader)

##
任意切片数据处理，Map,Reduce,Find查找符合,take查找一个,Divide分割,Page翻页,Pop,remove,sort,判断....等许多操作，详细参考

[参考文档：NewSlicer](https://pkg.go.dev/github.com/k-kkong/dataschema/dvap#NewSlicer)

## 
提供了一些专注于处理数据结构的func，可以提升处理数据的开放效率

## 其他使用案例参考各个test文件
#### dataer用法
```go
package main

import (
	"fmt"
	"github.com/k-kkong/dataschema/dvap"
)

func main() {
	dataer := dvap.NewDataer()
	// Do something
}
```






