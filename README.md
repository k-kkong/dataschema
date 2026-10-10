# dataschema

[![Go Reference](https://pkg.go.dev/badge/github.com/k-kkong/dataschema.svg)](https://pkg.go.dev/github.com/k-kkong/dataschema)
[![Go Version](https://img.shields.io/badge/go-1.21%2B-00ADD8.svg)](https://go.dev/doc/devel/release)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

几件反复要做、每次又得重写一遍的事，这里做成了库：

- **表结构同步**：yml 写表的目标结构，程序比对 `information_schema` 后只生成有差异的那部分 SQL，建表、字段增删改、类型、注释、可空性与默认值、索引、主键都在范围内。配置与库一致时不产生语句，可以重复执行；中间改过几轮不用记，只看最终结构。上线前可以 DryRun 预览、导出 SQL 交 DBA，变更报告里会标出删列、改主键这类高危项。分表展开、字段顺序对齐、字符集漂移、只发布加密编译产物另有开关。
- **表结构转 Go 模型**：根据库里已有的表生成结构体，字段名写法、标签、包名、时间类型都可以配，省去手写字段。
- **嵌套数据组装**：主数据查出来后，用 `student.class`、`teachers.user.candies` 这样的关系名声明谁挂谁、拿哪两个字段对上，外键汇总成一次 `IN` 查询再在内存里挂载，不写循环也没有 N+1。结果是动态结构，不必为每种组合再定义一套互相引用的 struct。
- **任意结构数据读写**：接口响应、JSON 列、`map[string]any`、结构体都能 `Parse` 成一个视图，按路径 `Get` / `Set` / `Delete` 读写任意层级（如 `data.list.0.user.name`），中间缺哪一层都不 panic，也不用层层判空。
- **泛型切片处理**：筛选、分组、排序、分页、集合运算、并发，52 个方法链式调用，覆盖日常所需，内部带锁。

## 目录

- [快速开始](#快速开始)
- [能力总览](#能力总览)
- [表结构同步](#表结构同步)
- [字段顺序对齐](#字段顺序对齐)
- [表结构转 Go 结构体](#表结构转-go-结构体)
- [bmap 任意 JSON 形态数据的惰性视图](#bmap-任意-json-形态数据的惰性视图)
- [dvap2 嵌套数据动态加载](#dvap2-嵌套数据动态加载)
- [gslicer 泛型切片处理](#gslicer-泛型切片处理)
- [文档与案例索引](#文档与案例索引)
- [测试](#测试)
- [仓库结构](#仓库结构)
- [实验性能力](#实验性能力)
- [约定](#约定)
- [License](#license)

## 快速开始

### 安装

```bash
go get github.com/k-kkong/dataschema
```

各包的依赖差别很大，按需引入：

| 包 | 第三方依赖 |
| --- | --- |
| `dataschema`（根包） | `gorm.io/gorm`、`gorm.io/driver/mysql`、`gopkg.in/yaml.v3`、`tidwall/gjson` |
| `bmap` | `tidwall/gjson` |
| `gslicer` | 无，只用标准库 |
| `dvap2` | `gorm.io/gorm`、本仓库的 `bmap` 与 `gslicer`、`tidwall/gjson`、`tidwall/sjson`、`novalagung/gubrak` |

只导入 `gslicer` 或 `bmap` 时，gorm 不会进入你的构建依赖。

### 我该用哪个

| 你的问题 | 用这个 | 入口 |
| --- | --- | --- |
| 想把建表语句变成可版本管理的配置 | [表结构同步](#表结构同步) | `dataschema.NewYamlToSqlHandler()` |
| 表已经建好了，想快速拿到 Go 模型 | [表结构转 Go 结构体](#表结构转-go-结构体) | `dataschema.NewTblToStructHandler()` |
| 接口返回的 JSON 层级不固定，取值到处判空 | [bmap](#bmap-任意-json-形态数据的惰性视图) | `bmap.Parse(x)` |
| 主数据查出来了，想按外键批量挂上子表 | [dvap2](#dvap2-嵌套数据动态加载) | `dvap2.NewRelationLoader(...)` |
| 切片要过滤 / 分组 / 分页 / 并发处理 | [gslicer](#gslicer-泛型切片处理) | `gslicer.NewSlicer(...)` |

### 环境要求

- Go 1.21+
- MySQL 5.7 / 8.0（仅表结构同步与模型生成需要；`bmap`、`gslicer`、`dvap2` 的内存计算部分不需要数据库）
- 数据库连接串一律走环境变量 `APP_MYSQL_DSN`，不要写进代码：

```bash
export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
```

## 能力总览

| 能力 | 包 | 入口 | 需要数据库 |
| --- | --- | --- | --- |
| yml 声明表结构并同步到库 | `dataschema`（根包） | `NewYamlToSqlHandler()` | 是 |
| 把存量表字段顺序调成 yml 声明的顺序 | `dataschema`（根包） | `SortFieldsWithYaml()` | 是 |
| 表结构生成 Go 结构体 | `dataschema`（根包） | `NewTblToStructHandler()` | 是 |
| 任意 JSON 形态数据的惰性视图 | [`bmap`](./bmap) | `bmap.Parse()` | 否 |
| 按外键关系批量组装嵌套数据 | [`dvap2`](./dvap2) | `dvap2.NewRelationLoader()` | 是 |
| 泛型切片处理（52 个方法） | [`gslicer`](./gslicer) | `gslicer.NewSlicer()` | 否 |

## 表结构同步

用 yml 描述表结构，`ExecuteSchema` 会把数据库同步到 yml 声明的状态：
建表、增删改字段、改类型、改注释、改可空性与默认值、增删改索引、改主键，一次完成。

使用者不需要判断"库里现在是什么状态"——它会先把 yml 解析成结构描述，
再去 `information_schema` 把库里的真实结构拉回来，两边比对之后**只针对差异生成 SQL**。
同一份配置反复执行是安全的：结构与 yml 一致时不会产生任何 SQL。

```go
import "github.com/k-kkong/dataschema"

dataschema.NewYamlToSqlHandler().
	SetDsn(os.Getenv("APP_MYSQL_DSN")).
	SetYamlPath("./etc/").
	ExecuteSchema()
```

项目里已经有全局 `*gorm.DB` 时用 `SetDB(db)` 复用，不让本库再建一条连接；
两个都调时以 `SetDB` 为准。

[参考文档：ExecuteSchema](https://pkg.go.dev/github.com/k-kkong/dataschema#example-YamlToSqlHandler.ExecuteSchema)
· [ExecuteSchemaSafeCheck](https://pkg.go.dev/github.com/k-kkong/dataschema#example-YamlToSqlHandler.ExecuteSchemaSafeCheck)

### 不用记录每一次结构变更

按迁移脚本的做法，测试环境每改一轮就要留一个脚本：

- 阶段 1：`表1` 加字段 `a`、`b`，删字段 `c`；`表2` 加字段 `ccc`；
- 阶段 2：`表1` 删掉 `b`，再加 `d`。

上生产时这些脚本要按顺序重放一遍，漏一个、顺序错一个，或者线上手工改过一点，结构就和测试环境对不上了。

这里不需要维护这串脚本。yml 写的是表最终的结构，程序拿它和线上真实结构比对，算出这一次要执行哪些 SQL：上面两个阶段结束后 yml 里是 `表1(a, d)`、`表2(ccc)`，生产库停在哪个中间状态都一样，同步一次就得到最终结果；已经一致的部分不产生语句，加了又删的 `b` 不会出现在 SQL 里。

想看这次上线动了什么，`SetDryRun(true)` 跑一遍看报告就行，不用翻变更记录。

### 上线前的三道闸

大表变更前建议按这个顺序走一遍：

| 闸口 | 怎么开 | 作用 |
| --- | --- | --- |
| 只看不动 | `SetDryRun(true)` | 无论调哪个 Execute 都只生成 SQL，一条都不执行 |
| 结构化变更报告 | `GetChangeReport()` | 返回 `[]SchemaChange` 而不是字符串，可自己判定：`Dangerous` 是删列/删索引/改主键这类会丢数据或锁表的；`Skipped` 是有差异但按当前策略不执行（一定不带 SQL） |
| 交给 DBA | `SetSqlExportPath(path)` | 把语句写成文件走审核流程 |

确认没有高危项后再放行：`ExecuteSchemaSafeCheck()` 执行前会要求输入 `Y` 确认，
不需要交互就用 `SetDryRun(false).ExecuteSchema()`。

其余常用开关：`SetDropPolicy`（yml 里删掉的字段/索引只报告不删）、
`SetSyncTableCharset`（表字符集与排序规则漂移）、
`SetTableFilter` / `SetTableExclude`（只同步部分表，支持 `*` `?` 通配符）、
`SetRecursive`（递归扫描子目录，默认只扫 `YamlPath` 本层）、
`SetMigrationHistory`（中断后接着跑，不重复执行已成功的语句）、
`SetIsOutputBuildSchema` + `SetBuildSchemaDest`（只发布编译产物、不发布 yml，可加密，启动时自检线上结构）、
`GetWarnings`（配置写错了告警但不中断）。

分表：一份定义可以展开成多张表，见
[ExecuteSchema_sharding](https://pkg.go.dev/github.com/k-kkong/dataschema#example-YamlToSqlHandler.ExecuteSchema_sharding)。

### 案例与回归程序

三个入口：

| 想看什么 | 去哪里 |
| --- | --- |
| 每种能力怎么调（代码 + 注释） | [`example_test.go`](./example_test.go)，搜 `ExampleYamlToSqlHandler_` |
| 每种情况跑起来到底是什么样 | [`cmd/test_schema_cases`](./cmd/test_schema_cases)，一条命令跑完 30 个用例并自带断言 |
| yml 到底怎么写 | [`cmd/test_schema_cases/etc/`](./cmd/test_schema_cases/etc)，每个子目录就是"同一张表的一个版本"，文件头有注释 |

`cmd/test_schema_cases` 覆盖的情况：

建表、幂等复检、新增字段、改类型、改注释、改可空性与默认值、删除字段、
索引的增删改、全文索引、删除索引、主键（建表 / 改顺序 / 删除）、
字符集漂移、DropPolicy、分表、配置告警、配置报错、
DryRun 导出 SQL、编译产物回读、表过滤与排除、递归扫描、迁移历史、
字段排序（预览 / 执行 / 结构未同步时拒绝）。

每个用例都会走四步：

1. 先 DryRun 预览，把变更清单与期望值逐条比对（含"高危"与"跳过"标记）；
2. 真的执行；
3. 再比对一次，确认结构与配置一致时不会产生任何 SQL（幂等）；
4. 去 `information_schema` 复查数据库的真实状态（列顺序、索引列、注释、字符集……）。

```bash
export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
go run ./cmd/test_schema_cases
```

程序只会创建与清理 `ds_case_` 开头的表，库里的其它表一律不碰。
MySQL 5.7 与 8.0 都验证过（两个版本回读的元数据形态不同，断言已经兼容）。

常用参数：

```bash
go run ./cmd/test_schema_cases -v               # 打印每个用例的完整过程输出（默认只在失败时打印）
go run ./cmd/test_schema_cases -only 索引        # 只跑名字里带"索引"的用例
go run ./cmd/test_schema_cases -from "10 主键"   # 从某个用例开始跑到最后
go run ./cmd/test_schema_cases -no-reset        # 开始前不清理上一轮留下的用例表
```

> 用例之间有先后依赖（`ds_case_demo` 从 01 到 09 是一条演进链，24 依赖 23 已经建好表），
> 所以 `-only` 只适合已经跑过一整轮之后重复观察某个用例。

## 字段顺序对齐

`ExecuteSchema` 默认**不挪已经存在的列**：新增的列按 yml 顺序落位，存量列保持原位。
这是刻意的——调列顺序在 MySQL 上意味着重建整表。

确实需要把存量表的字段顺序对齐到 yml 时，单独接入 `SortFieldsWithYaml`，由使用者自己决定什么时机做：

| 方法 | 做什么 | 代价 |
| --- | --- | --- |
| `ExecuteSchema` / `ExecuteSchemaSafeCheck` | 同步结构：建表、增删改字段、索引、主键、表注释 | 新增的列按 yml 顺序落位；**已经存在的列一律不挪位置** |
| `SortFieldsWithYaml` / `SortFieldsWithYamlSafeCheck` | 只把字段顺序调成 yml 声明的顺序，不改任何列定义 | 用 `MODIFY COLUMN`，**MySQL 会重建整表**，耗时与磁盘占用跟数据量成正比 |

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

排序算法本身是纯函数，不连数据库也能验证：`schema_sort_test.go` 会穷举 1~6 个字段的全部 873 种排列，
验证排序结果正确且移动的字段数是最小的。

## 表结构转 Go 结构体

根据库里已有的表生成 Go 结构体，省去手写字段。

```go
// 最简：库里所有表都生成，写到 ./tbl_<表名>/schema_model.go
dataschema.NewTblToStructHandler().
	SetDsn(os.Getenv("APP_MYSQL_DSN")).
	GenerateAllTblStruct()
```

```go
// 定制：标签、字段名写法、排序方式、结构体名前后缀
dataschema.NewTblToStructHandler().
	SetDsn(os.Getenv("APP_MYSQL_DSN")).
	SetStructOrmTag(dataschema.GORM).              // orm 标签用 gorm 还是 orm
	SetOtherTags("json", "msg").                   // 追加 `json:"xxx"` `msg:"xxx"`
	SeTblStructColumnNameInfo(
		dataschema.CAMEL_CASE,                     // 字段名写法
		dataschema.FIELD_ORDER_ORDINAL_POSITION,   // 按建表顺序排，还是按字段名字典序
		"", "",                                    // 字段名前后缀
	).
	SetTblStructNameInfo(dataschema.CAMEL_CASE, "Tbl", "").
	SetTimeType(dataschema.TIMETYPE_TIME).         // 时间列生成 time.Time 还是 string
	SetIsNullableValuePoint(true).                 // 可空列生成指针类型
	GenerateAllTblStruct()
```

单表生成用 `SetTableName(t)` + `SetSavePath(p)` + `SetPackageInfo(pkg, prefix, suffix)` + `GenerateTblStruct()`；
`GetAllTableNames()` 可以只列出库里的表名，自己决定生成哪些。

> `GenerateAllTblStruct()` 会自己设定每张表的保存路径与包名（`./tbl_<表名>/schema_model.go`），
> 之前设过的 `SetSavePath` / `SetPackageInfo` 会被覆盖；要完全控制输出位置就逐表调 `GenerateTblStruct()`。

可用常量：`CAMEL_CASE` / `FIRST_UPPER`、`ORM` / `GORM`、
`TIMETYPE_STRING` / `TIMETYPE_TIME`、`FIELD_ORDER_FIELD_NAME` / `FIELD_ORDER_ORDINAL_POSITION`。

[参考文档：GenerateAllTblStruct](https://pkg.go.dev/github.com/k-kkong/dataschema#example-TblToStructHandler.GenerateAllTblStruct)
· 生成结果样例见 [`all_tbl_model/`](./all_tbl_model)、[`cmd/test_alltabl_to_model/`](./cmd/test_alltabl_to_model)

## bmap 任意 JSON 形态数据的惰性视图

拿到一份结构不固定的数据（接口响应、数据库里的 JSON 列、`map[string]any`、结构体……），
用一个入口按路径取任意深度的值，不必为每种响应体先定义一个结构体：

```go
bm := bmap.Parse(resp)
name := bm.Get("data.list.0.user.name").String()   // 中间任何一层不存在都不会 panic
```

| 要点 | 说明 |
| --- | --- |
| 链式空安全 | 走不通返回"不存在的节点"而不是 nil，取值器给零值；要区分"值是零"和"路径不存在"用 `IsExists()` |
| 取值宽容 | `String/Int/Float/Bool/Time` 尽力转换，口径向 tidwall/gjson 看齐 |
| 读写双向 | `Get` 取值、`Set` 写值、`Delete` 删值、`Fill`/`Scan` 回填结构体 |
| 结构体按 tag 展开 | 默认 `json`，`Parse(form, "form")` 可指定；展开规则与 `encoding/json` 大体对齐 |

`Array()` 的语义是"把内容转成数组，然后看有几个"，所以标量也会得到长度 1 的切片。
接口有时返回一条记录、有时返回一批记录时，可以用同一段 `for range` 处理；
要区分"真数组"和"被提升的标量"先用 `IsArray()`，要元素个数用 `Len()`。

`Set` 有两个特殊规则：路径段是纯数字时容器按数组处理（长度不够补 `null`）；
键名字面量以数字开头、不希望被当成下标时加 `##` 前缀。
键名里本身含点时，`Get`/`Set`/`Delete` 都支持用 `\.` 转义。

[参考文档：Parse](https://pkg.go.dev/github.com/k-kkong/dataschema/bmap#Parse)

### 案例在哪里看

| 想看什么 | 去哪里 |
| --- | --- |
| 每个方法怎么用（首选入口） | [`bmap/example_test.go`](./bmap/example_test.go)，67 个可运行案例，每个都带 `// Output:`，pkg.go.dev 上展示的也是校验过的结果 |
| 每条既有规则与边界行为 | [`bmap/bmap_test.go`](./bmap/bmap_test.go)、[`bmap/structpkg_test.go`](./bmap/structpkg_test.go)、[`bmap/fill_test.go`](./bmap/fill_test.go)，文件头与逐条注释写明了每条规则存在的理由 |
| 包的设计意图与路径语法 | [`bmap/lib.go`](./bmap/lib.go) 的包注释 |
| 各项操作的开销 | [`bmap/benchmark_test.go`](./bmap/benchmark_test.go) |

案例不碰数据库、不读文件、不依赖机器时区，所以在任何机器上输出都一样：

```bash
go test -run Example ./bmap/                      # 只跑案例，逐字比对输出
go test ./bmap/                                   # 行为契约 + 案例
go test -run XXX -bench . -benchmem ./bmap/       # 基准
```

## dvap2 嵌套数据动态加载

把**已经查出来的主数据**按外键关系批量补齐成嵌套结构，
不必为每种组合再定义一套互相引用的 struct，也避免 N+1 查询。

> 你先用 GORM 查出一批用户 / 班级 / 订单；再告诉本包"谁挂谁、用哪两个字段对上"；
> 它会自动 `IN` 查询、按关系树组装，最后给你一棵可直接返回给前端的对象树。

```go
var users []TblUser
db.Find(&users)

rl := dvap2.NewRelationLoader(users, true) // true = 出错时打印堆栈
rl.AddRelation(dvap2.HAS_ONE,  "student",       "id",       "user_id", TblStudent{}, nil, nil)
rl.AddRelation(dvap2.HAS_ONE,  "student.class", "class_id", "id",      TblClass{},   nil, nil)
rl.AddRelation(dvap2.HAS_MANY, "teachers",      "id",       "user_id", TblTeacher{}, nil, nil)

result := rl.LoadResult(db).GetResult()
if err := rl.Error(); err != nil {
	return err
}
fmt.Println(dvap2.VtoJsonString(result))
```

关系名里的 `.` 表示挂到上一层关系下，所以多层嵌套是**声明**出来的，不是写循环拼出来的。
输入可以是切片，也可以是单个对象，组装逻辑相同。

| 手写 | dvap2 |
| --- | --- |
| 每个父行再查一次子表（N+1） | 先收集全部外键，一次 `IN` 查出子表，再在内存里挂上 |
| 多层嵌套要写很多组装代码 | 用 `student.class`、`teachers.user.candies` 声明关系网 |
| 不同接口要定义不同 DTO | 结果是动态 map/数组，结构由关系名决定 |

**完整文档见 [`dvap2/使用说明.md`](./dvap2/使用说明.md)**，含关系类型、自定义比对函数、
自定义子查询、挂载时改字段、`Dataer`（内存里手动连接）、性能与并发、常见坑、API 速查。

[参考文档：NewRelationLoader](https://pkg.go.dev/github.com/k-kkong/dataschema/dvap2#NewRelationLoader)

## gslicer 泛型切片处理

`Slicer[T]` 是泛型切片工具，52 个方法，链式调用，内部带锁：

```go
s := gslicer.NewSlicer(orders, true)   // 第二个参数 true 会先复制一份，避免改到原切片

// 筛选 -> 分组
paid := s.Filter(func(o Order) bool { return o.Amount > 0 })
byUser := paid.GroupBy(func(o Order) any { return o.UserID })   // *GroupData[Order]
orders1001 := byUser.Get(1001)                                  // []Order

// 遍历的回调带下标，返回 false 提前中断
paid.Foreach(func(idx int, o Order) bool {
	fmt.Println(idx, o.Name)
	return true
})

// 按自定义优先级排序，再分页
byStatus := s.SortByField(
	func(o Order) any { return o.Status },
	[]string{"paid", "shipped", "done"},   // 不在里面的值排到最后
)
page := byStatus.Page(20, 10)

// 普通的升降序用 Sort
desc := s.Sort(func(a, b Order) bool { return a.CreatedAt > b.CreatedAt })

// 并发批处理：panic 会被捕获并转成 error，不会让整个进程挂掉
err := s.ConcurrencyErr(ctx, func(ctx context.Context, o Order) error {
	return handle(ctx, o)
}, 8)
```

> `SortByField` 的第二个参数是「字段值的期望顺序」切片，**不是** `"asc"` / `"desc"`；
> 传字符串进去会直接原样返回、不排序也不报错。升降序用 `Sort`。

能力分组：

| 分组 | 方法 |
| --- | --- |
| 筛选与查找 | `Filter` `Find` `Take` `First` `Last` `At` `IndexOf` `LastIndexOf` `Contains` `InSlice` `Count` `All` `None` |
| 分组与去重 | `GroupBy` `KeyBy` `Unique` |
| 排序与重排 | `Sort` `SortByField` `Reverse` `Shuffle` `Rand` |
| 增删改 | `Append` `Prepend` `InsertIdx` `Remove` `RemoveByIdx` `PopHead` `PopTail` `PopIdx` `PopWhere` `ForeachModify` |
| 分页与分批 | `Page` `TakeN` `SkipN` `Batch` `BatchForeach` `Divide` |
| 集合运算 | `Union` `Intersection` `Difference` `SymmetricDifference` |
| 并发 | `Concurrency` `ConcurrencyIdx` `ConcurrencyErr`（带 ctx，收集错误） |
| 遍历与拷贝 | `Foreach` `Clone` `Data` `DataCopy` `Len` `IsEmpty` `IsNotEmpty` |

`GroupBy` 返回的 `*GroupData[T]` 还可以 `Keys` `Values` `Values2Dim` `ValuesSlic` `Has` `Get` `Set` `Delete` `Foreach`。

> `NewSlicer` 默认**不**复制输入切片，而 `Sort` `Reverse` `Shuffle` `Rand` `ForeachModify`
> 是就地改 `s.data`，会写穿到调用方手里的切片；不想被影响就传 `NewSlicer(x, true)`。
> （`SortByField` `Filter` `Remove` 这些是新建切片，不写穿。）
> 取结果时 `Data()` 返回的是内部切片本身，外部改动会反过来影响 Slicer，
> 要一份能安全带走的副本用 `DataCopy()`。

[参考文档：NewSlicer](https://pkg.go.dev/github.com/k-kkong/dataschema/gslicer#NewSlicer)

## 文档与案例索引

| 想了解 | 去哪里 |
| --- | --- |
| 表结构同步的每种能力怎么调 | [`example_test.go`](./example_test.go)，搜 `ExampleYamlToSqlHandler_` |
| 表结构同步跑起来的真实效果 | [`cmd/test_schema_cases`](./cmd/test_schema_cases) |
| yml 怎么写 | [`cmd/test_schema_cases/etc/`](./cmd/test_schema_cases/etc) |
| 模型生成的输出长什么样 | [`all_tbl_model/`](./all_tbl_model)、[`cmd/test_tbl_to_model/`](./cmd/test_tbl_to_model) |
| bmap 每个方法的用法 | [`bmap/example_test.go`](./bmap/example_test.go) |
| dvap2 完整用法 | [`dvap2/使用说明.md`](./dvap2/使用说明.md) |
| gslicer 的行为契约 | [`gslicer/slicer_test.go`](./gslicer/slicer_test.go) |
| pkg.go.dev 上的 API 文档 | [根包](https://pkg.go.dev/github.com/k-kkong/dataschema) · [bmap](https://pkg.go.dev/github.com/k-kkong/dataschema/bmap) · [dvap2](https://pkg.go.dev/github.com/k-kkong/dataschema/dvap2) · [gslicer](https://pkg.go.dev/github.com/k-kkong/dataschema/gslicer) |

## 测试

不需要数据库就能跑的部分：

```bash
go test ./bmap/ ./gslicer/     # 行为契约与案例
go test .                      # yml 解析、类型归一化、结构化比对、字段排序（纯函数）
go test -race ./bmap/ ./gslicer/
```

需要数据库的部分：

```bash
export APP_MYSQL_DSN="user:pwd@(127.0.0.1:3306)/test_kk?charset=utf8mb4&parseTime=True&loc=Local"
go run ./cmd/test_schema_cases          # 30 个用例，自带断言与 information_schema 复查
go run ./cmd/test_schema_cases -v       # 打印完整过程输出
```

`example_test.go` 里的 Example 都没有 `// Output:` 注释，按 Go 的约定"只编译不执行"，
所以 `go test ./...` 只会校验写法，不会去动你的数据库；
`bmap/example_test.go` 里的 67 个案例都带 `// Output:`，会真的执行并逐字比对输出。

## 仓库结构

```
.
├── *.go                    根包 dataschema：yml -> 表结构同步、表结构 -> Go 结构体
│   ├── yaml_to_sql_manager.go    YamlToSqlHandler
│   ├── table_schema_manager.go   TblToStructHandler
│   ├── schema_yml.go             yml 解析
│   ├── schema_type.go            类型归一化
│   ├── schema_diff.go            结构化比对
│   ├── schema_sort.go            字段顺序的最小移动集合算法
│   └── schema_sql.go             SQL 生成
├── bmap/                   任意 JSON 形态数据的惰性视图
├── dvap2/                  按外键关系批量组装嵌套数据（含 使用说明.md）
├── gslicer/                泛型切片处理
├── information_schema/     information_schema 的结构定义与序列化
├── cmd/
│   ├── test_schema_cases/        表结构同步的全 case 回归程序（30 个用例）
│   ├── test_yaml_to_sql/         yml 转 SQL 的手动验证
│   ├── test_alltabl_to_model/    全库生成模型
│   ├── test_tbl_to_model/        单表生成模型
│   └── 其余 test_*/              零散的手动验证程序，不属于对外能力
├── all_tbl_model/          模型生成的输出样例
└── example_test.go         根包的可编译案例
```

`private/` 是本地实验与探针程序，不属于对外 API。

## 实验性能力

下面这些还在实验中，接口可能调整，也可能在将来移除，生产环境请谨慎依赖：

| 包 | 大致用途 |
| --- | --- |
| [`dvap`](./dvap) | `dvap2` 的早期版本；`HasMany` / `HasOne` / `HasManyV2` / `HasOneV2` 已标注废弃，请改用 `NewDataer().HasMany` / `.HasOne` |
| [`gsave`](./gsave) | GORM 快速保存与更新映射 |

```go
// dvap 的 Dataer 用法
dataer := dvap.NewDataer()
// Do something
_ = dataer
```

## 约定

## License

[MIT](./LICENSE) © 2025 pulingfu
