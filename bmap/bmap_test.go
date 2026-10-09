package bmap

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// 本文件钉住 bmap 的行为契约。
//
// 每条断言都在回答同一个问题：这个行为是「设计如此」还是「算错了」。
// 已经有项目在用的规则属于前者，典型的是 Set 的 ## 前缀转义、
// Array() 把任意值提升成长度 1 的切片，动它们就是破坏兼容，
// 所以注释里会把每条规则存在的理由写清楚。
//
// 改这个包时先跑一遍本文件：红了就说明碰到了某条契约，
// 要么改回来，要么先确认这条规则确实可以动。

// ---------------------------------------------------------------------------
// Parse：接受哪些形态的输入
// ---------------------------------------------------------------------------

func TestParseInputShapes(t *testing.T) {
	t.Run("map 直接可用", func(t *testing.T) {
		bm := Parse(map[string]any{"a": 1})
		if got := bm.Get("a").Int(); got != 1 {
			t.Errorf("Get(a).Int() = %d, 期望 1", got)
		}
	})

	t.Run("JSON 文本会被解析成结构", func(t *testing.T) {
		bm := Parse(`{"a":{"b":[10,20]}}`)
		if got := bm.Get("a.b.1").Int(); got != 20 {
			t.Errorf("Get(a.b.1).Int() = %d, 期望 20", got)
		}
	})

	t.Run("JSON 字节流同样会被解析", func(t *testing.T) {
		bm := Parse([]byte(`{"a":"x"}`))
		if got := bm.Get("a").String(); got != "x" {
			t.Errorf("Get(a).String() = %q, 期望 x", got)
		}
	})

	t.Run("解析不出来的文本按普通字符串保留", func(t *testing.T) {
		// 契约：不 panic、不丢数据，IsExists 仍为 true，String 拿到原文。
		// 是不是 JSON 解析失败可以用 Error() 判断。
		bm := Parse(`{"a":`)
		if !bm.IsExists() {
			t.Error("非法 JSON 不该变成不存在的节点")
		}
		if got := bm.String(); got != `{"a":` {
			t.Errorf("String() = %q, 期望原文", got)
		}
		if bm.Err() == nil {
			t.Error("以 { 或 [ 开头却解析失败，Err() 应该能报出来")
		}
	})

	t.Run("普通字符串不是错误", func(t *testing.T) {
		bm := Parse("abc")
		if got := bm.String(); got != "abc" {
			t.Errorf("String() = %q, 期望 abc", got)
		}
		if bm.Err() != nil {
			t.Errorf("普通字符串不该报错，实际 %v", bm.Err())
		}
	})

	t.Run("nil 是不存在的节点", func(t *testing.T) {
		bm := Parse(nil)
		if bm.IsExists() {
			t.Error("Parse(nil).IsExists() 期望 false")
		}
		if got := bm.String(); got != "" {
			t.Errorf("String() = %q, 期望空串", got)
		}
		if !bm.IsNil() {
			t.Error("IsNil() 期望 true")
		}
	})

	t.Run("标量原样保留", func(t *testing.T) {
		if got := Parse(123).Int(); got != 123 {
			t.Errorf("Parse(123).Int() = %d", got)
		}
		if got := Parse(true).Bool(); got != true {
			t.Errorf("Parse(true).Bool() = %v", got)
		}
		if got := Parse(1.5).Float(); got != 1.5 {
			t.Errorf("Parse(1.5).Float() = %v", got)
		}
	})

	t.Run("传入 BMap 原样返回，不会套娃", func(t *testing.T) {
		// 契约：Parse 是可重入的，链式调用里重复 Parse 不会套娃。
		src := Parse(map[string]any{"a": 1})
		if got := Parse(src); got != src {
			t.Error("Parse(*BMap) 应该返回同一个指针")
		}
	})

	t.Run("指针一路解到底", func(t *testing.T) {
		m := map[string]any{"a": 1}
		if got := Parse(&m).Get("a").Int(); got != 1 {
			t.Errorf("Parse(*map) 取值失败，得到 %d", got)
		}
	})

	t.Run("命名字符串类型不该 panic", func(t *testing.T) {
		// 命名字符串类型（type MyJSON string）也要能解析，
		// 所以入口按 reflect 取字符串，不做 data.(string) 这种硬断言。
		type myJSON string
		if got := Parse(myJSON(`{"a":7}`)).Get("a").Int(); got != 7 {
			t.Errorf("命名字符串类型解析失败，得到 %d", got)
		}
	})
}

func TestParseTagName(t *testing.T) {
	type form struct {
		Name string `json:"name" form:"f_name"`
		Age  int    `json:"age" form:"f_age"`
	}

	t.Run("默认按 json 标签展开", func(t *testing.T) {
		bm := Parse(form{Name: "kk", Age: 9})
		if got := bm.TagName; got != "json" {
			t.Errorf("TagName = %q, 期望 json", got)
		}
		if got := bm.Get("name").String(); got != "kk" {
			t.Errorf("Get(name) = %q", got)
		}
	})

	t.Run("可以指定别的标签", func(t *testing.T) {
		bm := Parse(form{Name: "kk", Age: 9}, "form")
		if got := bm.Get("f_name").String(); got != "kk" {
			t.Errorf("Get(f_name) = %q", got)
		}
		// 换成 form 之后 json 的键名就不该再命中
		if bm.Get("name").IsExists() {
			t.Error("按 form 解析时不该还能用 json 的键名取到值")
		}
	})

	t.Run("TagName 要跟着子节点一起传递", func(t *testing.T) {
		// Get/Array/Foreach 产出的子节点都要带上 TagName，
		// 否则「按 form 解析出来的数组，元素再取 f_name」会取不到。
		bm := Parse([]form{{Name: "a"}, {Name: "b"}}, "form")

		if got := bm.Get("0").TagName; got != "form" {
			t.Errorf("Get(0).TagName = %q, 期望 form", got)
		}
		if got := bm.Array()[1].Get("f_name").String(); got != "b" {
			t.Errorf("Array()[1].Get(f_name) = %q, 期望 b", got)
		}

		var seen []string
		bm.Foreach(func(_ string, v *BMap) bool {
			seen = append(seen, v.Get("f_name").String())
			return true
		})
		if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
			t.Errorf("Foreach 里按 form 取名字失败，得到 %v", seen)
		}
	})
}

// ---------------------------------------------------------------------------
// Get：路径规则
// ---------------------------------------------------------------------------

func TestGetPath(t *testing.T) {
	src := map[string]any{
		"user": map[string]any{
			"name": "kk",
			"tags": []any{"a", "b"},
		},
		"0":    "数字键",
		"":     "空键",
		"a.b":  "带点键",
		"list": []any{map[string]any{"id": 1}, map[string]any{"id": 2}},
	}
	bm := Parse(src)

	t.Run("多层路径与数组下标", func(t *testing.T) {
		if got := bm.Get("user.name").String(); got != "kk" {
			t.Errorf("Get(user.name) = %q", got)
		}
		if got := bm.Get("user.tags.1").String(); got != "b" {
			t.Errorf("Get(user.tags.1) = %q", got)
		}
		if got := bm.Get("list.1.id").Int(); got != 2 {
			t.Errorf("Get(list.1.id) = %d", got)
		}
	})

	t.Run("map 上的数字键按下标以外的方式取", func(t *testing.T) {
		// 契约：数字段只在切片/数组上当下标，在 map 上就是普通键名。
		if got := bm.Get("0").String(); got != "数字键" {
			t.Errorf("Get(0) = %q", got)
		}
	})

	t.Run("空字符串也是一个合法的键", func(t *testing.T) {
		if got := bm.Get("").String(); got != "空键" {
			t.Errorf(`Get("") = %q`, got)
		}
	})

	t.Run("走不通就返回不存在的节点，绝不 panic", func(t *testing.T) {
		for _, key := range []string{
			"nope", "user.nope", "user.tags.9", "list.9.id",
			"user.name.deeper", "list.x", "0.1.2",
		} {
			got := bm.Get(key)
			if got.IsExists() {
				t.Errorf("Get(%q) 不该存在", key)
			}
			// 不存在的节点上继续取值，拿到的都是零值
			if s := got.String(); s != "" {
				t.Errorf("Get(%q).String() = %q, 期望空串", key, s)
			}
			if n := got.Int(); n != 0 {
				t.Errorf("Get(%q).Int() = %d, 期望 0", key, n)
			}
			if len(got.Array()) != 1 {
				t.Errorf("Get(%q).Array() 长度 = %d, 期望 1", key, len(got.Array()))
			}
		}
	})

	t.Run("键名里含点时用反斜杠转义", func(t *testing.T) {
		// 契约：不转义时 a.b 一定是「a 下面的 b」，这个行为不能变。
		if bm.Get("a.b").IsExists() {
			t.Error(`Get("a.b") 应该按路径解析，取不到字面键 a.b`)
		}
		// 键名里含点时用 \. 转义，与 gjson 一致
		if got := bm.Get(`a\.b`).String(); got != "带点键" {
			t.Errorf(`Get("a\\.b") = %q, 期望 带点键`, got)
		}
	})
}

// ---------------------------------------------------------------------------
// Set：写入规则
// ---------------------------------------------------------------------------

func TestSetPath(t *testing.T) {
	t.Run("自动创建中间层", func(t *testing.T) {
		bm := Parse(map[string]any{})
		bm.Set("a.b.c", 1)
		assertJSON(t, bm, `{"a":{"b":{"c":1}}}`)
	})

	t.Run("## 前缀让数字样式的键名不被当成下标", func(t *testing.T) {
		// 契约：已经有项目在依赖这条规则，键名会去掉 ## 后原样写入。
		bm := Parse(map[string]any{})
		bm.Set("##0", "v")
		assertJSON(t, bm, `{"0":"v"}`)
		if got := bm.Get("0").String(); got != "v" {
			t.Errorf("回读 Get(0) = %q, 期望 v", got)
		}
	})

	t.Run("数字段建数组，长度不够补 null", func(t *testing.T) {
		bm := Parse(map[string]any{"list": []any{"exist"}})
		bm.Set("list.2", "v")
		assertJSON(t, bm, `{"list":["exist",null,"v"]}`)
	})

	t.Run("中间层该建数组还是对象，看的是下一段", func(t *testing.T) {
		// 中间层建数组还是建对象，看的是「再下一段」是不是数字。
		// 看当前段没有意义：能走到建中间层这一步，就说明当前段不是数字。
		// 建错容器会让它被「标量提升」当成第 0 个元素，产出 {"list":[{},null,"v"]}。
		bm := Parse(map[string]any{})
		bm.Set("list.2", "v")
		assertJSON(t, bm, `{"list":[null,null,"v"]}`)

		bm2 := Parse(map[string]any{})
		bm2.Set("a.0.b", 1)
		assertJSON(t, bm2, `{"a":[{"b":1}]}`)
	})

	t.Run("标量提升成数组时，原值占第 0 位", func(t *testing.T) {
		// 契约：把已有内容当成数组的第一个元素，再往后写。
		bm := Parse("abc")
		bm.Set("1", "v")
		assertJSON(t, bm, `["abc","v"]`)

		bm2 := Parse(map[string]any{"k": 1})
		bm2.Set("2", "v")
		assertJSON(t, bm2, `[{"k":1},null,"v"]`)
	})

	t.Run("空容器提升成数组时不该塞进一个空对象", func(t *testing.T) {
		// 空容器不占第 0 位，否则结果里会多出一个凭空的 {}。
		bm := Parse(map[string]any{})
		bm.Set("2", "v")
		assertJSON(t, bm, `[null,null,"v"]`)
	})

	t.Run("覆盖已有值", func(t *testing.T) {
		bm := Parse(map[string]any{"a": 1})
		bm.Set("a", 2)
		assertJSON(t, bm, `{"a":2}`)
	})

	t.Run("写 nil 会落成一个 JSON null", func(t *testing.T) {
		bm := Parse(map[string]any{})
		bm.Set("a", nil)
		assertJSON(t, bm, `{"a":null}`)
		if !bm.Get("a").IsNil() {
			t.Error("写入的 nil 应该能读成 IsNil")
		}
	})

	t.Run("Set 返回自身以便链式调用", func(t *testing.T) {
		bm := Parse(map[string]any{})
		if got := bm.Set("a", 1); got != bm {
			t.Error("Set 应该返回同一个 *BMap")
		}
	})

	t.Run("在结构体上写入会先展开成 map", func(t *testing.T) {
		type item struct {
			Name string `json:"name"`
		}
		bm := Parse(item{Name: "kk"})
		bm.Set("age", 9)
		assertJSON(t, bm, `{"age":9,"name":"kk"}`)
	})
}

// ---------------------------------------------------------------------------
// Array：把任意值当数组看
// ---------------------------------------------------------------------------

func TestArraySemantics(t *testing.T) {
	// 契约（设计意图，不要改）：Array() 的语义是「把内容转成数组，然后看有几个」。
	//
	// 所以它和 gjson 的 Array() 不一样，不是「只有 JSON 数组才展开」：
	// 任何不是数组的值都会被当成长度 1 的数组，元素就是它自己。
	// 这样调用方可以统一写成 for _, item := range bm.Get(x).Array()，
	// 不用先判断对面到底是单个对象还是一批对象——
	// 接口返回「一条记录」和「一批记录」用同一段代码处理，正是这个语义的价值。
	//
	// 代价是标量上也会跑一次循环。要区分「不是数组」请先用 IsArray()。

	t.Run("数组逐元素展开", func(t *testing.T) {
		bm := Parse([]any{1, 2, 3})
		arr := bm.Array()
		if len(arr) != 3 {
			t.Fatalf("长度 = %d, 期望 3", len(arr))
		}
		for i, want := range []int{1, 2, 3} {
			if got := arr[i].Int(); got != want {
				t.Errorf("第 %d 个 = %d, 期望 %d", i, got, want)
			}
		}
	})

	t.Run("标量提升成长度 1 的数组", func(t *testing.T) {
		for _, src := range []any{"abc", 1, true, map[string]any{"a": 1}} {
			arr := Parse(src).Array()
			if len(arr) != 1 {
				t.Errorf("%T 的 Array() 长度 = %d, 期望 1", src, len(arr))
				continue
			}
			if !arr[0].IsExists() {
				t.Errorf("%T 提升出来的元素不该是不存在的节点", src)
			}
		}
	})

	t.Run("不存在的节点也返回长度 1，元素不存在", func(t *testing.T) {
		arr := Parse(nil).Array()
		if len(arr) != 1 {
			t.Fatalf("长度 = %d, 期望 1", len(arr))
		}
		if arr[0].IsExists() {
			t.Error("元素应该是不存在的节点")
		}
		if got := arr[0].String(); got != "" {
			t.Errorf("元素的 String() = %q, 期望空串", got)
		}
	})

	t.Run("IsArray 用来区分「真数组」和「被提升的标量」", func(t *testing.T) {
		if !Parse([]any{1}).IsArray() {
			t.Error("数组的 IsArray 期望 true")
		}
		if Parse("abc").IsArray() {
			t.Error("标量的 IsArray 期望 false")
		}
		if Parse(nil).IsArray() {
			t.Error("nil 的 IsArray 期望 false")
		}
	})

	t.Run("数组里的 JSON 文本元素仍会被解析", func(t *testing.T) {
		// 契约：元素走的是 Parse，所以字符串形态的 JSON 会展开成结构。
		bm := Parse([]any{`{"a":1}`})
		if got := bm.Array()[0].Get("a").Int(); got != 1 {
			t.Errorf("元素里的 JSON 没被解析，Get(a) = %d", got)
		}
	})

	t.Run("元素是空安全的", func(t *testing.T) {
		bm := Parse([]any{map[string]any{"a": 1}, nil, "x"})
		arr := bm.Array()
		if len(arr) != 3 {
			t.Fatalf("长度 = %d, 期望 3", len(arr))
		}
		if got := arr[1].Get("a").Int(); got != 0 {
			t.Errorf("nil 元素上取值应该是零值，得到 %d", got)
		}
	})
}

// ---------------------------------------------------------------------------
// 取值器
// ---------------------------------------------------------------------------

func TestValueGetters(t *testing.T) {
	t.Run("String 的各类型口径", func(t *testing.T) {
		cases := []struct {
			src  any
			want string
		}{
			{"abc", "abc"},
			{123, "123"},
			{int64(-5), "-5"},
			{uint8(7), "7"},
			{1.5, "1.5"},
			{true, "true"},
			{false, "false"},
			{nil, ""},
			{[]byte(`{"a":1}`), `{"a":1}`},
			{[]any{1, 2}, "[1,2]"},
			{map[string]any{"a": 1}, `{"a":1}`},
		}
		for _, c := range cases {
			if got := Parse(c.src).String(); got != c.want {
				t.Errorf("Parse(%#v).String() = %q, 期望 %q", c.src, got, c.want)
			}
		}
	})

	t.Run("String 对 time.Time 输出 RFC3339", func(t *testing.T) {
		tm := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
		if got := Parse(tm).String(); got != tm.Format(time.RFC3339) {
			t.Errorf("String() = %q", got)
		}
	})

	t.Run("Int 系列", func(t *testing.T) {
		cases := []struct {
			src  any
			want int64
		}{
			{1, 1},
			{int64(2), 2},
			{uint32(3), 3},
			{"4", 4},
			{5.9, 5},   // 截断，与 gjson 一致
			{-5.9, -5}, // 朝零截断
			{true, 1},  // 布尔按 1/0 算，与 gjson 一致
			{false, 0},
			{nil, 0},
			{"12abc", 0}, // 解析不出来就是 0，不报错
			{map[string]any{"a": 1}, 0},
		}
		for _, c := range cases {
			if got := Parse(c.src).Int64(); got != c.want {
				t.Errorf("Parse(%#v).Int64() = %d, 期望 %d", c.src, got, c.want)
			}
			if got := Parse(c.src).Int(); int64(got) != c.want {
				t.Errorf("Parse(%#v).Int() = %d, 期望 %d", c.src, got, c.want)
			}
		}
		if got := Parse(9).Int32(); got != 9 {
			t.Errorf("Int32() = %d", got)
		}
	})

	t.Run("Float 系列", func(t *testing.T) {
		cases := []struct {
			src  any
			want float64
		}{
			{1.5, 1.5},
			{"2.25", 2.25},
			{3, 3},
			{uint8(4), 4},
			{true, 1}, // 布尔按 1/0 算
			{nil, 0},
			{"abc", 0},
		}
		for _, c := range cases {
			if got := Parse(c.src).Float(); got != c.want {
				t.Errorf("Parse(%#v).Float() = %v, 期望 %v", c.src, got, c.want)
			}
		}
		if got := Parse(1.5).Float32(); got != 1.5 {
			t.Errorf("Float32() = %v", got)
		}
		if got := Parse(1.5).Float64(); got != 1.5 {
			t.Errorf("Float64() = %v", got)
		}
	})

	t.Run("Uint", func(t *testing.T) {
		// 无符号取值，口径与 gjson 一致：负数按 0 处理
		cases := []struct {
			src  any
			want uint64
		}{
			{uint64(18446744073709551615), 18446744073709551615},
			{7, 7},
			{"8", 8},
			{-1, 0},
			{nil, 0},
		}
		for _, c := range cases {
			if got := Parse(c.src).Uint(); got != c.want {
				t.Errorf("Parse(%#v).Uint() = %d, 期望 %d", c.src, got, c.want)
			}
		}
	})

	t.Run("Bool 先按 strconv 的口径，认不出来再按 gjson 兜底", func(t *testing.T) {
		cases := []struct {
			src  any
			want bool
		}{
			// strconv.ParseBool 认得的，结果一律不变（契约）
			{true, true},
			{false, false},
			{"true", true},
			{"TRUE", true},
			{"True", true},
			{"t", true},
			{"1", true},
			{"false", false},
			{"F", false},
			{"0", false},
			// 认不出来的按 gjson 的口径兜底（新增）
			{1, true},
			{0, false},
			{0.5, true},   // gjson：数字非零即真
			{-1, true},    //
			{"yes", true}, // gjson：非空且不是 "0" 的字符串为真
			{"", false},
			{nil, false},
			{[]any{1}, true},         // gjson：非 null 的 JSON 为真
			{[]any{}, true},          //
			{map[string]any{}, true}, //
		}
		for _, c := range cases {
			if got := Parse(c.src).Bool(); got != c.want {
				t.Errorf("Parse(%#v).Bool() = %v, 期望 %v", c.src, got, c.want)
			}
		}
	})

	t.Run("Value 与 Map", func(t *testing.T) {
		m := map[string]any{"a": 1}
		bm := Parse(m)
		if got, ok := bm.Value().(map[string]any); !ok || got["a"] != 1 {
			t.Errorf("Value() = %#v", bm.Value())
		}
		if got := bm.Map()["a"]; got != 1 {
			t.Errorf("Map()[a] = %#v", got)
		}
		// 契约：不是对象时 Map() 给一个空 map，不是 nil，调用方可以直接 range
		if got := Parse("abc").Map(); got == nil || len(got) != 0 {
			t.Errorf("标量的 Map() = %#v, 期望空 map", got)
		}
	})

	t.Run("Value 不该改写节点自身", func(t *testing.T) {
		// 取值器不许改写节点自身：解引用要在局部变量上做，
		// 否则每调一次就把节点剥掉一层，同一个节点反复取值的结果会漂移。
		n := 7
		bm := Parse(&n)
		for i := 0; i < 3; i++ {
			if got := bm.Value(); got != 7 {
				t.Fatalf("第 %d 次 Value() = %#v, 期望 7", i, got)
			}
		}
		if got := bm.Int(); got != 7 {
			t.Errorf("Int() = %d, 期望 7", got)
		}
	})

	t.Run("IsObject / IsArray / IsNil / IsExists 互不干扰", func(t *testing.T) {
		obj := Parse(map[string]any{"a": 1})
		if !obj.IsObject() || obj.IsArray() || obj.IsNil() || !obj.IsExists() {
			t.Error("对象的判定不对")
		}
		arr := Parse([]any{1})
		if arr.IsObject() || !arr.IsArray() || arr.IsNil() || !arr.IsExists() {
			t.Error("数组的判定不对")
		}
		// 反复调用结果稳定（判定函数不许有副作用）
		for i := 0; i < 3; i++ {
			if !obj.IsObject() || !arr.IsArray() {
				t.Fatalf("第 %d 次判定结果变了", i)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Time
// ---------------------------------------------------------------------------

func TestTime(t *testing.T) {
	t.Run("常见布局都能认", func(t *testing.T) {
		cases := []struct {
			src  string
			want string // 用同一个布局格式化回去，避免时区/年份干扰断言
		}{
			{"2026-10-09 12:30:45", "2026-10-09 12:30:45"},
			{"2026-10-09", "2026-10-09"},
			{"2026-10-09T12:30:45+08:00", "2026-10-09 12:30:45"},
			{"12:30:45", "12:30:45"},
			{"3:04PM", "3:04PM"},
			{"Oct 9 12:30:45", "Oct 9 12:30:45"},
		}
		for _, c := range cases {
			got := Parse(c.src).Time()
			if got.IsZero() {
				t.Errorf("Parse(%q).Time() 解析失败", c.src)
			}
		}
	})

	t.Run("纯时间不该被后面的布局覆盖掉", func(t *testing.T) {
		// 布局链必须「第一个成功就返回」：
		// 中途某个布局已经解析成功却继续往下试，后面的失败会把结果覆盖成零值。
		got := Parse("12:30:45").Time()
		if got.IsZero() {
			t.Fatal(`Parse("12:30:45").Time() 得到零值，说明成功结果被覆盖了`)
		}
		if s := got.Format(time.TimeOnly); s != "12:30:45" {
			t.Errorf("格式化回去 = %q, 期望 12:30:45", s)
		}
	})

	t.Run("认不出来就是零值", func(t *testing.T) {
		for _, src := range []string{"", "abc", "12:30", "2026/13/45"} {
			if got := Parse(src).Time(); !got.IsZero() {
				t.Errorf("Parse(%q).Time() = %v, 期望零值", src, got)
			}
		}
	})

	t.Run("TimeE 能区分「解析失败」和「本来就是零值」", func(t *testing.T) {
		if _, err := Parse("abc").TimeE(); err == nil {
			t.Error("解析失败应该返回 error")
		}
		if got, err := Parse("2026-10-09 12:30:45").TimeE(); err != nil || got.IsZero() {
			t.Errorf("TimeE() = %v, %v", got, err)
		}
	})

	t.Run("TimeLayout 用指定布局解析", func(t *testing.T) {
		got := Parse("09/10/2026").TimeLayout("02/01/2006")
		if got.Year() != 2026 || got.Month() != 10 || got.Day() != 9 {
			t.Errorf("TimeLayout 解析结果 = %v", got)
		}
		if !Parse("abc").TimeLayout(time.DateTime).IsZero() {
			t.Error("布局不匹配时应该是零值")
		}
	})

	t.Run("带引号的时间文本也能解析", func(t *testing.T) {
		// 契约：数据库里的 JSON 片段常带着引号，解析前先摘掉
		got := Parse(`"2026-10-09 12:30:45"`).Time()
		if got.IsZero() {
			t.Error("带引号的时间没解析出来")
		}
	})

	t.Run("time.Time 类型的值直接可用", func(t *testing.T) {
		tm := time.Date(2026, 10, 9, 12, 30, 45, 0, time.Local)
		if got := Parse(tm).Time(); !got.Equal(tm) {
			t.Errorf("Time() = %v, 期望 %v", got, tm)
		}
	})

	t.Run("UnixTime 把整数当秒级时间戳", func(t *testing.T) {
		// 数据库里的 int 列常存着时间戳，单独给一个方法，
		// 不去改 Time() 的口径（Time() 在数字上仍然返回零值）。
		if got := Parse(1760000000).UnixTime().Unix(); got != 1760000000 {
			t.Errorf("UnixTime() = %d", got)
		}
		if got := Parse("1760000000").UnixTime().Unix(); got != 1760000000 {
			t.Errorf("字符串时间戳 UnixTime() = %d", got)
		}
		if got := Parse(1760000000).Time(); !got.IsZero() {
			t.Errorf("Time() 在数字上应该是零值，得到 %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Foreach
// ---------------------------------------------------------------------------

func TestForeach(t *testing.T) {
	t.Run("对象按键遍历", func(t *testing.T) {
		bm := Parse(map[string]any{"a": 1, "b": 2})
		sum := 0
		bm.Foreach(func(k string, v *BMap) bool {
			sum += v.Int()
			return true
		})
		if sum != 3 {
			t.Errorf("累加结果 = %d, 期望 3", sum)
		}
	})

	t.Run("数组按下标遍历，键是下标的字符串", func(t *testing.T) {
		bm := Parse([]any{"x", "y"})
		var keys, vals []string
		bm.Foreach(func(k string, v *BMap) bool {
			keys = append(keys, k)
			vals = append(vals, v.String())
			return true
		})
		if keys[0] != "0" || keys[1] != "1" {
			t.Errorf("键 = %v, 期望 [0 1]", keys)
		}
		if vals[0] != "x" || vals[1] != "y" {
			t.Errorf("值 = %v", vals)
		}
	})

	t.Run("回调返回 false 立即中断", func(t *testing.T) {
		bm := Parse([]any{1, 2, 3, 4})
		n := 0
		bm.Foreach(func(_ string, _ *BMap) bool {
			n++
			return n < 2
		})
		if n != 2 {
			t.Errorf("回调次数 = %d, 期望 2", n)
		}
	})

	t.Run("键名里含点也能遍历到", func(t *testing.T) {
		// 回调里的值直接从容器取，不能拿键名再走一遍 Get：
		// 含点的键名会被当成路径，取到的是空值。
		bm := Parse(map[string]any{"a.b": "带点", "x": "普通"})
		got := map[string]string{}
		bm.Foreach(func(k string, v *BMap) bool {
			got[k] = v.String()
			return true
		})
		if got["a.b"] != "带点" {
			t.Errorf("含点键名的值 = %q, 期望 带点", got["a.b"])
		}
		if got["x"] != "普通" {
			t.Errorf("普通键的值 = %q", got["x"])
		}
	})

	t.Run("结构体也能遍历", func(t *testing.T) {
		type item struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		bm := Parse(item{Name: "kk", Age: 9})
		got := map[string]string{}
		bm.Foreach(func(k string, v *BMap) bool {
			got[k] = v.String()
			return true
		})
		if got["name"] != "kk" || got["age"] != "9" {
			t.Errorf("遍历结构体得到 %v", got)
		}
	})

	t.Run("指针容器也能遍历", func(t *testing.T) {
		// 指针容器要先解引用，否则 Kind() 是 Ptr，落不进任何分支，回调一次都不会执行。
		m := map[string]any{"a": 1}
		n := 0
		Parse(&m).Foreach(func(_ string, _ *BMap) bool {
			n++
			return true
		})
		if n != 1 {
			t.Errorf("*map 上的回调次数 = %d, 期望 1", n)
		}
	})

	t.Run("标量与不存在的节点不触发回调", func(t *testing.T) {
		for _, src := range []any{"abc", 1, nil, true} {
			n := 0
			Parse(src).Foreach(func(_ string, _ *BMap) bool {
				n++
				return true
			})
			if n != 0 {
				t.Errorf("Parse(%#v) 上的回调次数 = %d, 期望 0", src, n)
			}
		}
	})

	t.Run("SortedForeach 按键排序，输出稳定", func(t *testing.T) {
		// map 的遍历顺序每次都不一样，要稳定输出（拼日志、生成语句、做快照对比）用它
		bm := Parse(map[string]any{"c": 3, "a": 1, "b": 2})
		var keys []string
		bm.SortedForeach(func(k string, _ *BMap) bool {
			keys = append(keys, k)
			return true
		})
		if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
			t.Errorf("排序遍历的键 = %v, 期望 [a b c]", keys)
		}

		n := 0
		bm.SortedForeach(func(_ string, _ *BMap) bool {
			n++
			return n < 2
		})
		if n != 2 {
			t.Errorf("SortedForeach 返回 false 应该中断，实际跑了 %d 次", n)
		}
	})
}

// ---------------------------------------------------------------------------
// 类型判定与新增的基础方法
// ---------------------------------------------------------------------------

func TestTypeAndBasicHelpers(t *testing.T) {
	t.Run("Type 给出底层形态", func(t *testing.T) {
		cases := []struct {
			src  any
			want ValueType
		}{
			{nil, TypeNull},
			{"abc", TypeString},
			{[]byte(`{"a":1}`), TypeObject},
			{1, TypeNumber},
			{1.5, TypeNumber},
			{json.Number("3"), TypeNumber},
			{true, TypeBool},
			{map[string]any{}, TypeObject},
			{[]any{}, TypeArray},
		}
		for _, c := range cases {
			if got := Parse(c.src).Type(); got != c.want {
				t.Errorf("Parse(%#v).Type() = %v, 期望 %v", c.src, got, c.want)
			}
		}
		// 走不通的路径也是 TypeNull（口径与 gjson 一致），
		// 要区分「路径不存在」和「值就是 null」用 IsExists()
		if got := Parse(map[string]any{}).Get("nope").Type(); got != TypeNull {
			t.Errorf("不存在的节点 Type() = %v, 期望 TypeNull", got)
		}
		if Parse(map[string]any{}).Get("nope").IsExists() {
			t.Error("走不通的路径 IsExists() 应该为 false")
		}
		// 契约：解析不出来的文本仍然是「存在的字符串节点」
		if got := Parse("nope").Type(); got != TypeString {
			t.Errorf("Parse(\"nope\").Type() = %v, 期望 TypeString", got)
		}
	})

	t.Run("Is 系列判定", func(t *testing.T) {
		if !Parse("x").IsString() {
			t.Error("IsString")
		}
		if !Parse(1).IsNumber() || !Parse(1.5).IsNumber() {
			t.Error("IsNumber")
		}
		if !Parse(true).IsBool() {
			t.Error("IsBool")
		}
		if Parse("x").IsNumber() || Parse(1).IsString() || Parse(1).IsBool() {
			t.Error("不同类型之间不该误判")
		}
	})

	t.Run("Len 给出元素个数", func(t *testing.T) {
		// 数组给长度、对象给键数、其它给 0。
		// 注意它和 Array() 不是一回事：标量的 Array() 长度是 1，Len() 是 0。
		cases := []struct {
			src  any
			want int
		}{
			{[]any{1, 2, 3}, 3},
			{[]any{}, 0},
			{map[string]any{"a": 1, "b": 2}, 2},
			{"abc", 0},
			{1, 0},
			{nil, 0},
		}
		for _, c := range cases {
			if got := Parse(c.src).Len(); got != c.want {
				t.Errorf("Parse(%#v).Len() = %d, 期望 %d", c.src, got, c.want)
			}
		}
	})

	t.Run("Keys 给出对象的键", func(t *testing.T) {
		keys := Parse(map[string]any{"b": 2, "a": 1}).Keys()
		if len(keys) != 2 {
			t.Fatalf("Keys() = %v", keys)
		}
		// 排好序返回，调用方不用再 sort
		if keys[0] != "a" || keys[1] != "b" {
			t.Errorf("Keys() = %v, 期望已排序", keys)
		}
		if got := Parse([]any{1, 2}).Keys(); len(got) != 2 || got[0] != "0" {
			t.Errorf("数组的 Keys() = %v, 期望下标字符串", got)
		}
		if got := Parse("abc").Keys(); len(got) != 0 {
			t.Errorf("标量的 Keys() = %v, 期望空", got)
		}
	})

	t.Run("At 按下标取，不做提升", func(t *testing.T) {
		// 与 Array()[i] 的区别是它不做提升：越界给不存在的节点
		bm := Parse([]any{"x", "y"})
		if got := bm.At(1).String(); got != "y" {
			t.Errorf("At(1) = %q", got)
		}
		if bm.At(9).IsExists() {
			t.Error("越界应该是不存在的节点")
		}
		if bm.At(-1).IsExists() {
			t.Error("负下标应该是不存在的节点")
		}
		if Parse("abc").At(0).IsExists() {
			t.Error("标量上 At 不该提升成数组")
		}
	})

	t.Run("Delete 删掉一个键或一个下标", func(t *testing.T) {
		bm := Parse(map[string]any{"a": 1, "b": 2})
		bm.Delete("a")
		assertJSON(t, bm, `{"b":2}`)

		bm2 := Parse([]any{1, 2, 3})
		bm2.Delete("1")
		assertJSON(t, bm2, `[1,3]`)

		// 删不存在的键不报错
		bm.Delete("nope")
		assertJSON(t, bm, `{"b":2}`)
	})

	t.Run("带点的键名用转义删除", func(t *testing.T) {
		bm := Parse(map[string]any{"a.b": 1, "c": 2})
		bm.Delete(`a\.b`)
		assertJSON(t, bm, `{"c":2}`)
	})

	t.Run("Or 系列给默认值", func(t *testing.T) {
		// 路径不存在时用调用方给的默认值，省掉一层 if
		bm := Parse(map[string]any{"a": 1})
		if got := bm.Get("a").IntOr(99); got != 1 {
			t.Errorf("IntOr = %d", got)
		}
		if got := bm.Get("nope").IntOr(99); got != 99 {
			t.Errorf("不存在时 IntOr = %d, 期望 99", got)
		}
		if got := bm.Get("nope").StringOr("d"); got != "d" {
			t.Errorf("不存在时 StringOr = %q", got)
		}
		if got := bm.Get("a").StringOr("d"); got != "1" {
			t.Errorf("存在时 StringOr = %q", got)
		}
	})

	t.Run("Raw 给出 JSON 原文", func(t *testing.T) {
		if got := Parse(map[string]any{"a": 1}).Raw(); got != `{"a":1}` {
			t.Errorf("Raw() = %q", got)
		}
		if got := Parse(nil).Raw(); got != "null" {
			t.Errorf("nil 的 Raw() = %q, 期望 null", got)
		}
		if got := Parse("abc").Raw(); got != `"abc"` {
			t.Errorf("字符串的 Raw() = %q, 期望带引号", got)
		}
	})
}

// ---------------------------------------------------------------------------
// MarshalJSON
// ---------------------------------------------------------------------------

func TestMarshalJSON(t *testing.T) {
	t.Run("各种形态都能被 json.Marshal", func(t *testing.T) {
		cases := []struct {
			src  any
			want string
		}{
			{map[string]any{"a": 1}, `{"a":1}`},
			{[]any{1, 2}, `[1,2]`},
			{"abc", `"abc"`}, // 字符串要带引号，否则不是合法 JSON
			{123, `123`},
			{1.5, `1.5`},
			{true, `true`},
			{nil, `null`}, // 空字节会让 json.Marshal 直接报错
		}
		for _, c := range cases {
			b, err := json.Marshal(Parse(c.src))
			if err != nil {
				t.Errorf("json.Marshal(Parse(%#v)) 报错 %v", c.src, err)
				continue
			}
			if string(b) != c.want {
				t.Errorf("json.Marshal(Parse(%#v)) = %s, 期望 %s", c.src, b, c.want)
			}
		}
	})

	t.Run("取出来的子节点也能直接 Marshal", func(t *testing.T) {
		bm := Parse(`{"user":{"name":"kk","age":9}}`)
		b, err := json.Marshal(bm.Get("user"))
		if err != nil {
			t.Fatalf("报错 %v", err)
		}
		if string(b) != `{"age":9,"name":"kk"}` {
			t.Errorf("结果 = %s", b)
		}
	})

	t.Run("UnmarshalJSON 让 BMap 能被反序列化填充", func(t *testing.T) {
		var bm BMap
		if err := json.Unmarshal([]byte(`{"a":[1,2]}`), &bm); err != nil {
			t.Fatalf("Unmarshal 报错 %v", err)
		}
		if got := bm.Get("a.1").Int(); got != 2 {
			t.Errorf("Get(a.1) = %d, 期望 2", got)
		}
	})

	t.Run("time.Time 输出成带引号的 RFC3339", func(t *testing.T) {
		tm := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
		b, err := json.Marshal(Parse(tm))
		if err != nil {
			t.Fatalf("报错 %v", err)
		}
		if string(b) != strconv.Quote(tm.Format(time.RFC3339)) {
			t.Errorf("结果 = %s", b)
		}
	})
}

// assertJSON 断言节点的 JSON 文本。键顺序由 json.Marshal 决定，是稳定的。
func assertJSON(t *testing.T, bm *BMap, want string) {
	t.Helper()
	if got := bm.String(); got != want {
		t.Errorf("JSON = %s, 期望 %s", got, want)
	}
}
