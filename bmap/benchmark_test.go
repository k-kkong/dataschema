package bmap

import (
	"strconv"
	"testing"
)

// 包内基准。
//
// 这里刻意只用对外 API，不碰任何内部函数，
// 这样同一个文件可以在改动前后的两个版本上分别跑，直接对比数字。
//
//	go test -run XXX -bench . -benchmem ./bmap/

// ---------------------------------------------------------------------------
// 基准用的样本
// ---------------------------------------------------------------------------

// benchSmall 字段很少的结构体
type benchSmall struct {
	A string `json:"a"`
	B int    `json:"b"`
}

// benchBig 字段很多的结构体，用来看开销是否随字段数增长
type benchBig struct {
	F00 string `json:"f00"`
	F01 string `json:"f01"`
	F02 string `json:"f02"`
	F03 string `json:"f03"`
	F04 string `json:"f04"`
	F05 string `json:"f05"`
	F06 string `json:"f06"`
	F07 string `json:"f07"`
	F08 string `json:"f08"`
	F09 string `json:"f09"`
	F10 string `json:"f10"`
	F11 string `json:"f11"`
	F12 string `json:"f12"`
	F13 string `json:"f13"`
	F14 string `json:"f14"`
	F15 string `json:"f15"`
	F16 string `json:"f16"`
	F17 string `json:"f17"`
	F18 string `json:"f18"`
	F19 string `json:"f19"`
	F20 string `json:"f20"`
	F21 string `json:"f21"`
	F22 string `json:"f22"`
	F23 string `json:"f23"`
	F24 string `json:"f24"`
	F25 string `json:"f25"`
	F26 string `json:"f26"`
	F27 string `json:"f27"`
	F28 string `json:"f28"`
	F29 string `json:"f29"`
	F30 string `json:"f30"`
	F31 string `json:"f31"`
}

type benchNested struct {
	Name string   `json:"name"`
	Age  int      `json:"age"`
	Sub  benchBig `json:"sub"`
}

// benchMap 造一个 n 个键的 map[string]any
func benchMap(n int) map[string]any {
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		m["key"+strconv.Itoa(i)] = i
	}
	return m
}

// benchDeep 造一条 5 层深的路径
func benchDeep() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"list": []any{
				map[string]any{
					"user": map[string]any{"name": "kk"},
				},
			},
		},
	}
}

// benchSlice 造一个 n 个元素的切片，元素是 map
func benchSlice(n int) []any {
	out := make([]any, n)
	for i := 0; i < n; i++ {
		out[i] = map[string]any{"id": i, "name": "n" + strconv.Itoa(i)}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parse
// ---------------------------------------------------------------------------

func BenchmarkParseMap16(b *testing.B)  { benchmarkParseMap(b, 16) }
func BenchmarkParseMap512(b *testing.B) { benchmarkParseMap(b, 512) }
func BenchmarkParseJSONText(b *testing.B) {
	src := `{"data":{"list":[{"id":1,"name":"kk"},{"id":2,"name":"bb"}]}}`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Parse(src)
	}
}

func benchmarkParseMap(b *testing.B, n int) {
	m := benchMap(n)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Parse(m)
	}
}

// 结构体展开：字段元信息缓存的收益就在这里
func BenchmarkParseStructSmall(b *testing.B) { benchmarkParseStruct(b, benchSmall{A: "x", B: 1}) }
func BenchmarkParseStructBig(b *testing.B)   { benchmarkParseStruct(b, benchBig{}) }

func benchmarkParseStruct(b *testing.B, v any) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Parse(v)
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func BenchmarkGetFlat(b *testing.B) {
	bm := Parse(benchMap(512))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get("key500").Int()
	}
}

func BenchmarkGetDeep(b *testing.B) {
	bm := Parse(benchDeep())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get("data.list.0.user.name").String()
	}
}

// 结构体嵌在 map 里：每走一段都要在结构体上定位字段，
// 是「单字段定位」与「整包拆成 map」差别最大的场景
func BenchmarkGetStructInMapFirst(b *testing.B) { benchmarkGetStructInMap(b, "f00") }
func BenchmarkGetStructInMapLast(b *testing.B)  { benchmarkGetStructInMap(b, "f31") }

func benchmarkGetStructInMap(b *testing.B, key string) {
	bm := Parse(map[string]any{"s": benchBig{}})
	path := "s." + key // 拼路径的开销不算在基准里
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get(path).String()
	}
}

func BenchmarkGetMissing(b *testing.B) {
	bm := Parse(benchMap(512))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get("nope.deep.path").String()
	}
}

// ---------------------------------------------------------------------------
// 取值器
// ---------------------------------------------------------------------------

func BenchmarkString(b *testing.B) {
	bm := Parse(map[string]any{"s": "hello world"})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get("s").String()
	}
}

func BenchmarkInt(b *testing.B) {
	bm := Parse(map[string]any{"n": "12345"})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get("n").Int()
	}
}

func BenchmarkBool(b *testing.B) {
	bm := Parse(map[string]any{"b": "true"})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Get("b").Bool()
	}
}

// 时间解析：命中越早越便宜，所以第一个布局与最后一个布局要分开测
func BenchmarkTimeFirstLayout(b *testing.B) { benchmarkTime(b, "2026-10-09 12:30:45") }
func BenchmarkTimeLastLayout(b *testing.B)  { benchmarkTime(b, "3:04PM") }

func benchmarkTime(b *testing.B, src string) {
	bm := Parse(src)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Time()
	}
}

// ---------------------------------------------------------------------------
// 遍历与批量
// ---------------------------------------------------------------------------

func BenchmarkArray32(b *testing.B)  { benchmarkArray(b, 32) }
func BenchmarkArray512(b *testing.B) { benchmarkArray(b, 512) }

func benchmarkArray(b *testing.B, n int) {
	bm := Parse(benchSlice(n))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, item := range bm.Array() {
			_ = item.Get("id").Int()
		}
	}
}

func BenchmarkForeach32(b *testing.B)  { benchmarkForeach(b, 32) }
func BenchmarkForeach512(b *testing.B) { benchmarkForeach(b, 512) }

func benchmarkForeach(b *testing.B, n int) {
	m := benchMap(n)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Parse(m).Foreach(func(_ string, _ *BMap) bool { return true })
	}
}

// 对照：裸 range 同一个 map 的成本，用来估 Foreach 的额外开销
func BenchmarkRawRangeMap512(b *testing.B) {
	m := benchMap(512)
	sink := 0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for k, v := range m {
			if len(k) > 0 {
				sink += v.(int)
			}
		}
	}
	_ = sink
}

// ---------------------------------------------------------------------------
// 写与回填
// ---------------------------------------------------------------------------

func BenchmarkSetFlat(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bm := Parse(map[string]any{})
		bm.Set("a", 1)
	}
}

func BenchmarkSetDeep(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bm := Parse(map[string]any{})
		bm.Set("a.b.c.d", 1)
	}
}

func BenchmarkFillStruct(b *testing.B) {
	bm := Parse(map[string]any{
		"name": "kk", "age": 9,
		"sub": map[string]any{"f00": "a", "f31": "b"},
	})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var dst benchNested
		bm.Fill(&dst)
	}
}

func BenchmarkMapAccessor(b *testing.B) {
	bm := Parse(benchMap(512))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bm.Map()
	}
}
