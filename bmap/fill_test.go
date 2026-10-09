package bmap

import (
	"testing"
	"time"
)

// 本文件钉住 Fill / Scan 的回填规则。
//
// Fill 与 Scan 的分工：
//   - Fill 走 bmap 自己的宽容转换，认 TagName，字段类型对不上时尽力转（"9" 能填进 int）；
//   - Scan 走 encoding/json，严格但通用，类型不匹配会返回 error。
//
// 两者都是「数据形态不固定，但目标结构体是固定的」这个场景用的。

type FillEmb struct {
	E string `json:"e" form:"f_e"`
}

type FillTarget struct {
	FillEmb         // 匿名嵌入：平铺展开，回填时也要能填进去
	Name    string  `json:"name" form:"f_name"`
	Age     int     `json:"age" form:"f_age"`
	Score   float64 `json:"score"`
	OK      bool    `json:"ok"`
	Skip    string  `json:"-"`
	NoTag   string
	Ptr     *int           `json:"ptr"`
	NilPtr  *int           `json:"nil_ptr"`
	Sub     FillInner      `json:"sub"`
	List    []int          `json:"list"`
	Items   []FillInner    `json:"items"`
	M       map[string]int `json:"m"`
	Any     any            `json:"any"`
	Tm      time.Time      `json:"tm"`
	private string         //nolint:unused // 未导出字段，回填时必须跳过
}

type FillInner struct {
	X int    `json:"x"`
	Y string `json:"y"`
}

func TestFillBasic(t *testing.T) {
	t.Run("基本类型", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{
			"name": "kk", "age": 9, "score": 1.5, "ok": true, "NoTag": "nt",
		}).Fill(&dst)

		if dst.Name != "kk" || dst.Age != 9 || dst.Score != 1.5 || !dst.OK || dst.NoTag != "nt" {
			t.Errorf("回填结果 = %+v", dst)
		}
	})

	t.Run("宽容转换：字符串形态的数字也能填进数字字段", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"age": "12", "score": "2.5", "ok": "true"}).Fill(&dst)
		if dst.Age != 12 || dst.Score != 2.5 || !dst.OK {
			t.Errorf("回填结果 = %+v", dst)
		}
	})

	t.Run("数据里没有的字段保持原值", func(t *testing.T) {
		dst := FillTarget{Name: "keep", Age: 3}
		Parse(map[string]any{"age": 9}).Fill(&dst)
		if dst.Name != "keep" {
			t.Errorf("没有 name 时不该被清空，得到 %q", dst.Name)
		}
		if dst.Age != 9 {
			t.Errorf("Age = %d, 期望 9", dst.Age)
		}
	})

	t.Run(`tag 是 "-" 的字段不回填`, func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"-": "x", "Skip": "y"}).Fill(&dst)
		if dst.Skip != "" {
			t.Errorf("Skip = %q, 期望保持空", dst.Skip)
		}
	})

	t.Run("未导出字段跳过，不 panic", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"private": "x", "name": "kk"}).Fill(&dst)
		if dst.Name != "kk" {
			t.Errorf("Name = %q", dst.Name)
		}
	})

	t.Run("指针字段", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"ptr": 7}).Fill(&dst)
		if dst.Ptr == nil {
			t.Fatal("Ptr 没被创建")
		}
		if *dst.Ptr != 7 {
			t.Errorf("*Ptr = %d, 期望 7", *dst.Ptr)
		}
		// 数据里没有这个键时不该凭空造一个指针
		if dst.NilPtr != nil {
			t.Errorf("NilPtr 应该是 nil，实际指向 %d", *dst.NilPtr)
		}
	})

	t.Run("嵌套结构体", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"sub": map[string]any{"x": 1, "y": "yy"}}).Fill(&dst)
		if dst.Sub.X != 1 || dst.Sub.Y != "yy" {
			t.Errorf("Sub = %+v", dst.Sub)
		}
	})

	t.Run("切片", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{
			"list":  []any{1, 2, 3},
			"items": []any{map[string]any{"x": 1, "y": "a"}, map[string]any{"x": 2, "y": "b"}},
		}).Fill(&dst)

		if len(dst.List) != 3 || dst.List[2] != 3 {
			t.Errorf("List = %v", dst.List)
		}
		if len(dst.Items) != 2 || dst.Items[1].X != 2 || dst.Items[1].Y != "b" {
			t.Errorf("Items = %+v", dst.Items)
		}
	})

	t.Run("map", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"m": map[string]any{"a": 1, "b": 2}}).Fill(&dst)
		if len(dst.M) != 2 || dst.M["b"] != 2 {
			t.Errorf("M = %v", dst.M)
		}
	})

	t.Run("map 的键名里含点也能回填", func(t *testing.T) {
		// Foreach 给回调的值直接从容器取，不能拿键名再走一遍 Get：
		// 含点的键名会被当成路径，填进去的就是零值
		var dst map[string]string
		Parse(map[string]any{"a.b": "带点"}).Fill(&dst)
		if dst["a.b"] != "带点" {
			t.Errorf("回填结果 = %v", dst)
		}
	})

	t.Run("any 字段拿到原始值", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"any": map[string]any{"k": "v"}}).Fill(&dst)
		m, ok := dst.Any.(map[string]any)
		if !ok || m["k"] != "v" {
			t.Errorf("Any = %#v", dst.Any)
		}
	})

	t.Run("time.Time 字段", func(t *testing.T) {
		var dst FillTarget
		Parse(map[string]any{"tm": "2026-10-09 12:30:45"}).Fill(&dst)
		if dst.Tm.IsZero() {
			t.Error("Tm 没被填上")
		}
		if got := dst.Tm.Format(time.DateTime); got != "2026-10-09 12:30:45" {
			t.Errorf("Tm = %q", got)
		}
	})
}

func TestFillAnonymousEmbedded(t *testing.T) {
	t.Run("匿名嵌入的字段能回填", func(t *testing.T) {
		// 嵌入字段在展开阶段被平铺到了上一层，
		// 回填时就要对着同一份数据递归进去，而不是去找那个并不存在的键名。
		var dst FillTarget
		Parse(map[string]any{"e": "emb", "name": "kk"}).Fill(&dst)
		if dst.E != "emb" {
			t.Errorf("嵌入字段 E = %q, 期望 emb", dst.E)
		}
		if dst.Name != "kk" {
			t.Errorf("Name = %q", dst.Name)
		}
	})

	t.Run("往返一致：展开再回填应该拿回同样的数据", func(t *testing.T) {
		src := FillTarget{Name: "kk", Age: 9}
		src.E = "emb"

		var dst FillTarget
		Parse(src).Fill(&dst)
		if dst.Name != src.Name || dst.Age != src.Age || dst.E != src.E {
			t.Errorf("往返结果 = %+v, 原始 = %+v", dst, src)
		}
	})

	t.Run("匿名嵌入的指针也会创建", func(t *testing.T) {
		type outer struct {
			*FillEmb
			Name string `json:"name"`
		}
		var dst outer
		Parse(map[string]any{"e": "emb", "name": "kk"}).Fill(&dst)
		if dst.FillEmb == nil {
			t.Fatal("嵌入的指针没被创建")
		}
		if dst.E != "emb" || dst.Name != "kk" {
			t.Errorf("回填结果 = %+v", dst)
		}
	})
}

func TestFillTagName(t *testing.T) {
	t.Run("按 form 标签回填", func(t *testing.T) {
		// 回填读的标签名跟 Parse 用的保持一致，
		// 否则按 form 解析出来的数据只能按 json 的键名去填，一个也对不上
		var dst FillTarget
		Parse(map[string]any{"f_name": "kk", "f_age": 9, "f_e": "emb"}, "form").Fill(&dst)
		if dst.Name != "kk" || dst.Age != 9 || dst.E != "emb" {
			t.Errorf("回填结果 = %+v", dst)
		}
	})

	t.Run("从结构体到结构体：两边用同一个 tag", func(t *testing.T) {
		type formSrc struct {
			Name string `json:"name" form:"f_name"`
			Age  int    `json:"age" form:"f_age"`
		}
		var dst FillTarget
		Parse(formSrc{Name: "kk", Age: 9}, "form").Fill(&dst)
		if dst.Name != "kk" || dst.Age != 9 {
			t.Errorf("回填结果 = %+v", dst)
		}
	})

	t.Run("Fill 到结构体指针之外的目标", func(t *testing.T) {
		var n int
		Parse("42").Fill(&n)
		if n != 42 {
			t.Errorf("*int 回填结果 = %d", n)
		}

		var s string
		Parse(7).Fill(&s)
		if s != "7" {
			t.Errorf("*string 回填结果 = %q", s)
		}

		var list []int
		Parse([]any{1, 2}).Fill(&list)
		if len(list) != 2 || list[1] != 2 {
			t.Errorf("*[]int 回填结果 = %v", list)
		}

		var m map[string]int
		Parse(map[string]any{"a": 1}).Fill(&m)
		if m["a"] != 1 {
			t.Errorf("*map 回填结果 = %v", m)
		}
	})

	t.Run("传进来不是指针会 panic", func(t *testing.T) {
		// 契约：非指针拿不到可设置的值，直接 panic 而不是静默什么都不做
		defer func() {
			if recover() == nil {
				t.Error("Fill(非指针) 应该 panic")
			}
		}()
		Parse(map[string]any{}).Fill(FillTarget{})
	})
}

func TestScan(t *testing.T) {
	t.Run("按 encoding/json 的口径回填", func(t *testing.T) {
		var dst struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		if err := Parse(`{"name":"kk","age":9}`).Scan(&dst); err != nil {
			t.Fatalf("Scan 报错 %v", err)
		}
		if dst.Name != "kk" || dst.Age != 9 {
			t.Errorf("回填结果 = %+v", dst)
		}
	})

	t.Run("认 json 标签，不认 TagName", func(t *testing.T) {
		// Scan 走的是 encoding/json，所以只认 json 标签，
		// 要按别的标签回填得用 Fill。
		var dst FillTarget
		err := Parse(map[string]any{"f_name": "kk"}, "form").Scan(&dst)
		if err != nil {
			t.Fatalf("Scan 报错 %v", err)
		}
		if dst.Name != "" {
			t.Errorf("Scan 不该认 form 标签，Name = %q", dst.Name)
		}
	})

	t.Run("类型对不上会返回 error", func(t *testing.T) {
		var dst struct {
			Age int `json:"age"`
		}
		if err := Parse(`{"age":"abc"}`).Scan(&dst); err == nil {
			t.Error("类型不匹配时 Scan 应该返回 error")
		}
	})

	t.Run("节点不存在时不报错，目标保持零值", func(t *testing.T) {
		var dst FillTarget
		if err := Parse(nil).Scan(&dst); err != nil {
			t.Fatalf("Scan(nil) 报错 %v", err)
		}
		if dst.Name != "" {
			t.Errorf("Name = %q", dst.Name)
		}
	})

	t.Run("结构体节点也能 Scan", func(t *testing.T) {
		var dst FillInner
		if err := Parse(FillInner{X: 1, Y: "y"}).Scan(&dst); err != nil {
			t.Fatalf("Scan 报错 %v", err)
		}
		if dst.X != 1 || dst.Y != "y" {
			t.Errorf("回填结果 = %+v", dst)
		}
	})
}

func TestCopy(t *testing.T) {
	t.Run("Copy 出来的节点与源互不影响", func(t *testing.T) {
		src := Parse(map[string]any{"a": 1})
		cp := src.Copy()
		if got := cp.Get("a").Int(); got != 1 {
			t.Errorf("副本取值 = %d", got)
		}
		cp.Set("a", 2)
		if got := src.Get("a").Int(); got != 1 {
			t.Errorf("改副本影响到了源，源 = %d", got)
		}
		if got := cp.Get("a").Int(); got != 2 {
			t.Errorf("副本 = %d", got)
		}
	})

	t.Run("Copy 保留 TagName", func(t *testing.T) {
		type form struct {
			Name string `json:"name" form:"f_name"`
		}
		cp := Parse(form{Name: "kk"}, "form").Copy()
		if cp.TagName != "form" {
			t.Errorf("副本 TagName = %q", cp.TagName)
		}
		if got := cp.Get("f_name").String(); got != "kk" {
			t.Errorf("副本取值 = %q", got)
		}
	})
}
