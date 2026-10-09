package bmap

import (
	"encoding/json"
	"testing"
	"time"
)

// 本文件钉住「结构体按 tag 展开成 map」的规则。
//
// 这一层是 Parse 的地基：结构体一进来就被展开，之后所有取值都在 map 上进行。
// 展开规则与 encoding/json 对齐，任何一处口径变了，
// 已经上线的调用方按名字取值就会突然取不到，所以这里的断言要逐条覆盖。

type EmbBase struct {
	E string `json:"e"`
}

type EmbTagged struct {
	Inner string `json:"inner"`
}

type SampleStruct struct {
	EmbBase        // 匿名嵌入且没有自己的 tag：平铺到上一层
	Name    string `json:"name"` // 有 tag：用 tag 名
	NoTag   string // 没有 tag：用字段名
	Skip    string `json:"-"` // tag 是 "-"：跳过
	Omit    string `json:"omit,omitempty"`
	Ptr     *int   `json:"ptr"`
	Sub     struct {
		X int `json:"x"`
	} `json:"sub"`
	List   []int          `json:"list"`
	M      map[string]int `json:"m"`
	Tag    EmbTagged      `json:"tag"` // 匿名类型但写了 tag：当成一个字段，不平铺
	hidden string         //nolint:unused // 未导出字段，必须被跳过
}

func newSample() SampleStruct {
	n := 7
	return SampleStruct{
		EmbBase: EmbBase{E: "emb"},
		Name:    "kk",
		NoTag:   "nt",
		Skip:    "should-not-appear",
		Omit:    "",
		Ptr:     &n,
		List:    []int{1, 2},
		M:       map[string]int{"a": 1},
		Tag:     EmbTagged{Inner: "in"},
		hidden:  "hidden",
	}
}

func TestStructUnpackRules(t *testing.T) {
	bm := Parse(newSample())

	t.Run("写了 tag 用 tag 名", func(t *testing.T) {
		if got := bm.Get("name").String(); got != "kk" {
			t.Errorf("Get(name) = %q", got)
		}
		if bm.Get("Name").IsExists() {
			t.Error("写了 tag 之后不该还能用字段名取到")
		}
	})

	t.Run("没写 tag 用字段名", func(t *testing.T) {
		if got := bm.Get("NoTag").String(); got != "nt" {
			t.Errorf("Get(NoTag) = %q", got)
		}
	})

	t.Run(`tag 是 "-" 的字段跳过`, func(t *testing.T) {
		if bm.Get("Skip").IsExists() {
			t.Error(`tag 为 "-" 的字段不该出现`)
		}
		if bm.Get("-").IsExists() {
			t.Error(`tag 为 "-" 的字段不该出现`)
		}
	})

	t.Run("omitempty 的零值跳过", func(t *testing.T) {
		if bm.Get("omit").IsExists() {
			t.Error("omitempty 且是零值，应该被跳过")
		}
		// 非零值时照常出现
		s := newSample()
		s.Omit = "here"
		if got := Parse(s).Get("omit").String(); got != "here" {
			t.Errorf("Get(omit) = %q", got)
		}
	})

	t.Run("未导出字段跳过", func(t *testing.T) {
		if bm.Get("hidden").IsExists() {
			t.Error("未导出字段不该出现")
		}
	})

	t.Run("匿名嵌入且没有 tag 时平铺", func(t *testing.T) {
		// 契约：e 直接出现在上一层，而不是 EmbBase.e
		if got := bm.Get("e").String(); got != "emb" {
			t.Errorf("Get(e) = %q", got)
		}
		if bm.Get("EmbBase").IsExists() {
			t.Error("平铺之后不该还有嵌入字段名这一层")
		}
	})

	t.Run("匿名类型写了 tag 就不平铺", func(t *testing.T) {
		if got := bm.Get("tag.inner").String(); got != "in" {
			t.Errorf("Get(tag.inner) = %q", got)
		}
		if bm.Get("inner").IsExists() {
			t.Error("写了 tag 的字段不该被平铺")
		}
	})

	t.Run("嵌套结构体保持原样，取值时再展开", func(t *testing.T) {
		if got := bm.Get("sub.x").Int(); got != 0 {
			t.Errorf("Get(sub.x) = %d", got)
		}
		s := newSample()
		s.Sub.X = 3
		if got := Parse(s).Get("sub.x").Int(); got != 3 {
			t.Errorf("Get(sub.x) = %d, 期望 3", got)
		}
	})

	t.Run("指针字段解引用后取值", func(t *testing.T) {
		if got := bm.Get("ptr").Int(); got != 7 {
			t.Errorf("Get(ptr) = %d", got)
		}
		// nil 指针取出来是不存在的节点，不是 panic
		s := newSample()
		s.Ptr = nil
		if got := Parse(s).Get("ptr").Int(); got != 0 {
			t.Errorf("nil 指针的 Get(ptr).Int() = %d", got)
		}
	})

	t.Run("切片与 map 字段", func(t *testing.T) {
		if got := bm.Get("list.1").Int(); got != 2 {
			t.Errorf("Get(list.1) = %d", got)
		}
		if got := bm.Get("m.a").Int(); got != 1 {
			t.Errorf("Get(m.a) = %d", got)
		}
	})

	t.Run("结构体指针与结构体本身结果一致", func(t *testing.T) {
		s := newSample()
		byPtr := Parse(&s)
		if got := byPtr.Get("name").String(); got != "kk" {
			t.Errorf("Parse(*struct).Get(name) = %q", got)
		}
		if got := byPtr.Get("e").String(); got != "emb" {
			t.Errorf("Parse(*struct).Get(e) = %q", got)
		}
	})
}

// unexportedEmb 未导出类型的匿名嵌入，专门用来钉住下面这条与 encoding/json 的差异
type unexportedEmb struct {
	E string `json:"e"`
}

type withUnexportedEmb struct {
	unexportedEmb
	Name string `json:"name"`
}

func TestStructUnpackUnexportedEmbedded(t *testing.T) {
	// 契约：未导出类型的匿名嵌入字段整体跳过，它里面的字段不会被平铺出来。
	//
	// 这一点与 encoding/json 不一样（json 会把 e 提升上来），差异是反射能力决定的：
	// 未导出字段名让 reflect 给这一层打上只读标记，对它调 Interface() 会 panic，
	// 所以只能跳过。需要平铺就把嵌入的类型名写成导出的。
	//
	// 这条断言存在的意义是：哪天有人"顺手对齐 json"，会先在这里看到红。
	bm := Parse(withUnexportedEmb{Name: "kk"})
	if got := bm.Get("name").String(); got != "kk" {
		t.Errorf("Get(name) = %q", got)
	}
	if bm.Get("e").IsExists() {
		t.Error("未导出类型的匿名嵌入不该被平铺")
	}
	if bm.Get("unexportedEmb").IsExists() {
		t.Error("未导出类型的匿名嵌入不该以字段名出现")
	}
	if got := bm.Keys(); len(got) != 1 || got[0] != "name" {
		t.Errorf("Keys() = %v, 期望 [name]", got)
	}
}

func TestStructUnpackTagName(t *testing.T) {
	type form struct {
		Name string `json:"name" form:"f_name"`
		Age  int    `json:"age" form:"f_age"`
	}

	t.Run("按 form 展开", func(t *testing.T) {
		bm := Parse(form{Name: "kk", Age: 9}, "form")
		if got := bm.Get("f_name").String(); got != "kk" {
			t.Errorf("Get(f_name) = %q", got)
		}
		if got := bm.Get("f_age").Int(); got != 9 {
			t.Errorf("Get(f_age) = %d", got)
		}
	})

	t.Run("嵌套结构体也要用同一个 tag", func(t *testing.T) {
		// 契约：TagName 一路往下传，深层字段同样按 form 展开
		type wrap struct {
			Sub form `json:"sub" form:"f_sub"`
		}
		bm := Parse(wrap{Sub: form{Name: "kk"}}, "form")
		if got := bm.Get("f_sub.f_name").String(); got != "kk" {
			t.Errorf("Get(f_sub.f_name) = %q", got)
		}
		if bm.Get("sub.name").IsExists() {
			t.Error("按 form 解析时不该命中 json 的键名")
		}
	})

	t.Run("同一个类型用不同 tag 解析互不串味", func(t *testing.T) {
		// 字段元信息带缓存，缓存键必须把 tag 名算进去
		byJSON := Parse(form{Name: "j"})
		byForm := Parse(form{Name: "f"}, "form")
		if got := byJSON.Get("name").String(); got != "j" {
			t.Errorf("按 json 取值 = %q", got)
		}
		if got := byForm.Get("f_name").String(); got != "f" {
			t.Errorf("按 form 取值 = %q", got)
		}
		// 再取一次 json 的，确认缓存没被 form 那次污染
		if got := byJSON.Get("name").String(); got != "j" {
			t.Errorf("缓存串味：按 json 取值 = %q", got)
		}
		if byForm.Get("name").IsExists() {
			t.Error("缓存串味：按 form 解析的节点命中了 json 键名")
		}
	})
}

// customMarshaler 自己决定怎么序列化
type customMarshaler struct {
	V string
}

func (c customMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom":"` + c.V + `"}`), nil
}

func TestStructUnpackJSONMarshaler(t *testing.T) {
	t.Run("实现 json.Marshaler 的类型走它自己的序列化", func(t *testing.T) {
		bm := Parse(customMarshaler{V: "x"})
		if got := bm.Get("custom").String(); got != "x" {
			t.Errorf("Get(custom) = %q, 期望 x", got)
		}
		if bm.Get("V").IsExists() {
			t.Error("走了 MarshalJSON 就不该再按字段名展开")
		}
	})

	t.Run("time.Time 也走 MarshalJSON", func(t *testing.T) {
		tm := time.Date(2026, 10, 9, 12, 30, 45, 0, time.UTC)
		bm := Parse(map[string]any{"t": tm})
		if got := bm.Get("t").String(); got != tm.Format(time.RFC3339) {
			t.Errorf("Get(t).String() = %q", got)
		}
		if got := bm.Get("t").Time(); !got.Equal(tm) {
			t.Errorf("Get(t).Time() = %v, 期望 %v", got, tm)
		}
	})

	t.Run("嵌在 map 里的 Marshaler 类型也能展开", func(t *testing.T) {
		bm := Parse(map[string]any{"c": customMarshaler{V: "y"}})
		if got := bm.Get("c.custom").String(); got != "y" {
			t.Errorf("Get(c.custom) = %q", got)
		}
	})
}

func TestStructUnpackDirect(t *testing.T) {
	t.Run("Unpack 返回 map", func(t *testing.T) {
		m, ok := NewStructUnpack(newSample()).Unpack().(map[string]any)
		if !ok {
			t.Fatalf("Unpack() 的类型是 %T, 期望 map[string]any", NewStructUnpack(newSample()).Unpack())
		}
		if m["name"] != "kk" || m["e"] != "emb" {
			t.Errorf("Unpack() = %#v", m)
		}
		if _, exists := m["Skip"]; exists {
			t.Error(`Unpack() 里不该有 tag 为 "-" 的字段`)
		}
	})

	t.Run("Unpack 默认用 json 标签", func(t *testing.T) {
		type form struct {
			Name string `json:"name" form:"f_name"`
		}
		m := NewStructUnpack(form{Name: "kk"}).Unpack().(map[string]any)
		if m["name"] != "kk" {
			t.Errorf("Unpack() = %#v", m)
		}
		m2 := NewStructUnpack(form{Name: "kk"}, "form").Unpack().(map[string]any)
		if m2["f_name"] != "kk" {
			t.Errorf(`Unpack(form) = %#v`, m2)
		}
	})

	t.Run("UnpackValue 单字段取值，结果与 Unpack 一致", func(t *testing.T) {
		// 走路径取值时只要一个字段，没必要把整个结构体拆成 map
		s := newSample()
		su := NewStructUnpack(s)
		for _, name := range []string{"name", "NoTag", "e", "tag", "list"} {
			fast, ok := su.UnpackValue(name)
			if !ok {
				t.Errorf("UnpackValue(%q) 说找不到", name)
				continue
			}
			want := su.Unpack().(map[string]any)[name]
			if !jsonEqual(fast, want) {
				t.Errorf("UnpackValue(%q) = %#v, Unpack 里是 %#v", name, fast, want)
			}
		}
		if _, ok := su.UnpackValue("Skip"); ok {
			t.Error(`tag 为 "-" 的字段不该被 UnpackValue 取到`)
		}
		if _, ok := su.UnpackValue("hidden"); ok {
			t.Error("未导出字段不该被 UnpackValue 取到")
		}
		if _, ok := su.UnpackValue("nope"); ok {
			t.Error("不存在的字段不该被取到")
		}
	})

	t.Run("UnpackValue 认得的键是 Unpack 的超集", func(t *testing.T) {
		// 匿名嵌入且没有自己 tag 的字段，在 Unpack 里是被平铺的：
		// 键名是它展开出来的成员（这里是 e），没有 EmbBase 这个键。
		su := NewStructUnpack(newSample())
		flat := su.Unpack().(map[string]any)
		if _, exists := flat["EmbBase"]; exists {
			t.Errorf("平铺的嵌入字段不该在 Unpack 里留下自己的键名：%#v", flat)
		}
		if flat["e"] != "emb" {
			t.Errorf("Unpack() 里应该有平铺上来的 e，实际是 %#v", flat)
		}

		// 但 UnpackValue 还认嵌入字段自己的名字，取到整个被嵌入的结构体。
		// 刻意留这个超集：Get 在结构体上走的就是这条路，
		// 少认一个键名就是一个取值失败，多认一个只是多一条能走通的路。
		v, ok := su.UnpackValue("EmbBase")
		if !ok {
			t.Fatal(`UnpackValue("EmbBase") 说找不到`)
		}
		if !jsonEqual(v, EmbBase{E: "emb"}) {
			t.Errorf(`UnpackValue("EmbBase") = %#v`, v)
		}

		// 平铺上来的成员名照样能取，两条路都通
		if v, ok := su.UnpackValue("e"); !ok || v != "emb" {
			t.Errorf(`UnpackValue("e") = %#v, %v`, v, ok)
		}

		// 键名不做大小写归一：找的是 Go 的字段名或 tag 名
		if _, ok := su.UnpackValue("embbase"); ok {
			t.Error("嵌入字段名不该按小写命中")
		}
		if _, ok := su.UnpackValue("notag"); ok {
			t.Error("没有 tag 的字段用原始字段名做键，不该按小写命中")
		}
	})

	t.Run("嵌入指针：Unpack 不平铺，UnpackValue 仍能取到成员", func(t *testing.T) {
		type withPtr struct {
			*EmbBase
			Name string `json:"name"`
		}
		su := NewStructUnpack(withPtr{EmbBase: &EmbBase{E: "emb"}, Name: "kk"})

		// 只有「匿名 + 非指针」的嵌入字段才平铺，指针的留在自己的键名下
		flat := su.Unpack().(map[string]any)
		if _, exists := flat["e"]; exists {
			t.Errorf("嵌入的是指针，不该被平铺：%#v", flat)
		}
		if _, exists := flat["EmbBase"]; !exists {
			t.Errorf("嵌入指针应该留在自己的键名下：%#v", flat)
		}

		// UnpackValue 认得键名的范围更宽，成员名照样能取到
		if v, ok := su.UnpackValue("e"); !ok || v != "emb" {
			t.Errorf(`UnpackValue("e") = %#v, %v`, v, ok)
		}

		// nil 的嵌入指针走不下去，但不能 panic
		nilSu := NewStructUnpack(withPtr{Name: "kk"})
		if _, ok := nilSu.UnpackValue("e"); ok {
			t.Error("嵌入指针是 nil 时不该取到成员")
		}
	})

	t.Run("非结构体传进来会 panic", func(t *testing.T) {
		// 契约：NewStructUnpack 只接受结构体（或结构体指针），别的一律 panic
		defer func() {
			if recover() == nil {
				t.Error("传入 map 应该 panic")
			}
		}()
		NewStructUnpack(map[string]any{})
	})
}

// jsonEqual 用 JSON 文本比较两个值，避开切片/映射不能用 == 比较的问题
func jsonEqual(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return err1 == nil && err2 == nil
	}
	return string(x) == string(y)
}
