package bmap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	typeMapStringAny = reflect.TypeOf(map[string]any{})
	typeSliceAny     = reflect.TypeOf([]any{})
	typeString       = reflect.TypeOf("")
	typeTime         = reflect.TypeOf(time.Time{})
)

func isIntegerStr(s string) (int, bool) {
	val, err := strconv.Atoi(s)
	return val, err == nil
}

// deref 把指针与接口一路解到底。
//
// 取值器和判定函数都要先做这一步：不解引用的话 Kind() 拿到的是 Ptr 或 Interface，
// 落不进任何业务分支，表现出来就是「明明有值却什么都取不到」。
// 解到底之后可能变成无效值（nil 指针、装着 nil 的接口），调用方还要再判一次 IsValid。
func deref(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		v = v.Elem()
	}
	return v
}

// isNilValue 判断一个值是不是 nil。
//
// reflect.Value.IsNil() 只对 chan/func/interface/map/ptr/slice 有意义，
// 对 int、string 这类值调用会直接 panic，所以要先看 Kind。
func isNilValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// isEmptyContainer 判断值是不是一个空容器。
//
// Set 把当前值提升成数组时要用它：空 map、空切片不该被塞进下标 0，
// 否则会在结果里留下一个凭空多出来的 {} 或 []。
func isEmptyContainer(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return v.Len() == 0
	}
	return false
}

// bytesOf 取出字节流的底层字节，数组会先复制一份。
//
// reflect.Value.Bytes() 只对切片和「可寻址的数组」有效，
// 直接对 [3]byte 这样的值调用会 panic；从未导出字段拿到的值同样调不了。
func bytesOf(v reflect.Value) ([]byte, bool) {
	if !v.IsValid() || !v.CanInterface() || v.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	switch v.Kind() {
	case reflect.Slice:
		return v.Bytes(), true
	case reflect.Array:
		out := make([]byte, v.Len())
		for i := range out {
			out[i] = byte(v.Index(i).Uint())
		}
		return out, true
	}
	return nil, false
}

// mapKeyValue 把路径段转成 map 的键类型。
//
// map 的键不一定是 string：yaml 解析出来的是 map[any]any，
// 有些模型还会用 string 的命名类型。转不过去就返回无效值，表示「这个 map 上查不了」。
// 这里不依赖 reflect 的隐式转换——string 与 int 之间的转换规则太容易出意外。
//
// map[string]X 走不到这里，mapIndex 已经把它拦在前一层了。
func mapKeyValue(m reflect.Value, key string) reflect.Value {
	kt := m.Type().Key()
	v := reflect.ValueOf(key)
	switch {
	case kt.Kind() == reflect.String:
		return v.Convert(kt)
	case v.Type().AssignableTo(kt):
		return v
	}
	return reflect.Value{}
}

// mapIndex 在 map 上按路径段取值，取不到返回无效值。
//
// map[string]X 是压倒多数的形态，先把它单拎出来直接查：
// 转键类型看着只多一步，但 reflect 的 Convert 带着完整的类型检查，
// 在逐段取值的循环里开销相当可观。
func mapIndex(m reflect.Value, key string) reflect.Value {
	t := m.Type()
	if t == typeMapStringAny || t.Key() == typeString {
		return m.MapIndex(reflect.ValueOf(key))
	} else if k := mapKeyValue(m, key); k.IsValid() {
		return m.MapIndex(k)
	}
	return reflect.Value{}
}

// looksLikeJSON 判断一段文本是不是想当 JSON 用。
//
// 只有以 { 或 [ 开头的文本解析失败才算错误：
// "abc" 解析不出来是正常的，它本来就是一个普通字符串。
func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// splitPath 按 . 把路径切成分段，键名里本身含点时用 \. 转义（与 gjson 一致）。
//
// 返回值里不保留转义用的反斜杠：`a\.b` 切成 ["a.b"]，`a\\b` 切成 [`a\b`]。
// 空路径切出一个空段，因为空字符串本身就是一个合法的键名；
// 末尾的 . 也会切出一个空段，两种情况都与「按键名逐段查找」的语义一致。
func splitPath(key string) []string {
	if strings.IndexByte(key, '\\') < 0 {
		// 绝大多数路径不带转义，直接切，省掉逐字符扫描
		return strings.Split(key, ".")
	}
	out := make([]string, 0, strings.Count(key, ".")+1)
	var sb strings.Builder
	for i := 0; i < len(key); i++ {
		switch key[i] {
		case '\\':
			if i+1 < len(key) {
				i++
			}
			sb.WriteByte(key[i])
		case '.':
			out = append(out, sb.String())
			sb.Reset()
		default:
			sb.WriteByte(key[i])
		}
	}
	return append(out, sb.String())
}

// unescape 去掉一个路径段里用于转义的反斜杠
func unescape(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// BMap 一份数据的惰性视图。
//
// 用 Parse 造出来，之后按路径取值；取不到时拿到的是「不存在的节点」而不是 nil，
// 所以整条链可以一直点下去，不需要层层判空。
type BMap struct {
	rvalue  reflect.Value
	TagName string

	// badJSON 表示 Parse 时那段文本看着是 JSON 却解析不了，用 Err() 取详情。
	//
	// 存一个 bool 而不是 error：error 是两个字节宽的接口，
	// 会让每个节点的分配从 48 字节涨到 64 字节，
	// 而节点是这个包里分配最频繁的东西。
	badJSON bool
}

// MarshalJSON 让 BMap 可以直接交给 json.Marshal。
//
// 输出一定是合法的 JSON：字符串会带上引号，没有值时输出 null。
// json 包会校验 MarshalJSON 的返回值，吐出不合法的片段会让整个 Marshal 失败，
// 所以这里不能直接把 String() 的结果交出去——String() 给的是「人看的文本」，
// 字符串是不带引号的。
func (t BMap) MarshalJSON() ([]byte, error) {
	v := t.Value()
	if v == nil {
		return []byte("null"), nil
	}
	if b, err := json.Marshal(v); err == nil {
		return b, nil
	}
	// 底层值里有 json 处理不了的东西（chan、func、循环引用……），
	// 退一步给一个带引号的字符串，至少不让整个 Marshal 失败。
	return []byte(strconv.Quote(t.String())), nil
}

// UnmarshalJSON 让 BMap 可以直接作为 json.Unmarshal 的目标。
func (t *BMap) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	tag := t.TagName
	if tag == "" {
		tag = "json"
	}
	*t = *Parse(v, tag)
	return nil
}

// Parse 把任意形态的数据包成一个可以按路径取值的节点。
//
// 接受的形态：map、切片、结构体（及其指针）、JSON 文本（string 或 []byte）、
// 以及任意标量。结构体会按 TagName 展开成 map，JSON 文本会先解析成结构。
//
// opts 的第一个元素是结构体展开用的 tag 名，默认 json：
//
//	bmap.Parse(form)          // 按 `json:"..."` 展开
//	bmap.Parse(form, "form")  // 按 `form:"..."` 展开
//
// Parse 不会失败也不会 panic：解析不出来的文本按普通字符串保留，
// nil 变成一个不存在的节点。要确认「这段文本是不是合法 JSON」用 Error()。
//
// 传入 *BMap 时原样返回，所以链式调用里重复 Parse 不会套娃。
func Parse(data any, opts ...string) *BMap {
	if val, ok := data.(*BMap); ok {
		return val
	}

	tagname := "json"
	if len(opts) > 0 && opts[0] != "" {
		tagname = opts[0]
	}

	rv := reflect.ValueOf(data)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}

	var badJSON bool
	switch rv.Kind() {
	case reflect.Struct:
		if rv.CanInterface() {
			rv = reflect.ValueOf(NewStructUnpack(rv.Interface(), tagname).Unpack())
		}
	case reflect.String:
		// 字符串可能是 JSON 文本，先试着解析；解析不出来就按普通字符串保留。
		// 这里用 rv.String() 而不是 data.(string)：后者遇到
		// type MyJSON string 这样的命名类型会直接 panic。
		text := rv.String()
		var sv any
		if json.Unmarshal([]byte(text), &sv) == nil {
			rv = reflect.ValueOf(sv)
		} else if looksLikeJSON(text) {
			badJSON = true
		}
	case reflect.Slice, reflect.Array:
		if b, ok := bytesOf(rv); ok {
			var sv any
			if json.Unmarshal(b, &sv) == nil {
				rv = reflect.ValueOf(sv)
			} else if looksLikeJSON(string(b)) {
				badJSON = true
			}
		}
	}

	return &BMap{rvalue: rv, TagName: tagname, badJSON: badJSON}
}

// missing 造一个「不存在」的节点。
//
// 链式取值走不通时统一返回它，调用方拿到的永远是可用的 *BMap，不需要判 nil；
// TagName 要带上，这样后续在这个节点上继续取值时口径不会变。
func (bm *BMap) missing() *BMap {
	return &BMap{TagName: bm.TagName}
}

// Get 按路径取值，路径用 . 分段，数组用下标。
//
//	bm.Get("data.list.0.user.name")
//
// 键名里本身含点时用反斜杠转义（与 gjson 一致），取字面键名为 a.b 的那一项：
//
//	bm.Get(`a\.b`)
//
// 任何一段走不通都返回不存在的节点，不会 panic；
// 在这样的节点上继续取值，拿到的都是各类型的零值。
func (bm *BMap) Get(key string) *BMap {
	// 只有一段的普通键名是最常见的形态，直接查一次：
	// 不切分、不分配，逐层取值的开销才不会被路径解析本身掩盖。
	// 这里用两次 IndexByte 而不是 ContainsAny：后者对短字符串会走逐 rune 解码的分支，
	// 比两次 memchr 慢一个量级。
	dot := strings.IndexByte(key, '.')
	esc := strings.IndexByte(key, '\\')
	if dot < 0 && esc < 0 {
		cur, ok := bm.step(bm.rvalue, key)
		if !ok {
			return bm.missing()
		}
		return &BMap{rvalue: cur, TagName: bm.TagName}
	}

	// 多段路径：按 . 逐段扫，被 \ 转义的字符不参与分段。
	// 不先切出 []string 再遍历——逐段取值本来就用不到完整的分段清单，
	// 白分配一个切片在循环取值的场合里很显眼。
	hasEsc := esc >= 0
	cur := bm.rvalue
	start := 0
	for i := 0; i <= len(key); i++ {
		if i < len(key) {
			if key[i] == '\\' && i+1 < len(key) {
				i++ // 连反斜杠带它后面那个字符一起跳过
				continue
			}
			if key[i] != '.' {
				continue
			}
		}
		seg := key[start:i]
		if hasEsc {
			seg = unescape(seg)
		}
		next, ok := bm.step(cur, seg)
		if !ok {
			return bm.missing()
		}
		cur = next
		start = i + 1
	}
	return &BMap{rvalue: cur, TagName: bm.TagName}
}

// step 从 cur 往下走一段，走不通返回 false。
//
// map 上取一个键、切片上取一个下标，这两条路径占绝大多数，
// 所以它们就地展开，省掉一层函数调用。
func (bm *BMap) step(cur reflect.Value, p string) (reflect.Value, bool) {
	k := cur.Kind()
	if k == reflect.Interface || k == reflect.Ptr {
		cur = deref(cur)
		k = cur.Kind()
	}
	// 零值 Value 上调 CanInterface 会 panic，所以先看 IsValid
	if !cur.IsValid() || !cur.CanInterface() {
		return reflect.Value{}, false
	}

	switch k {
	case reflect.Map:
		if t := cur.Type(); t == typeMapStringAny || t.Key() == typeString {
			mv := cur.MapIndex(reflect.ValueOf(p))
			return mv, mv.IsValid()
		}
		mv := mapIndex(cur, p)
		return mv, mv.IsValid()
	case reflect.Slice, reflect.Array:
		// 数字段在切片上才是下标，在 map 上就是普通键名
		idx, ok := isIntegerStr(p)
		if !ok || idx < 0 || idx >= cur.Len() {
			return reflect.Value{}, false
		}
		return cur.Index(idx), true
	case reflect.Struct:
		return bm.structValue(cur, p)
	}
	return reflect.Value{}, false
}

// structValue 从结构体上取一个字段。
//
// 优先走单字段定位：走路径时往往只要其中一个字段，
// 把整个结构体拆成 map 的开销会随字段数增长，一次定位则不会。
// 单字段没命中时再退回整包展开——实现了 json.Marshaler 的类型没有字段概念，
// 只能靠它自己序列化出来的结果。
func (bm *BMap) structValue(cur reflect.Value, p string) (reflect.Value, bool) {
	su := NewStructUnpack(cur.Interface(), bm.TagName)
	if v, ok := su.UnpackValue(p); ok {
		return reflect.ValueOf(v), true
	}

	unpacked := deref(reflect.ValueOf(su.Unpack()))
	if !unpacked.IsValid() {
		return reflect.Value{}, false
	}
	switch unpacked.Kind() {
	case reflect.Map:
		mv := mapIndex(unpacked, p)
		if !mv.IsValid() {
			return reflect.Value{}, false
		}
		return mv, true
	case reflect.Slice, reflect.Array:
		idx, ok := isIntegerStr(p)
		if !ok || idx < 0 || idx >= unpacked.Len() {
			return reflect.Value{}, false
		}
		return unpacked.Index(idx), true
	}
	return reflect.Value{}, false
}

// IsExists 节点上有没有值。
//
// 用来区分「值就是零」和「路径根本不存在」：
// 两种情况下 String()/Int() 都返回零值，光看取值结果分不出来。
func (bm *BMap) IsExists() bool {
	return bm.rvalue.IsValid()
}

// Set 按路径写值，中间层不存在时自动创建。
//
// 路径段是纯数字时容器按数组处理，长度不够会用 null 补齐到目标下标：
//
//	bmap.Parse(map[string]any{}).Set("list.2", "v")   // {"list":[null,null,"v"]}
//
// 键名字面量以数字开头、不希望被当成数组下标时，加 ## 前缀：
//
//	bmap.Parse(map[string]any{}).Set("##0", "v")      // {"0":"v"}
//
// 当前值不是容器时会被提升成数组，原值占第 0 位（空容器不占位）：
//
//	bmap.Parse("abc").Set("1", "v")                   // ["abc","v"]
//
// 返回自身，可以链式调用。
func (bm *BMap) Set(key string, value any) *BMap {
	paths := splitPath(key)
	if len(paths) == 0 {
		return bm
	}
	if value == nil {
		// 存一个类型化的 nil：reflect.ValueOf(nil) 是无效值，
		// 用它去 SetMapIndex 会变成「删除这个键」而不是「写入 null」
		value = new(any)
	}
	bm.rvalue = bm.setValue(bm.rvalue, paths, value)
	return bm
}

func (bm *BMap) setValue(target reflect.Value, paths []string, value any) reflect.Value {
	target = deref(target)

	// 没有容器就从空对象开始建
	if !target.IsValid() {
		target = reflect.ValueOf(map[string]any{})
	}

	oriTargetKind := target.Kind()
	if idx, ok := isIntegerStr(paths[0]); ok {
		// 当前不是 []any 就先转成 []any，原有内容整体搬过去
		if target.Type() != typeSliceAny {
			ltv := 1
			if oriTargetKind == reflect.Slice || oriTargetKind == reflect.Array {
				ltv = target.Len()
			}
			tv := make([]any, 0, ltv)
			switch oriTargetKind {
			case reflect.Slice, reflect.Array:
				for i := 0; i < target.Len(); i++ {
					tv = append(tv, target.Index(i).Interface())
				}
			default:
				// 标量提升成数组：原值占第 0 位。
				// 空容器不占位，否则下标 0 上会多出一个凭空的 {} 或 []。
				if !isEmptyContainer(target) {
					tv = append(tv, target.Interface())
				}
			}
			target = reflect.ValueOf(tv)
		}

		// 长度不够就用零值补到目标下标
		for l := target.Len(); l <= idx; l++ {
			target = reflect.Append(target, reflect.Zero(target.Type().Elem()))
		}

		if len(paths) == 1 {
			target.Index(idx).Set(reflect.ValueOf(value))
		} else {
			next := bm.setValue(target.Index(idx), paths[1:], value)
			target.Index(idx).Set(next)
		}
		return target
	}

	// 当前不是 map[string]any 就先转过去
	if target.Type() != typeMapStringAny {
		tv := make(map[string]any)
		switch oriTargetKind {
		case reflect.Map:
			iter := target.MapRange()
			for iter.Next() {
				tv[fmt.Sprint(iter.Key().Interface())] = iter.Value().Interface()
			}
		case reflect.Struct:
			if unpv, ok := NewStructUnpack(target.Interface(), bm.TagName).Unpack().(map[string]any); ok {
				tv = unpv
			}
		}
		target = reflect.ValueOf(tv)
	}

	p0 := strings.TrimPrefix(paths[0], "##")

	if len(paths) == 1 {
		target.SetMapIndex(reflect.ValueOf(p0), reflect.ValueOf(value))
		return target
	}

	next := target.MapIndex(reflect.ValueOf(p0))
	if !next.IsValid() || isNilValue(next) {
		// 下一级不存在，建数组还是建对象看的是「再下一段」是不是数字，
		// 看当前段没有意义——能走到这个分支就说明当前段不是数字。
		if _, ok := isIntegerStr(paths[1]); ok {
			next = reflect.ValueOf(make([]any, 0))
		} else {
			next = reflect.ValueOf(make(map[string]any))
		}
	}
	target.SetMapIndex(reflect.ValueOf(p0), bm.setValue(next, paths[1:], value))
	return target
}

// Value 给出底层值，没有值时返回 nil。
//
// 指针与接口会一路解到底，所以 *int、any 里装的东西拿到的都是最终那个值。
func (bm *BMap) Value() any {
	v := deref(bm.rvalue)
	if !v.IsValid() || !v.CanInterface() {
		return nil
	}
	return v.Interface()
}

// Map 给出对象形态的数据。
//
// 不是对象时返回一个空 map 而不是 nil，调用方可以直接 range，不用判空。
// 键不是 string 的 map（例如 yaml 解析出来的 map[any]any）会把键转成字符串；
// 结构体按 TagName 展开。
func (bm *BMap) Map() map[string]any {
	v := deref(bm.rvalue)
	if !v.IsValid() || !v.CanInterface() {
		return map[string]any{}
	}
	switch v.Kind() {
	case reflect.Map:
		// 已经是 map[string]any 就直接给出去，不额外复制一份
		if m, ok := v.Interface().(map[string]any); ok {
			return m
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			if iter.Key().CanInterface() && iter.Value().CanInterface() {
				out[fmt.Sprint(iter.Key().Interface())] = iter.Value().Interface()
			}
		}
		return out
	case reflect.Struct:
		if m, ok := NewStructUnpack(v.Interface(), bm.TagName).Unpack().(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

// IsArray 底层是不是数组（切片或数组）
func (bm *BMap) IsArray() bool {
	v := deref(bm.rvalue)
	return v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array)
}

// IsNil 底层是不是没有值
func (bm *BMap) IsNil() bool {
	return bm.Value() == nil
}

// IsObject 底层是不是对象（map 或结构体）
func (bm *BMap) IsObject() bool {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return false
	}
	k := v.Kind()
	return k == reflect.Map || k == reflect.Struct
}

// Array 把内容当数组看，给出逐元素的节点。
//
// 这里的语义是「把内容转成数组，然后看有几个」，所以它和 gjson 的 Array() 不一样，
// 不是「只有 JSON 数组才展开」：任何不是数组的值都会被当成长度 1 的数组，
// 元素就是它自己；不存在的节点同样返回长度 1，元素是不存在的节点。
//
//	bmap.Parse([]any{1, 2}).Array()   // 长度 2
//	bmap.Parse("abc").Array()         // 长度 1，元素是 "abc"
//	bmap.Parse(nil).Array()           // 长度 1，元素 IsExists() 为 false
//
// 这样做的用处是：接口有时返回一条记录、有时返回一批记录，
// 调用方可以统一写成 for _, item := range bm.Get(x).Array()，
// 不用先判断对面到底是单个对象还是数组。
//
// 代价是标量上也会跑一次循环。要区分「真数组」和「被提升的标量」，先用 IsArray()。
//
// 元素走的是 Parse，所以字符串形态的 JSON 会被展开成结构，
// 结构体元素会按 TagName 展开。
func (bm *BMap) Array() []*BMap {
	v := deref(bm.rvalue)
	if v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
		n := v.Len()
		values := make([]*BMap, n)
		for i := 0; i < n; i++ {
			values[i] = bm.child(v.Index(i))
		}
		return values
	}
	return []*BMap{bm}
}

// child 用当前节点的 TagName 造一个子节点。
//
// 字符串、字节流和结构体仍然走 Parse：字符串可能是 JSON 文本、结构体要按 tag 展开，
// 这两种能力不能丢。其余类型直接建节点，省掉一次装箱与类型分派。
func (bm *BMap) child(v reflect.Value) *BMap {
	d := deref(v)
	if !d.IsValid() || !d.CanInterface() {
		return bm.missing()
	}
	switch d.Kind() {
	case reflect.String:
		return Parse(d.String(), bm.TagName)
	case reflect.Struct:
		return Parse(d.Interface(), bm.TagName)
	case reflect.Slice, reflect.Array:
		if b, ok := bytesOf(d); ok {
			return Parse(b, bm.TagName)
		}
	}
	return &BMap{rvalue: d, TagName: bm.TagName}
}

// Foreach 遍历对象的每个键、或数组的每个下标。
//
// 回调的键：对象上是键名，数组上是下标的字符串形式。
// 回调返回 false 立即中断。标量与不存在的节点不触发回调。
//
// map 的遍历顺序是随机的，要稳定的输出用 SortedForeach。
func (bm *BMap) Foreach(f func(key string, value *BMap) bool) {
	v := deref(bm.rvalue)
	if !v.IsValid() || !v.CanInterface() {
		return
	}
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if !f(strconv.Itoa(i), bm.child(v.Index(i))) {
				return
			}
		}
	case reflect.Map:
		for _, keyv := range v.MapKeys() {
			// 值直接从 map 里取，不能再走一遍 Get(key)：
			// 键名里含点时 Get 会把它当成路径，取到的是空值。
			if !f(fmt.Sprint(keyv.Interface()), bm.child(v.MapIndex(keyv))) {
				return
			}
		}
	case reflect.Struct:
		if unpk, ok := NewStructUnpack(v.Interface(), bm.TagName).Unpack().(map[string]any); ok {
			for k, val := range unpk {
				if !f(k, Parse(val, bm.TagName)) {
					return
				}
			}
		}
	}
}

// String 给出值的文本形态。
//
// 对象与数组输出 JSON 文本，字符串原样返回（不带引号），
// 数字按最短写法输出，time.Time 输出不带引号的 RFC3339，没有值时返回空串。
func (bm *BMap) String() string {
	v := deref(bm.rvalue)
	if !v.IsValid() || !v.CanInterface() {
		return ""
	}

	// 标量直接用 v.String()/v.Int() 这些取值方法，不走 Interface()：
	// Interface() 会把值装进一个 any，对字符串与数字而言那就是一次白白分配的装箱。
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case reflect.Slice, reflect.Array:
		// 字节流按文本看，它承载的通常是一段字符串而不是一串数字
		if b, ok := bytesOf(v); ok {
			return string(b)
		}
		return marshalOrSprint(v.Interface())
	case reflect.Struct:
		// time.Time 序列化出来是一段带引号的 JSON 字符串，
		// 而顶层 Parse(time.Time) 拿到的已经是不带引号的文本，两边必须一致。
		if v.Type() == typeTime {
			return v.Interface().(time.Time).Format(time.RFC3339)
		}
		return marshalOrSprint(v.Interface())
	case reflect.Map:
		return marshalOrSprint(v.Interface())
	default:
		// 布尔、chan、func 这些剩下的形态，给一个能看出是什么的文本
		return fmt.Sprint(v.Interface())
	}
}

// marshalOrSprint 把值序列化成 JSON 文本，序列化不了时退回 fmt.Sprint。
//
// 底层值里可能有 chan、func、循环引用这些 json 处理不了的东西，
// 这种情况给一个「至少能看出是什么」的文本，而不是一个静默的空串——
// 空串会让人分不清是「值就是空」还是「序列化失败了」。
func marshalOrSprint(v any) string {
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

// Int 给出 int 形态的值，转不出来就是 0
func (bm *BMap) Int() int {
	return int(bm.Int64())
}

// Int64 给出 int64 形态的值，转不出来就是 0。
//
// 数字字符串会被解析，浮点数朝零截断（1.9 -> 1），布尔值给 1 或 0，
// 口径与 gjson 一致。解析不出来不报错，返回 0。
func (bm *BMap) Int64() int64 {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return 0
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return int64(v.Float())
	case reflect.Bool:
		if v.Bool() {
			return 1
		}
		return 0
	default:
		n, _ := strconv.ParseInt(bm.String(), 10, 64)
		return n
	}
}

// Int32 给出 int32 形态的值，超出范围会截断
func (bm *BMap) Int32() int32 {
	return int32(bm.Int64())
}

// Float 给出 float64 形态的值，转不出来就是 0
func (bm *BMap) Float() float64 {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return 0
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Bool:
		if v.Bool() {
			return 1
		}
		return 0
	default:
		f, _ := strconv.ParseFloat(bm.String(), 64)
		return f
	}
}

// Float32 给出 float32 形态的值
func (bm *BMap) Float32() float32 {
	return float32(bm.Float())
}

// Float64 给出 float64 形态的值，与 Float() 等价
func (bm *BMap) Float64() float64 {
	return bm.Float()
}

// Uint 给出 uint64 形态的值，负数与转不出来的情况都是 0
func (bm *BMap) Uint() uint64 {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return 0
	}
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		if n < 0 {
			return 0
		}
		return uint64(n)
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if f < 0 {
			return 0
		}
		return uint64(f)
	case reflect.Bool:
		if v.Bool() {
			return 1
		}
		return 0
	default:
		s := bm.String()
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
			return uint64(f)
		}
		return 0
	}
}

// Bool 给出布尔形态的值。
//
// strconv.ParseBool 认得的写法优先，所以 "true"/"TRUE"/"t"/"1"/"false"/"F"/"0"
// 这些的结果与 strconv 完全一致。
//
// 认不出来的按 gjson 的口径兜底：数字非零为真，字符串非空且不是 "0" 为真，
// 对象与数组一律为真（有内容就算存在），null 与不存在的节点为假。
// 兜底只作用在 strconv 本来就会失败、只能返回 false 的输入上，
// 例如 0.5、"yes"、{}；这些输入光看结果分不出是真值还是转换失败。
func (bm *BMap) Bool() bool {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return false
	}

	// 布尔与数字直接看值，不绕文本（绕一圈要先 Format 再 ParseBool，白花两道工）
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return v.Float() != 0
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		// 有内容就算真，与 gjson 对 JSON 值的口径一致
		return true
	}

	// 文本形态：strconv.ParseBool 认得的写法优先，保证既有口径不变
	s := bm.String()
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	return s != "" && s != "0"
}
