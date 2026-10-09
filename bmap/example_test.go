package bmap_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/k-kkong/dataschema/bmap"
)

// 本文件是 bmap 的用法案例集，按能力逐个给例子。
//
// 与仓库根目录的 example_test.go 不同，这里的案例不碰数据库、不读文件，
// 所以每一个都带 // Output: 注释——go test 会真的跑一遍并逐字比对输出，
// pkg.go.dev 上展示的也是校验过的结果，不会出现"文档写的和实际跑的不一样"。
//
//	go test -run Example ./bmap/
//
// 因为输出要能逐字比对，涉及 map 遍历的地方一律用 SortedForeach 或 Keys，
// 涉及时间的地方一律避开受机器时区影响的写法。

// ---------------------------------------------------------------------------
// 入门
// ---------------------------------------------------------------------------

// 案例：拿到一份结构不固定的数据，按路径取任意深度的值。
//
// 这是 bmap 最典型的用法：不必为每种响应体先定义一个结构体，
// 中间任何一层不存在都不会 panic，取值器会给各自的零值。
func Example() {
	body := `{
		"code": 0,
		"data": {
			"list": [
				{"id": 1, "user": {"name": "kk", "vip": true}},
				{"id": 2, "user": {"name": "bb", "vip": false}}
			],
			"total": 2
		}
	}`

	bm := bmap.Parse(body)
	fmt.Println("code:", bm.Get("code").Int())
	fmt.Println("total:", bm.Get("data.total").Int())

	for _, row := range bm.Get("data.list").Array() {
		fmt.Printf("  %d %s vip=%v\n",
			row.Get("id").Int(),
			row.Get("user.name").String(),
			row.Get("user.vip").Bool())
	}

	// 走不通的路径不需要层层判空
	fmt.Println("missing:", bm.Get("data.list.9.user.name").String() == "")
	// Output:
	// code: 0
	// total: 2
	//   1 kk vip=true
	//   2 bb vip=false
	// missing: true
}

// 案例：BMap 是一份数据的惰性视图，Parse 一次之后按路径反复取。
//
// 它不复制数据，也不提前把整棵树拆开，取到哪一层才算到哪一层。
func ExampleBMap() {
	bm := bmap.Parse(map[string]any{"a": map[string]any{"b": 1}})

	fmt.Println(bm.Get("a.b").Int())
	fmt.Println(bm.Get("a").IsObject())
	fmt.Println(bm.IsExists())
	// Output:
	// 1
	// true
	// true
}

// 案例：Parse 接受哪些形态的输入。
//
// map、切片、结构体、JSON 文本、字节流、标量都能直接丢进来，不用先转换。
// Parse 不返回 error 也不会 panic：解析不出来的文本按普通字符串保留，
// nil 变成一个"不存在的节点"。
func ExampleParse() {
	fmt.Println(bmap.Parse(map[string]any{"a": 1}).Get("a").Int())
	fmt.Println(bmap.Parse(`{"a":2}`).Get("a").Int())
	fmt.Println(bmap.Parse([]byte(`{"a":3}`)).Get("a").Int())
	fmt.Println(bmap.Parse([]any{4}).At(0).Int())

	type user struct {
		Name string `json:"name"`
	}
	fmt.Println(bmap.Parse(user{Name: "kk"}).Get("name").String())

	// 看着像 JSON 却解析不了：数据不丢，原文还在，Err() 能查到
	bad := bmap.Parse(`{"a":`)
	fmt.Println(bad.String(), bad.Err() != nil)

	// 普通字符串解析不出来是正常的，不算错误
	fmt.Println(bmap.Parse("abc").Err() == nil)

	// nil 是一个不存在的节点
	fmt.Println(bmap.Parse(nil).IsExists())

	// 传入 *BMap 原样返回，链式调用里重复 Parse 不会套娃
	once := bmap.Parse(map[string]any{"a": 5})
	fmt.Println(bmap.Parse(once) == once)
	// Output:
	// 1
	// 2
	// 3
	// 4
	// kk
	// {"a": true
	// true
	// false
	// true
}

// 案例：结构体按别的标签展开。
//
// 默认按 json 标签，第二个参数可以换成 form、gorm 等任意标签名。
// TagName 会跟着子节点一路传下去，所以深层字段和数组元素用的是同一套键名。
func ExampleParse_tagName() {
	type Form struct {
		Name string `json:"name" form:"f_name"`
		Age  int    `json:"age" form:"f_age"`
	}

	fmt.Println(bmap.Parse(Form{Name: "kk", Age: 9}).Get("name").String())

	// 换成 form 之后，json 的键名就不再命中
	byForm := bmap.Parse(Form{Name: "kk", Age: 9}, "form")
	fmt.Println(byForm.Get("f_name").String(), byForm.Get("name").IsExists())

	// 数组元素同样按 form 展开
	list := bmap.Parse([]Form{{Name: "a"}, {Name: "b"}}, "form")
	fmt.Println(list.At(1).Get("f_name").String())
	// Output:
	// kk
	// kk false
	// b
}

// ---------------------------------------------------------------------------
// 取值：Get 与路径规则
// ---------------------------------------------------------------------------

// 案例：多层路径与数组下标。
//
// 路径用 . 分段，数组用下标，一段写错整条链就到此为止，
// 后面的取值器一律给零值，不会 panic。
func ExampleBMap_Get() {
	bm := bmap.Parse(`{
		"data": {
			"list": [
				{"id": 1, "user": {"name": "kk"}},
				{"id": 2, "user": {"name": "bb"}}
			],
			"total": 2
		}
	}`)

	fmt.Println(bm.Get("data.total").Int())
	fmt.Println(bm.Get("data.list.0.user.name").String())
	fmt.Println(bm.Get("data.list.1.id").Int())
	// Output:
	// 2
	// kk
	// 2
}

// 案例：键名里本身含有 . 时用反斜杠转义。
//
// 不转义时 a.b 一定是"a 下面的 b"，这条规则不能变；
// 要取字面键名为 a.b 的那一项，写成 `a\.b`（与 gjson 一致）。
// Set 与 Delete 也认同一套转义。
func ExampleBMap_Get_escaped() {
	bm := bmap.Parse(map[string]any{
		"a":   map[string]any{"b": "路径取到的"},
		"a.b": "转义取到的",
	})

	fmt.Println(bm.Get("a.b").String())
	fmt.Println(bm.Get(`a\.b`).String())
	// Output:
	// 路径取到的
	// 转义取到的
}

// 案例：路径走不通时的空安全。
//
// 拿到的是"不存在的节点"而不是 nil，所以可以继续往下点、继续取值，
// 调用方不需要写一串 if 判空。
func ExampleBMap_Get_missing() {
	bm := bmap.Parse(`{"a":{"b":1}}`)

	fmt.Println(bm.Get("a.x.y.z").IsExists())
	fmt.Println(bm.Get("a.x").Int())
	fmt.Printf("%q\n", bm.Get("a.x").String())

	// 在标量上继续往下走也是同样的结果
	fmt.Println(bm.Get("a.b.c.d").Float())
	fmt.Println(len(bm.Get("a.b.c.d").Array()))
	// Output:
	// false
	// 0
	// ""
	// 0
	// 1
}

// 案例：区分"值就是零"和"路径根本不存在"。
//
// 这两种情况下 Int()/String() 都返回零值，光看取值结果分不出来，
// 需要区分时用 IsExists()。
func ExampleBMap_IsExists() {
	bm := bmap.Parse(`{"zero":0,"empty":"","none":null}`)

	for _, key := range []string{"zero", "empty", "none", "nope"} {
		n := bm.Get(key)
		fmt.Printf("%-5s Int=%d String=%q exists=%v\n", key, n.Int(), n.String(), n.IsExists())
	}
	// Output:
	// zero  Int=0 String="0" exists=true
	// empty Int=0 String="" exists=true
	// none  Int=0 String="" exists=true
	// nope  Int=0 String="" exists=false
}

// 案例：结构体不用先转成 map，路径能直接穿过去。
//
// 数据库驱动、上游 SDK 返回的数据里常常混着结构体，
// 先按 TagName 定位字段、定位不到再退回整包展开，
// 所以字段很多的结构体也不会把取值拖慢。
func ExampleBMap_Get_struct() {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
		Addr struct {
			City string `json:"city"`
		} `json:"addr"`
	}

	u := User{Name: "kk", Age: 9}
	u.Addr.City = "Beijing"
	bm := bmap.Parse(map[string]any{"user": u, "list": []any{u}})

	fmt.Println(bm.Get("user.name").String())
	fmt.Println(bm.Get("user.addr.city").String())
	fmt.Println(bm.Get("list.0.age").Int())

	// 结构体指针也一样
	fmt.Println(bmap.Parse(map[string]any{"u": &u}).Get("u.name").String())
	// Output:
	// kk
	// Beijing
	// 9
	// kk
}

// 案例：map 的键值类型不一定是 string 和 any。
//
// yaml 解析出来的是 map[any]any，有些模型用 map[string]string，
// 这些都能直接取。键类型对不上时就是"不存在"，不会 panic。
func ExampleBMap_Get_otherMapTypes() {
	fmt.Println(bmap.Parse(map[any]any{"name": "kk", "age": 28}).Get("name").String())
	fmt.Println(bmap.Parse(map[string]int{"n": 7}).Get("n").Int())
	fmt.Println(bmap.Parse(map[string]string{"s": "x"}).Get("s").String())

	// 键是 int 的 map 用字符串路径取不到，结果是"不存在"而不是 panic
	fmt.Println(bmap.Parse(map[int]string{1: "x"}).Get("1").IsExists())
	// Output:
	// kk
	// 7
	// x
	// false
}

// 案例：匿名嵌入的字段被平铺上来，路径能直接取到。
//
// 展开时没有自己标签的匿名嵌入字段会平铺到上一层，
// 取值时也跟着往里找，所以不必写 u.Base.id 这种完整层级。
// 字段名没有 tag 时直接用 Go 的字段名做键，不会转小写。
func ExampleBMap_Get_embedded() {
	type Base struct {
		ID int64 `json:"id"`
	}
	type User struct {
		Base
		Name string `json:"name"`
	}

	bm := bmap.Parse(map[string]any{"u": User{Base: Base{ID: 7}, Name: "kk"}})
	fmt.Println(bm.Get("u.id").Int64())
	fmt.Println(bm.Get("u.name").String())

	// 展开时 Base 这一层被平铺掉了，所以 Map() 里没有 "Base" 这个键；
	// 但按嵌入字段自己的名字仍然能取到整个被嵌入的结构体
	fmt.Println(bm.Get("u").Keys())
	fmt.Println(bm.Get("u.Base").String())
	// Output:
	// 7
	// kk
	// [id name]
	// {"id":7}
}

// ---------------------------------------------------------------------------
// 取值器：各种类型之间的宽容转换
// ---------------------------------------------------------------------------

// 案例：String 的输出口径。
//
// 对象与数组输出 JSON 文本，字符串原样返回（不带引号），
// 数字按最短写法输出，字节流按文本看，没有值时是空串。
func ExampleBMap_String() {
	for _, src := range []any{"abc", 123, int64(-5), uint8(7), 1.5, true, []any{1, 2}, nil} {
		fmt.Printf("%q\n", bmap.Parse(src).String())
	}

	// 对象输出的是 JSON，键按字典序排列，所以文本是稳定的
	fmt.Println(bmap.Parse(map[string]any{"b": 2, "a": 1}).String())

	// 字节流承载的通常是一段文本，不是一串数字
	fmt.Printf("%q\n", bmap.Parse(map[string]any{"raw": []byte("hi")}).Get("raw").String())

	// 字节数组也算字节流
	fmt.Printf("%q\n", bmap.Parse([3]byte{'a', 'b', 'c'}).String())

	// 底层值里有 json 处理不了的东西（chan、func、循环引用）时，
	// 退一步给一个能看出是什么的文本，而不是一个静默的空串——
	// 空串会让人分不清是"值就是空"还是"序列化失败了"
	ch := make(chan int)
	fmt.Println(bmap.Parse(map[string]any{"c": ch}).String() != "")
	fmt.Println(bmap.Parse(map[string]any{"c": ch}).Get("c").String() != "")
	// Output:
	// "abc"
	// "123"
	// "-5"
	// "7"
	// "1.5"
	// "true"
	// "[1,2]"
	// ""
	// {"a":1,"b":2}
	// "hi"
	// "abc"
	// true
	// true
}

// 案例：整数取值。
//
// 数字字符串会被解析，浮点数朝零截断，布尔给 1 或 0，
// 解析不出来就是 0，不报错。Int64/Int32 同理，只是位宽不同。
func ExampleBMap_Int() {
	bm := bmap.Parse(map[string]any{"i": 42, "s": "42", "f": 42.9, "neg": -42.9, "b": true, "bad": "12abc"})

	for _, key := range []string{"i", "s", "f", "neg", "b", "bad", "nope"} {
		n := bm.Get(key)
		fmt.Printf("%-4s Int=%-4d Int64=%-4d Int32=%d\n", key, n.Int(), n.Int64(), n.Int32())
	}
	// Output:
	// i    Int=42   Int64=42   Int32=42
	// s    Int=42   Int64=42   Int32=42
	// f    Int=42   Int64=42   Int32=42
	// neg  Int=-42  Int64=-42  Int32=-42
	// b    Int=1    Int64=1    Int32=1
	// bad  Int=0    Int64=0    Int32=0
	// nope Int=0    Int64=0    Int32=0
}

// 案例：浮点取值。
//
// 整数、数字字符串、布尔都能转，Float32/Float64 只是位宽不同，
// Float64() 与 Float() 完全等价。
func ExampleBMap_Float() {
	bm := bmap.Parse(map[string]any{"f": 1.5, "s": "2.25", "i": 3, "b": true, "bad": "abc"})

	for _, key := range []string{"f", "s", "i", "b", "bad", "nope"} {
		n := bm.Get(key)
		fmt.Printf("%-4s Float=%-5v Float32=%-5v Float64=%v\n", key, n.Float(), n.Float32(), n.Float64())
	}
	// Output:
	// f    Float=1.5   Float32=1.5   Float64=1.5
	// s    Float=2.25  Float32=2.25  Float64=2.25
	// i    Float=3     Float32=3     Float64=3
	// b    Float=1     Float32=1     Float64=1
	// bad  Float=0     Float32=0     Float64=0
	// nope Float=0     Float32=0     Float64=0
}

// 案例：无符号取值。
//
// 负数按 0 处理——无符号字段填一个负数没有意义，
// 给 0 比给一个绕回去的大数更容易发现问题。
func ExampleBMap_Uint() {
	bm := bmap.Parse(map[string]any{
		"u": uint64(18446744073709551615), "i": 7, "s": "8", "f": 9.7, "b": true, "neg": -1,
		"fs": "9.7",
	})

	for _, key := range []string{"u", "i", "s", "f", "b", "neg", "fs", "nope"} {
		fmt.Printf("%-4s %d\n", key, bm.Get(key).Uint())
	}
	// Output:
	// u    18446744073709551615
	// i    7
	// s    8
	// f    9
	// b    1
	// neg  0
	// fs   9
	// nope 0
}

// 案例：布尔取值。
//
// strconv.ParseBool 认得的写法（"true"/"TRUE"/"t"/"1"/"false"/"F"/"0"）按它的口径走；
// 认不出来的按 gjson 的口径兜底：数字非零为真，字符串非空且不是 "0" 为真，
// 对象与数组有内容就算真，null 与不存在的节点为假。
func ExampleBMap_Bool() {
	// strconv.ParseBool 认得的写法按它的口径走
	for _, s := range []string{"true", "TRUE", "t", "1", "false", "F", "0"} {
		fmt.Printf("%-5s => %v\n", s, bmap.Parse(s).Bool())
	}

	// 认不出来的按 gjson 的口径兜底
	for _, src := range []any{0.5, -1, 0, "yes", "", []any{}, map[string]any{}, nil} {
		fmt.Printf("%v => %v\n", src, bmap.Parse(src).Bool())
	}
	// Output:
	// true  => true
	// TRUE  => true
	// t     => true
	// 1     => true
	// false => false
	// F     => false
	// 0     => false
	// 0.5 => true
	// -1 => true
	// 0 => false
	// yes => true
	//  => false
	// [] => true
	// map[] => true
	// <nil> => false
}

// 案例：拿到底层值本身。
//
// 指针与接口会一路解到底，所以 *int、any 里装的东西拿到的都是最终那个值。
// 没有值时返回 nil。
func ExampleBMap_Value() {
	bm := bmap.Parse(map[string]any{"n": 7, "s": "x"})

	fmt.Printf("%T %v\n", bm.Get("n").Value(), bm.Get("n").Value())
	fmt.Println(bm.Get("nope").Value() == nil)

	n := 7
	fmt.Println(bmap.Parse(&n).Value())

	// 反复取值结果稳定，取值器不会改写节点自身
	for i := 0; i < 3; i++ {
		fmt.Print(bmap.Parse(&n).Value())
	}
	fmt.Println()
	// Output:
	// int 7
	// true
	// 7
	// 777
}

// 案例：拿到对象形态的数据。
//
// 不是对象时返回一个空 map 而不是 nil，所以可以直接 range，不用判空。
// 键不是 string 的 map（例如 yaml 解析出来的 map[any]any）会把键转成字符串。
func ExampleBMap_Map() {
	m := bmap.Parse(map[string]any{"b": 2, "a": 1}).Map()
	fmt.Println(m) // fmt 打印 map 时会按键排序，输出是稳定的
	fmt.Println(len(m), m["a"])

	// 结构体节点会按 TagName 展开
	type user struct {
		Name string `json:"name"`
	}
	fmt.Println(bmap.Parse(map[string]any{"u": user{Name: "kk"}}).Get("u").Map())

	fmt.Println(len(bmap.Parse("abc").Map()))
	// Output:
	// map[a:1 b:2]
	// 2 1
	// map[name:kk]
	// 0
}

// 案例：路径不存在时用调用方给的默认值。
//
// Or 系列与直接取值的区别是它分得清"值就是零"和"路径走不通"：
// 前者返回真实的零值，后者返回默认值，省掉一层 if。
func ExampleBMap_StringOr() {
	bm := bmap.Parse(map[string]any{"a": 1, "s": "x", "f": 1.5, "b": true})

	// 节点存在时拿到真实值，不存在时拿到默认值
	fmt.Println(bm.Get("a").IntOr(99), bm.Get("nope").IntOr(99))
	fmt.Println(bm.Get("s").StringOr("默认值"), bm.Get("nope").StringOr("默认值"))
	fmt.Println(bm.Get("f").FloatOr(0.5), bm.Get("nope").FloatOr(0.5))
	fmt.Println(bm.Get("b").BoolOr(false), bm.Get("nope").BoolOr(true))
	fmt.Println(bm.Get("a").ValueOr("兜底"), bm.Get("nope").ValueOr("兜底"))
	// Output:
	// 1 99
	// x 默认值
	// 1.5 0.5
	// true true
	// 1 兜底
}

// ---------------------------------------------------------------------------
// 形态判定
// ---------------------------------------------------------------------------

// 案例：先看清楚是什么形态，再决定怎么取。
//
// Type() 回答"这里到底是什么"，Is 系列是它的快捷写法。
// 走不通的路径与 null 都是 TypeNull，要区分用 IsExists()。
func ExampleBMap_Type() {
	for _, src := range []any{nil, "abc", 1, 1.5, true, map[string]any{"a": 1}, []any{1}} {
		bm := bmap.Parse(src)
		fmt.Printf("%-6s string=%-5v number=%-5v bool=%v\n",
			bm.Type(), bm.IsString(), bm.IsNumber(), bm.IsBool())
	}
	// Output:
	// null   string=false number=false bool=false
	// string string=true  number=false bool=false
	// number string=false number=true  bool=false
	// number string=false number=true  bool=false
	// bool   string=false number=false bool=true
	// object string=false number=false bool=false
	// array  string=false number=false bool=false
}

// 案例：ValueType 的可读名称，打日志和报错时直接用。
func ExampleValueType_String() {
	fmt.Println(bmap.TypeNull, bmap.TypeString, bmap.TypeNumber,
		bmap.TypeBool, bmap.TypeObject, bmap.TypeArray)
	// Output:
	// null string number bool object array
}

// 案例：三个形态判定的分工。
//
// IsArray 看是不是数组，IsObject 看是不是对象，IsNil 看有没有值。
// 三者互不干扰，反复调用结果稳定（判定函数不带副作用）。
func ExampleBMap_IsArray() {
	bm := bmap.Parse(`{"list":[1,2],"obj":{"a":1},"none":null}`)

	for _, key := range []string{"list", "obj", "none", "nope"} {
		n := bm.Get(key)
		fmt.Printf("%-5s exists=%-5v array=%-5v object=%-5v nil=%v\n",
			key, n.IsExists(), n.IsArray(), n.IsObject(), n.IsNil())
	}
	// Output:
	// list  exists=true  array=true  object=false nil=false
	// obj   exists=true  array=false object=true  nil=false
	// none  exists=true  array=false object=false nil=true
	// nope  exists=false array=false object=false nil=true
}

// 案例：元素个数。
//
// 数组给长度，对象给键数，其它一律 0。
// 注意它和 Array() 不是一回事：标量的 Array() 长度是 1，Len() 是 0。
func ExampleBMap_Len() {
	fmt.Println(bmap.Parse([]any{1, 2, 3}).Len())
	fmt.Println(bmap.Parse(map[string]any{"a": 1, "b": 2}).Len())
	fmt.Println(bmap.Parse("abc").Len())
	fmt.Println(len(bmap.Parse("abc").Array()))
	// Output:
	// 3
	// 2
	// 0
	// 1
}

// 案例：拿到对象的键或数组的下标。
//
// 结果按字典序排好，调用方不用再 sort——Go 的 map 遍历顺序每次都不同，
// 拼日志、生成语句、做快照对比这类场合需要一个确定的顺序。
func ExampleBMap_Keys() {
	fmt.Println(bmap.Parse(map[string]any{"city": "Beijing", "name": "kk", "age": 28}).Keys())
	fmt.Println(bmap.Parse([]any{"x", "y"}).Keys())
	fmt.Println(len(bmap.Parse("abc").Keys()))
	// Output:
	// [age city name]
	// [0 1]
	// 0
}

// 案例：Map() 对各种形态的对象都给得出结果。
func ExampleBMap_Map_otherTypes() {
	// 键不是 string 的 map 会把键转成字符串（yaml 解析出来的常见形态）
	fmt.Println(bmap.Parse(map[any]any{"b": 2, "a": 1}).Map())

	// 值类型不是 any 的 map 也一样
	fmt.Println(bmap.Parse(map[string]int{"n": 7}).Map())

	// 结构体节点按 TagName 展开
	type user struct {
		Name string `json:"name"`
	}
	fmt.Println(bmap.Parse(map[string]any{"u": user{Name: "kk"}}).Get("u").Map())
	// Output:
	// map[a:1 b:2]
	// map[n:7]
	// map[name:kk]
}

// 案例：结构体节点的键与个数，口径与展开结果一致。
func ExampleBMap_Keys_struct() {
	type user struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
		Skip string `json:"-"`
	}

	bm := bmap.Parse(map[string]any{"u": user{Name: "kk", Age: 9, Skip: "不会出现"}}).Get("u")
	fmt.Println(bm.Keys())
	fmt.Println(bm.Len())
	// Output:
	// [age name]
	// 2
}

// ---------------------------------------------------------------------------
// 数组
// ---------------------------------------------------------------------------

// 案例：Array() 的语义是"把内容转成数组，然后看有几个"。
//
// 所以它和 gjson 的 Array() 不一样，不是"只有 JSON 数组才展开"：
// 任何不是数组的值都会被当成长度 1 的数组，元素就是它自己。
// 这样接口返回一条记录和返回一批记录可以用同一段代码处理。
//
// 代价是标量上也会跑一次循环。要区分"真数组"和"被提升的标量"先用 IsArray()，
// 要元素个数用 Len()。
func ExampleBMap_Array() {
	// 同一个接口，data 有时是一个对象、有时是一个数组
	single := `{"data":{"id":1,"name":"kk"}}`
	list := `{"data":[{"id":1,"name":"kk"},{"id":2,"name":"bb"}]}`

	for _, body := range []string{single, list} {
		rows := bmap.Parse(body).Get("data").Array()
		fmt.Printf("共 %d 条\n", len(rows))
		for _, row := range rows {
			fmt.Printf("  %d %s\n", row.Get("id").Int(), row.Get("name").String())
		}
	}

	// 不存在的节点也返回长度 1，元素是不存在的节点，遍历它不会 panic
	missing := bmap.Parse(nil).Array()
	fmt.Println(len(missing), missing[0].IsExists())
	// Output:
	// 共 1 条
	//   1 kk
	// 共 2 条
	//   1 kk
	//   2 bb
	// 1 false
}

// 案例：数组里的元素是什么形态都能处理。
//
// 元素走的是 Parse，所以结构体会按 TagName 展开、字符串形态的 JSON 会被解析成结构、
// 字节流按文本看，nil 元素则是一个不存在的节点，继续取值不会 panic。
func ExampleBMap_Array_elements() {
	type user struct {
		Name string `json:"name"`
	}

	bm := bmap.Parse([]any{
		user{Name: "kk"}, // 结构体元素按 TagName 展开
		`{"name":"bb"}`,  // 字符串形态的 JSON 会被解析成结构
		[]byte("raw"),    // 字节流按文本看，不当成一串数字
		nil,              // nil 元素是不存在的节点
	})

	for i, item := range bm.Array() {
		fmt.Printf("%d name=%q exists=%v\n", i, item.Get("name").String(), item.IsExists())
	}
	// Output:
	// 0 name="kk" exists=true
	// 1 name="bb" exists=true
	// 2 name="" exists=true
	// 3 name="" exists=false
}

// 案例：按下标取元素，不做提升。
//
// 与 Array()[i] 的区别是：下标越界、负数、或者节点根本不是数组，
// 都返回不存在的节点，不会凭空造出一个元素来。
func ExampleBMap_At() {
	bm := bmap.Parse(`["x","y","z"]`)

	fmt.Println(bm.At(1).String())
	fmt.Println(bm.At(9).IsExists())
	fmt.Println(bm.At(-1).IsExists())

	// 标量上 At 不做提升，这与 Array() 是刻意的区别
	fmt.Println(bmap.Parse("abc").At(0).IsExists())
	// Output:
	// y
	// false
	// false
	// false
}

// ---------------------------------------------------------------------------
// 遍历
// ---------------------------------------------------------------------------

// 案例：遍历数组或对象。
//
// 回调的键：数组上是下标的字符串，对象上是键名。回调返回 false 立即中断。
// 标量与不存在的节点不触发回调。
func ExampleBMap_Foreach() {
	bm := bmap.Parse(`{"users":[{"id":1,"name":"kk"},{"id":2,"name":"bb"}]}`)

	bm.Get("users").Foreach(func(key string, value *bmap.BMap) bool {
		fmt.Printf("%s => %d %s\n", key, value.Get("id").Int(), value.Get("name").String())
		return true
	})

	// 返回 false 可以提前中断
	n := 0
	bm.Get("users").Foreach(func(_ string, _ *bmap.BMap) bool {
		n++
		return false
	})
	fmt.Println("只跑了一次：", n)

	// 对象也能遍历，但 map 的顺序是随机的，要稳定输出用 SortedForeach
	sum := 0
	bm.Get("users").Foreach(func(_ string, v *bmap.BMap) bool {
		sum += v.Get("id").Int()
		return true
	})
	fmt.Println("id 合计：", sum)

	// 不存在的节点遍历不触发回调，也不 panic
	bmap.Parse(nil).Foreach(func(_ string, _ *bmap.BMap) bool {
		panic("不该走到这里")
	})
	fmt.Println("nil 节点遍历安全")
	// Output:
	// 0 => 1 kk
	// 1 => 2 bb
	// 只跑了一次： 1
	// id 合计： 3
	// nil 节点遍历安全
}

// 案例：按键的字典序遍历，输出稳定。
//
// Go 的 map 遍历顺序每次运行都不一样，需要稳定输出的场合
// （拼日志、生成语句、做前后对比）用它。数组本来就是有序的，行为与 Foreach 一致。
func ExampleBMap_SortedForeach() {
	bm := bmap.Parse(map[string]any{"city": "Beijing", "name": "kk", "age": 28})

	bm.SortedForeach(func(key string, value *bmap.BMap) bool {
		fmt.Printf("%s = %s\n", key, value.String())
		return true
	})

	// 返回 false 立即中断
	n := 0
	bm.SortedForeach(func(_ string, _ *bmap.BMap) bool {
		n++
		return false
	})
	fmt.Println(n)

	// 不存在的节点遍历不触发回调，也不 panic
	bmap.Parse(nil).SortedForeach(func(_ string, _ *bmap.BMap) bool {
		panic("不该走到这里")
	})
	fmt.Println("nil 节点遍历安全")
	// Output:
	// age = 28
	// city = Beijing
	// name = kk
	// 1
	// nil 节点遍历安全
}

// 案例：遍历对象与结构体。
//
// map 的遍历顺序是随机的，所以这里把结果收进一个 map 再打印
// （fmt 打印 map 时会按键排序）；要按顺序逐个处理用 SortedForeach。
func ExampleBMap_Foreach_object() {
	got := map[string]string{}
	bmap.Parse(map[string]any{"name": "kk", "age": 28}).Foreach(func(key string, value *bmap.BMap) bool {
		got[key] = value.String()
		return true
	})
	fmt.Println(got)

	// 结构体也能遍历，键名是展开之后的
	type user struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	got2 := map[string]string{}
	bmap.Parse(user{Name: "kk", Age: 9}).Foreach(func(key string, value *bmap.BMap) bool {
		got2[key] = value.String()
		return true
	})
	fmt.Println(got2)

	// 标量与不存在的节点都不触发回调
	n := 0
	for _, src := range []any{"abc", nil} {
		bmap.Parse(src).Foreach(func(_ string, _ *bmap.BMap) bool {
			n++
			return true
		})
	}
	fmt.Println(n)
	// Output:
	// map[age:28 name:kk]
	// map[age:9 name:kk]
	// 0
}

// 案例：指针容器也能遍历。
//
// 遍历前先解引用，所以 *map、*[]T 这些形态与普通容器完全一样。
func ExampleBMap_Foreach_pointer() {
	m := map[string]any{"a": 1}
	n := 0
	bmap.Parse(&m).Foreach(func(key string, value *bmap.BMap) bool {
		n += value.Int()
		return true
	})
	fmt.Println(n)

	list := []any{"x", "y"}
	bmap.Parse(&list).Foreach(func(key string, value *bmap.BMap) bool {
		fmt.Printf("%s=%s\n", key, value.String())
		return true
	})
	// Output:
	// 1
	// 0=x
	// 1=y
}

// 案例：结构体与数组上的排序遍历。
func ExampleBMap_SortedForeach_struct() {
	type user struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	bmap.Parse(user{Name: "kk", Age: 9}).SortedForeach(func(key string, value *bmap.BMap) bool {
		fmt.Printf("%s = %s\n", key, value.String())
		return true
	})

	// 数组本来就是有序的，行为与 Foreach 一致
	bmap.Parse([]any{"x", "y"}).SortedForeach(func(key string, value *bmap.BMap) bool {
		fmt.Printf("%s = %s\n", key, value.String())
		return true
	})
	// Output:
	// age = 9
	// name = kk
	// 0 = x
	// 1 = y
}

// ---------------------------------------------------------------------------
// 写入
// ---------------------------------------------------------------------------

// 案例：按路径写值，中间层不存在时自动创建。
//
// Set 返回自身，可以链式调用。写完直接 String() 就能拿到 JSON 文本。
func ExampleBMap_Set() {
	bm := bmap.Parse(map[string]any{"name": "kk"})
	bm.Set("age", 9).
		Set("profile.city", "Beijing").
		Set("tags", []string{"a", "b"})
	fmt.Println(bm.String())

	// 已有的键直接覆盖
	bm.Set("name", "bb")
	fmt.Println(bm.String())
	// Output:
	// {"age":9,"name":"kk","profile":{"city":"Beijing"},"tags":["a","b"]}
	// {"age":9,"name":"bb","profile":{"city":"Beijing"},"tags":["a","b"]}
}

// 案例：路径段是纯数字时，容器按数组处理。
//
// 长度不够会用 null 补齐到目标下标；
// 建数组还是建对象看的是「再下一段」是不是数字，所以 a.0.b 会建出一个数组。
func ExampleBMap_Set_arrayIndex() {
	bm := bmap.Parse(map[string]any{})
	bm.Set("list.2", "c")
	fmt.Println(bm.String())

	bm.Set("list.0", "a")
	fmt.Println(bm.String())

	deep := bmap.Parse(map[string]any{})
	deep.Set("a.0.b", 1)
	fmt.Println(deep.String())
	// Output:
	// {"list":[null,null,"c"]}
	// {"list":["a",null,"c"]}
	// {"a":[{"b":1}]}
}

// 案例：键名字面量以数字开头、又不希望被当成数组下标时，加 ## 前缀。
//
// 写进去的键名就是去掉前缀之后的样子，读回来用去掉前缀的键名。
func ExampleBMap_Set_hashPrefix() {
	bm := bmap.Parse(map[string]any{})
	bm.Set("##0", "v")
	bm.Set("##1day", "24h")

	fmt.Println(bm.String())
	fmt.Println(bm.Get("0").String(), bm.Get("1day").String())
	// Output:
	// {"0":"v","1day":"24h"}
	// v 24h
}

// 案例：当前值不是容器时会被提升成数组。
//
// 原值占第 0 位，再往后写；空容器不占位，
// 否则结果里会多出一个凭空的 {}。
func ExampleBMap_Set_promote() {
	bm := bmap.Parse("abc")
	bm.Set("1", "v")
	fmt.Println(bm.String())

	empty := bmap.Parse(map[string]any{})
	empty.Set("2", "v")
	fmt.Println(empty.String())

	// 写 nil 会落成一个 JSON null，而不是把键删掉
	null := bmap.Parse(map[string]any{})
	null.Set("a", nil)
	fmt.Println(null.String(), null.Get("a").IsNil())
	// Output:
	// ["abc","v"]
	// [null,null,"v"]
	// {"a":null} true
}

// 案例：当前值不是 []any 时会先转过去。
//
// 原有内容整体搬到新切片里，下标含义不变，所以可以直接往后追加或按下标改。
func ExampleBMap_Set_typedSlice() {
	bm := bmap.Parse([]int{1, 2})
	bm.Set("2", 3)
	fmt.Println(bm.String())

	bm.Set("0", 9)
	fmt.Println(bm.String())
	// Output:
	// [1,2,3]
	// [9,2,3]
}

// 案例：删掉一个键或一个数组元素。
//
// 路径写法与 Get 一致，键名里含点时用 \. 转义。
// 数组上删一个元素，后面的会往前挪。
// 要删的东西本来就不存在时什么都不做，不报错。
func ExampleBMap_Delete() {
	bm := bmap.Parse(map[string]any{"a": 1, "b": 2, "c": 3})
	bm.Delete("b")
	fmt.Println(bm.String())

	list := bmap.Parse([]any{1, 2, 3})
	list.Delete("1")
	fmt.Println(list.String())

	dotted := bmap.Parse(map[string]any{"a.b": 1, "c": 2})
	dotted.Delete(`a\.b`)
	fmt.Println(dotted.String())

	bm.Delete("nope")
	fmt.Println(bm.String())
	// Output:
	// {"a":1,"c":3}
	// [1,3]
	// {"c":2}
	// {"a":1,"c":3}
}

// 案例：复制出一个互不影响的节点。
//
// map 与切片会真的复制一份，所以在副本上 Set / Delete 不会动到源节点。
// 需要拿一份数据试几种改法、又不想影响手上那份数据时用它。
func ExampleBMap_Copy() {
	src := bmap.Parse(map[string]any{"a": 1})
	cp := src.Copy()
	cp.Set("a", 2)

	fmt.Println(src.String(), cp.String())
	// Output:
	// {"a":1} {"a":2}
}

// 案例：在结构体上写入。
//
// 写入会先按 TagName 把结构体展开成 map，再把新值写进去，
// 所以展开后的字段与新加的字段能在同一个节点上共存。
func ExampleBMap_Set_struct() {
	type user struct {
		Name string `json:"name"`
	}

	bm := bmap.Parse(user{Name: "kk"})
	bm.Set("age", 9)
	fmt.Println(bm.String())
	fmt.Println(bm.Get("name").String())
	// Output:
	// {"age":9,"name":"kk"}
	// kk
}

// 案例：删深层路径上的东西。
func ExampleBMap_Delete_nested() {
	bm := bmap.Parse(map[string]any{
		"user": map[string]any{"name": "kk", "age": 9},
		"list": []any{map[string]any{"id": 1}, map[string]any{"id": 2}},
	})

	// 进到父容器里删一个键
	bm.Delete("user.age")
	fmt.Println(bm.Get("user").String())

	// 数组里嵌的对象也能删
	bm.Delete("list.0.id")
	fmt.Println(bm.Get("list").String())

	// 删整个下标，后面的往前挪
	bm.Delete("list.0")
	fmt.Println(bm.Get("list").String())

	// 路径走不通时什么都不做，也不报错
	bm.Delete("nope.deep.path")
	fmt.Println(bm.Get("user").String())
	// Output:
	// {"name":"kk"}
	// [{},{"id":2}]
	// [{"id":2}]
	// {"name":"kk"}
}

// 案例：嵌套的容器也是各自一份。
//
// 深拷贝会一直做到叶子，所以在副本上改嵌套的对象与数组，源节点也看不到。
func ExampleBMap_Copy_deep() {
	src := bmap.Parse(map[string]any{
		"user": map[string]any{"name": "kk"},
		"list": []any{1, 2},
	})
	cp := src.Copy()

	cp.Set("user.name", "bb")
	cp.Set("list.0", 9)

	fmt.Println(src.String())
	fmt.Println(cp.String())
	// Output:
	// {"list":[1,2],"user":{"name":"kk"}}
	// {"list":[9,2],"user":{"name":"bb"}}
}

// ---------------------------------------------------------------------------
// 时间
// ---------------------------------------------------------------------------

// 案例：按常见布局解析时间。
//
// 不带时区信息的文本按本地时区解析，认不出来就是零值。
// 底层本来就是 time.Time 时直接拿来用，不走文本解析。
func ExampleBMap_Time() {
	for _, s := range []string{
		"2026-10-09 12:30:45",
		"2026-10-09",
		"2026-10-09T12:30:45Z",
		"12:30:45",
		"3:04PM",
	} {
		// 用不带时区的布局格式化，输出就不会受机器时区影响
		fmt.Println(bmap.Parse(s).Time().Format("2006-01-02 15:04:05"))
	}

	fmt.Println(bmap.Parse("abc").Time().IsZero())
	// Output:
	// 2026-10-09 12:30:45
	// 2026-10-09 00:00:00
	// 2026-10-09 12:30:45
	// 0000-01-01 12:30:45
	// 0000-01-01 15:04:00
	// true
}

// 案例：区分"时间就是零值"和"格式没认出来"。
//
// 零值时间在业务上是有意义的（表示"没有设置"），
// 光看 Time() 的结果分不清这两种情况，需要区分时用 TimeE。
func ExampleBMap_TimeE() {
	if _, err := bmap.Parse("abc").TimeE(); err != nil {
		fmt.Println("认不出格式：", err != nil)
	}

	t, err := bmap.Parse("2026-10-09 12:30:45").TimeE()
	fmt.Println(t.Format(time.DateTime), err)
	// Output:
	// 认不出格式： true
	// 2026-10-09 12:30:45 <nil>
}

// 案例：数据源的时间格式已知时，直接指定布局。
//
// 比 Time() 挨个试更快，也更明确。要 error 用 TimeLayoutE。
func ExampleBMap_TimeLayout() {
	fmt.Println(bmap.Parse("09/10/2026").TimeLayout("02/01/2006").Format(time.DateOnly))
	fmt.Println(bmap.Parse("abc").TimeLayout(time.DateTime).IsZero())

	_, err := bmap.Parse("abc").TimeLayoutE(time.DateTime)
	fmt.Println(err != nil)
	// Output:
	// 2026-10-09
	// true
	// true
}

// 案例：带引号的时间文本与已是 time.Time 的值。
//
// 数据库里存的 JSON 片段常常带着引号，解析前会先摘掉；
// 底层本来就是 time.Time 时直接拿来用，传什么布局都不影响。
func ExampleBMap_Time_quoted() {
	fmt.Println(bmap.Parse(`"2026-10-09 12:30:45"`).Time().Format(time.DateTime))
	fmt.Println(bmap.Parse(map[string]any{"t": `"2026-10-09"`}).Get("t").Time().Format(time.DateOnly))

	tm := time.Date(2026, 10, 9, 12, 30, 45, 0, time.UTC)
	nested := bmap.Parse(map[string]any{"t": tm}).Get("t")
	fmt.Println(nested.TimeLayout("完全对不上的布局").Format("2006-01-02 15:04:05"))
	// Output:
	// 2026-10-09 12:30:45
	// 2026-10-09
	// 2026-10-09 12:30:45
}

// 案例：time.Time 这类自己序列化的类型上继续往下走。
//
// 它没有"字段"的概念，展开结果是一段 RFC3339 文本而不是对象，
// 所以再往深层取就取不到东西了——但同样不会 panic，也不会遍历出任何键。
func ExampleBMap_Get_timeValue() {
	tm := time.Date(2026, 10, 9, 12, 30, 45, 0, time.UTC)
	node := bmap.Parse(map[string]any{"t": tm}).Get("t")

	fmt.Println(node.String())
	fmt.Println(node.Get("anything").IsExists())
	fmt.Println(node.Keys())

	n := 0
	node.SortedForeach(func(_ string, _ *bmap.BMap) bool {
		n++
		return true
	})
	fmt.Println(n)
	// Output:
	// 2026-10-09T12:30:45Z
	// false
	// []
	// 0
}

// 案例：整数形式的时间戳。
//
// 数据库里的 int 列常存着时间戳。Time() 不会去猜一个整数是不是时间戳
// （它在数字上返回零值），要按时间戳解释用 UnixTime / UnixMilliTime。
func ExampleBMap_UnixTime() {
	tm := bmap.Parse(1760000000).UnixTime()
	fmt.Println(tm.Unix(), tm.UTC().Format(time.DateTime))

	fmt.Println(bmap.Parse("1760000000000").UnixMilliTime().Unix())
	fmt.Println(bmap.Parse(1760000000).Time().IsZero())

	// 值为 0 时给零值时间，而不是 1970 年
	fmt.Println(bmap.Parse(0).UnixTime().IsZero())
	fmt.Println(bmap.Parse("0").UnixMilliTime().IsZero())
	// Output:
	// 1760000000 2025-10-09 08:53:20
	// 1760000000
	// true
	// true
	// true
}

// ---------------------------------------------------------------------------
// 回填到结构体
// ---------------------------------------------------------------------------

// 案例：把数据回填到结构体。
//
// Fill 走的是宽容转换：数据库回来的值什么类型都有可能，
// 字符串形态的 "42" 能填进 int，"true" 能填进 bool。
// 数据里没有的键不会动对应字段，原值保留。
func ExampleBMap_Fill() {
	type Order struct {
		ID     int      `json:"id"`
		Amount float64  `json:"amount"`
		OK     bool     `json:"ok"`
		Items  []string `json:"items"`
		User   struct {
			Name string `json:"name"`
		} `json:"user"`
	}

	var o Order
	bmap.Parse(map[string]any{
		"id":     "42",
		"amount": "19.9",
		"ok":     "true",
		"items":  []any{"a", "b"},
		"user":   map[string]any{"name": "kk"},
	}).Fill(&o)
	fmt.Printf("%d %v %v %v %s\n", o.ID, o.Amount, o.OK, o.Items, o.User.Name)

	// 数据里没有的字段保持原值，不会被清空
	o.ID = 7
	bmap.Parse(map[string]any{"ok": false}).Fill(&o)
	fmt.Println(o.ID, o.OK)
	// Output:
	// 42 19.9 true [a b] kk
	// 7 false
}

// 案例：Parse 时指定了标签，Fill 也按同一套键名回填。
func ExampleBMap_Fill_tagName() {
	type Form struct {
		Name string `json:"name" form:"f_name"`
		Age  int    `json:"age" form:"f_age"`
	}

	var f Form
	bmap.Parse(map[string]any{"f_name": "kk", "f_age": 9}, "form").Fill(&f)
	fmt.Printf("%+v\n", f)
	// Output:
	// {Name:kk Age:9}
}

// 案例：匿名嵌入的字段也能填回去。
//
// 结构体展开时，没有自己标签的匿名嵌入字段被平铺到了上一层，
// 所以回填时是对着同一份数据递归进去，而不是去找那个并不存在的键名。
func ExampleBMap_Fill_embedded() {
	type Base struct {
		ID int64 `json:"id"`
	}
	type User struct {
		Base
		Name string `json:"name"`
	}

	var u User
	bmap.Parse(map[string]any{"id": 7, "name": "kk"}).Fill(&u)
	fmt.Printf("%+v\n", u)

	// 展开再回填，应该拿回同样的数据
	var back User
	bmap.Parse(u).Fill(&back)
	fmt.Println(back.ID, back.Name)
	// Output:
	// {Base:{ID:7} Name:kk}
	// 7 kk
}

// 案例：嵌入的是指针时也能填回去。
//
// nil 的嵌入指针会先建出指向的值再填，
// 不会像 encoding/json 那样静默跳过、让字段永远是零值。
func ExampleBMap_Fill_embeddedPointer() {
	type Base struct {
		ID int64 `json:"id"`
	}
	type User struct {
		*Base
		Name string `json:"name"`
	}

	var u User
	bmap.Parse(map[string]any{"id": 7, "name": "kk"}).Fill(&u)
	fmt.Println(u.Base != nil, u.ID, u.Name)
	// Output:
	// true 7 kk
}

// 案例：走 encoding/json 的口径回填。
//
// Scan 与 Fill 的分工：Scan 严格、只认 json 标签、类型不匹配会返回 error；
// Fill 宽容、认 TagName、尽力转换。需要知道"到底填成没填成"时用 Scan。
func ExampleBMap_Scan() {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	var u User
	if err := bmap.Parse(`{"name":"kk","age":9}`).Scan(&u); err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", u)

	// 类型对不上会报错，而不是静默填一个零值
	var bad User
	fmt.Println(bmap.Parse(`{"name":"kk","age":"abc"}`).Scan(&bad) != nil)

	// 节点没有值时不报错，目标保持原样
	fmt.Println(bmap.Parse(nil).Scan(&bad))
	// Output:
	// {Name:kk Age:9}
	// true
	// <nil>
}

// 案例：Fill 能处理的各种字段类型。
//
// 指针字段会先建出指向的值再填，数据里没有的键不会凭空造一个指针；
// time.Time、map、切片、any 都能直接填；无符号字段遇到负数保持零值。
func ExampleBMap_Fill_types() {
	type Item struct {
		Ptr     *int           `json:"ptr"`
		Missing *int           `json:"missing"`
		Tm      time.Time      `json:"tm"`
		M       map[string]int `json:"m"`
		List    []int          `json:"list"`
		Any     any            `json:"any"`
		U       uint8          `json:"u"`
		Neg     uint8          `json:"neg"`
	}

	var it Item
	bmap.Parse(map[string]any{
		"ptr":  "7",
		"tm":   "2026-10-09 12:30:45",
		"m":    map[string]any{"a": "1"},
		"list": []any{"2", "3"},
		"any":  map[string]any{"k": "v"},
		"u":    9,
		"neg":  -1,
	}).Fill(&it)

	fmt.Println(*it.Ptr)
	fmt.Println(it.Missing == nil) // 数据里没有这个键，不会凭空造一个指针
	fmt.Println(it.Tm.Format(time.DateTime))
	fmt.Println(it.M, it.List, it.U, it.Neg)
	fmt.Println(it.Any)
	// Output:
	// 7
	// true
	// 2026-10-09 12:30:45
	// map[a:1] [2 3] 9 0
	// map[k:v]
}

// 案例：目标不是结构体时，整个节点就是它的值。
func ExampleBMap_Fill_scalar() {
	var n int
	bmap.Parse("42").Fill(&n)
	fmt.Println(n)

	var s string
	bmap.Parse(7).Fill(&s)
	fmt.Println(s)

	var list []int
	bmap.Parse([]any{1, 2}).Fill(&list)
	fmt.Println(list)

	var m map[string]int
	bmap.Parse(map[string]any{"a": 1}).Fill(&m)
	fmt.Println(m)

	// 传进来不是指针会 panic：非指针拿不到可设置的值，
	// 与其静默什么都不做，不如直接把问题暴露出来
	func() {
		defer func() { fmt.Println(recover() != nil) }()
		bmap.Parse(map[string]any{}).Fill(struct{ A int }{})
	}()
	// Output:
	// 42
	// 7
	// [1 2]
	// map[a:1]
	// true
}

// 案例：处理一批数据库查询结果。
//
// 驱动回来的行通常是 []map[string]any，值的具体类型随列类型而变
// （可能是 int64、[]byte、time.Time、nil……），
// 用 bmap 就不必为每种列写一套类型分支，Fill 会把它们统一转好。
func ExampleBMap_Foreach_rows() {
	rows := []map[string]any{
		{"id": int64(1), "name": []byte("kk"), "amount": "19.9", "vip": 1},
		{"id": int64(2), "name": []byte("bb"), "amount": "5.00", "vip": 0},
	}

	type Order struct {
		ID     int64   `json:"id"`
		Name   string  `json:"name"`
		Amount float64 `json:"amount"`
		VIP    bool    `json:"vip"`
	}

	bmap.Parse(rows).Foreach(func(i string, row *bmap.BMap) bool {
		var o Order
		row.Fill(&o)
		fmt.Printf("%s => %+v\n", i, o)
		return true
	})
	// Output:
	// 0 => {ID:1 Name:kk Amount:19.9 VIP:true}
	// 1 => {ID:2 Name:bb Amount:5 VIP:false}
}

// ---------------------------------------------------------------------------
// 与 encoding/json 互操作
// ---------------------------------------------------------------------------

// 案例：节点可以直接交给 json.Marshal。
//
// 输出一定是合法的 JSON：字符串会带上引号，没有值时是 null。
// 这一点与 String() 不同——String() 给的是人看的文本，字符串不带引号。
func ExampleBMap_MarshalJSON() {
	bm := bmap.Parse(`{"user":{"name":"kk","age":9}}`)

	b, err := json.Marshal(bm.Get("user"))
	fmt.Println(string(b), err)

	for _, src := range []any{"abc", 123, nil} {
		b, _ := json.Marshal(bmap.Parse(src))
		fmt.Println(string(b))
	}
	// Output:
	// {"age":9,"name":"kk"} <nil>
	// "abc"
	// 123
	// null
}

// 案例：BMap 也可以作为 json.Unmarshal 的目标。
func ExampleBMap_UnmarshalJSON() {
	var bm bmap.BMap
	if err := json.Unmarshal([]byte(`{"a":[1,2]}`), &bm); err != nil {
		panic(err)
	}

	fmt.Println(bm.Get("a.1").Int())
	fmt.Println(bm.Get("a").IsArray())
	// Output:
	// 2
	// true
}

// 案例：拿到一段合法的 JSON 原文。
//
// 要把节点原样嵌进另一段 JSON 里用 Raw；要给人看的文本用 String。
func ExampleBMap_Raw() {
	fmt.Println(bmap.Parse(map[string]any{"a": 1}).Raw())
	fmt.Println(bmap.Parse("abc").Raw())
	fmt.Println(bmap.Parse(nil).Raw())

	// 对照 String()：字符串不带引号，没有值时是空串
	fmt.Println(bmap.Parse("abc").String())
	fmt.Printf("%q\n", bmap.Parse(nil).String())
	// Output:
	// {"a":1}
	// "abc"
	// null
	// abc
	// ""
}

// 案例：知道一段文本到底是不是合法的 JSON。
//
// 取值链路上任何一段走不通都不会报错，只会得到零值，这是设计如此。
// 真正会"失败"的只有一件事：传进来的文本以 { 或 [ 开头、看着是 JSON，却解析不了。
// 此时数据不会丢，原文仍然按字符串留在节点里。
func ExampleBMap_Err() {
	// 普通字符串解析不出来是正常的，不算错误
	fmt.Println(bmap.Parse("abc").Err())

	bad := bmap.Parse(`{"a":`)
	fmt.Println(bad.Err() != nil)
	fmt.Println(bad.String()) // 原文还在
	fmt.Println(bad.IsExists())

	// 错误信息里只带原文的一小段，太长的会截断，
	// 免得把一整块脏数据打进日志
	long := `{"a":`
	for i := 0; i < 50; i++ {
		long += "xxxxxxxxxx"
	}
	fmt.Println(len(bmap.Parse(long).Err().Error()) < len(long))
	// Output:
	// <nil>
	// true
	// {"a":
	// true
	// true
}

// ---------------------------------------------------------------------------
// 结构体展开
// ---------------------------------------------------------------------------

// 案例：直接看结构体按标签展开成什么。
//
// 平时不需要自己调它——Parse 遇到结构体就会走这条路。
// 展开规则与 encoding/json 大体对齐：未导出字段与标签为 "-" 的字段跳过，
// omitempty 的零值跳过，匿名嵌入字段在没有自己的标签时平铺到上一层。
func ExampleNewStructUnpack() {
	type Base struct {
		ID int64 `json:"id"`
	}
	type User struct {
		Base
		Name string `json:"name"`
		Age  int    `json:"age,omitempty"`
		Skip string `json:"-"`
	}

	su := bmap.NewStructUnpack(User{Base: Base{ID: 7}, Name: "kk", Skip: "不会出现"})
	fmt.Println(su.Unpack())

	// 换一个标签就按另一套键名展开
	type Form struct {
		Name string `json:"name" form:"f_name"`
	}
	fmt.Println(bmap.NewStructUnpack(Form{Name: "kk"}, "form").Unpack())
	// Output:
	// map[id:7 name:kk]
	// map[f_name:kk]
}

// 案例：实现了 json.Marshaler 的类型走它自己的序列化。
//
// time.Time 就是一个典型：它自己决定输出成 RFC3339 文本。
// 这类类型没有"字段"的概念，展开结果完全由 MarshalJSON 决定。
func ExampleNewStructUnpack_marshaler() {
	tm := time.Date(2026, 10, 9, 12, 30, 45, 0, time.UTC)

	// 嵌在 map 里的 time.Time 保留原类型，取时间值不用走文本解析
	bm := bmap.Parse(map[string]any{"t": tm})
	fmt.Println(bm.Get("t").String())
	fmt.Println(bm.Get("t").Time().UTC().Format(time.DateTime))

	// 顶层就是 time.Time 时，Parse 会把它展开成 MarshalJSON 的结果
	fmt.Println(bmap.Parse(tm).String())
	fmt.Println(bmap.Parse(tm).IsString())
	// Output:
	// 2026-10-09T12:30:45Z
	// 2026-10-09 12:30:45
	// 2026-10-09T12:30:45Z
	// true
}

// examplePoint 自己决定怎么序列化：输出成一个数组而不是对象。
// 下面的案例用它来说明实现了 json.Marshaler 的类型怎么被对待。
type examplePoint struct{ X, Y int }

func (p examplePoint) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "[%d,%d]", p.X, p.Y), nil
}

// 案例：自定义了 json.Marshaler 的类型怎么被对待。
//
// 这类类型没有"字段"的概念，展开结果完全由 MarshalJSON 决定；
// 单字段定位在这种类型上拿不到东西，Get 会退回整包解析后再走路径。
func ExampleStructUnpack_customMarshaler() {
	bm := bmap.Parse(map[string]any{"p": examplePoint{X: 1, Y: 2}})

	fmt.Println(bm.Get("p").String()) // MarshalJSON 的输出
	fmt.Println(bm.Get("p.0").Int())  // 展开结果是数组，能按下标取
	fmt.Println(bm.Get("p.X").IsExists())

	// 单字段定位对这类类型不生效，返回 false
	_, ok := bmap.NewStructUnpack(examplePoint{X: 1, Y: 2}).UnpackValue("X")
	fmt.Println(ok)
	// Output:
	// [1,2]
	// 1
	// false
	// false
}

// 案例：只要结构体里的一个字段，不必把整个结构体拆成 map。
//
// 走路径取值时往往只用到其中一个字段，而拆整包的开销会随字段数增长。
// Get 在结构体上就是走这条路，所以"map 里嵌一个字段很多的结构体"也能很快。
func ExampleStructUnpack_UnpackValue() {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	su := bmap.NewStructUnpack(User{Name: "kk", Age: 9})

	v, ok := su.UnpackValue("name")
	fmt.Println(v, ok)

	// 找不到的键、被 "-" 跳过的字段、omitempty 的零值都返回 false
	_, ok = su.UnpackValue("nope")
	fmt.Println(ok)
	// Output:
	// kk true
	// false
}

// 案例：拿到结构体展开后的完整 map。
//
// 与 Unpack() 的区别是它直接给 map[string]any，不用再做一次类型断言。
func ExampleStructUnpack_Map() {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	m := bmap.NewStructUnpack(User{Name: "kk", Age: 9}).Map()
	fmt.Println(m["name"], m["age"], len(m))
	// Output:
	// kk 9 2
}
