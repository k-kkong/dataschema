package bmap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

// ValueType 节点的底层形态。
//
// 取值前先看清楚是什么形态，比取完再猜要省事：
// 例如「有值」和「值是零」在 Int() 上都是 0，但 Type() 分得开。
type ValueType int

const (
	// TypeNull 没有值：节点不存在，或者值就是 null
	TypeNull ValueType = iota
	// TypeString 字符串
	TypeString
	// TypeNumber 数字，含整数、无符号整数与浮点
	TypeNumber
	// TypeBool 布尔
	TypeBool
	// TypeObject 对象，含 map 与结构体
	TypeObject
	// TypeArray 数组，含切片与数组
	TypeArray
)

// String 给出形态的可读名称，打日志和报错时用
func (v ValueType) String() string {
	switch v {
	case TypeNull:
		return "null"
	case TypeString:
		return "string"
	case TypeNumber:
		return "number"
	case TypeBool:
		return "bool"
	case TypeObject:
		return "object"
	case TypeArray:
		return "array"
	}
	return "unknown"
}

// Type 给出节点的底层形态。
//
// 不存在的节点与 null 都是 TypeNull，要区分「路径走不通」用 IsExists()。
// 字节流按字符串算——它承载的通常是一段文本，不是一串数字。
func (bm *BMap) Type() ValueType {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return TypeNull
	}
	switch v.Kind() {
	case reflect.String:
		return TypeString
	case reflect.Bool:
		return TypeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return TypeNumber
	case reflect.Map, reflect.Struct:
		return TypeObject
	case reflect.Slice, reflect.Array:
		if _, ok := bytesOf(v); ok {
			return TypeString
		}
		return TypeArray
	}
	return TypeNull
}

// IsString 底层是不是字符串
func (bm *BMap) IsString() bool { return bm.Type() == TypeString }

// IsNumber 底层是不是数字
func (bm *BMap) IsNumber() bool { return bm.Type() == TypeNumber }

// IsBool 底层是不是布尔
func (bm *BMap) IsBool() bool { return bm.Type() == TypeBool }

// Len 给出元素个数：数组是长度，对象是键数，其它一律 0。
//
// 注意它和 Array() 不是一回事。Array() 的语义是「转成数组看有几个」，
// 标量也会被提升成长度 1 的切片；Len() 只回答「这里到底有几个元素」，
// 标量上是 0。要判断有没有内容用 Len，要统一遍历用 Array。
func (bm *BMap) Len() int {
	v := deref(bm.rvalue)
	if !v.IsValid() {
		return 0
	}
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return v.Len()
	case reflect.Struct:
		// 结构体没有「元素个数」的概念，按展开后的字段数算，与 Keys() 保持一致
		if m, ok := NewStructUnpack(v.Interface(), bm.tagName()).Unpack().(map[string]any); ok {
			return len(m)
		}
	}
	return 0
}

// Keys 给出对象的键、或数组的下标字符串，结果按字典序排好。
//
// 排序是为了让输出稳定：Go 的 map 遍历顺序每次都不同，
// 拼日志、生成 SQL、做快照对比这类场合需要一个确定的顺序。
// 标量与不存在的节点返回空切片。
func (bm *BMap) Keys() []string {
	v := deref(bm.rvalue)
	if !v.IsValid() || !v.CanInterface() {
		return nil
	}

	var keys []string
	switch v.Kind() {
	case reflect.Map:
		keys = make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, fmt.Sprint(k.Interface()))
		}
	case reflect.Slice, reflect.Array:
		keys = make([]string, v.Len())
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}
		return keys // 下标本来就是有序的，不用再排
	case reflect.Struct:
		if m, ok := NewStructUnpack(v.Interface(), bm.tagName()).Unpack().(map[string]any); ok {
			keys = make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
		}
	default:
		return nil
	}

	sort.Strings(keys)
	return keys
}

// At 按数组下标取元素。
//
// 与 Array()[i] 的区别是它不做提升：下标越界、负数、
// 或者节点根本不是数组，都返回不存在的节点，不会凭空造出一个元素来。
func (bm *BMap) At(i int) *BMap {
	v := deref(bm.rvalue)
	if !v.IsValid() || i < 0 {
		return bm.missing()
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		if i >= v.Len() {
			return bm.missing()
		}
		return bm.child(v.Index(i))
	}
	return bm.missing()
}

// SortedForeach 与 Foreach 一样遍历，但按键的字典序进行。
//
// Go 的 map 遍历顺序每次运行都不一样，需要稳定输出的场合
// （拼日志、生成语句、做前后对比）用它。数组本来就是有序的，行为与 Foreach 一致。
func (bm *BMap) SortedForeach(f func(key string, value *BMap) bool) {
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
		// 先把键和值一起收下来再排序：
		// 排序之后不能用 Get(key) 回查，键名里含点时会被当成路径。
		values := make(map[string]reflect.Value, v.Len())
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			s := fmt.Sprint(k.Interface())
			keys = append(keys, s)
			values[s] = v.MapIndex(k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !f(k, bm.child(values[k])) {
				return
			}
		}
	case reflect.Struct:
		m, ok := NewStructUnpack(v.Interface(), bm.tagName()).Unpack().(map[string]any)
		if !ok {
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !f(k, Parse(m[k], bm.tagName())) {
				return
			}
		}
	}
}

// Delete 删掉一个键或一个数组元素。
//
// 路径写法与 Get 一致，键名里含点时用 \. 转义。
// 最后一段落在对象上就删这个键，落在数组上就把那个元素去掉、后面的往前挪。
//
// 路径走不通、或者要删的东西本来就不存在时什么都不做，不报错——
// 删除是一个「让它不在」的操作，本来就不在也算达到了目的。
func (bm *BMap) Delete(key string) *BMap {
	paths := splitPath(key)
	if len(paths) == 0 {
		return bm
	}
	bm.rvalue = bm.deleteAt(bm.rvalue, paths)
	return bm
}

func (bm *BMap) deleteAt(target reflect.Value, paths []string) reflect.Value {
	target = deref(target)
	if !target.IsValid() || len(paths) == 0 {
		return target
	}

	// 最后一段：真正执行删除
	if len(paths) == 1 {
		switch target.Kind() {
		case reflect.Map:
			if k := mapKeyValue(target, paths[0]); k.IsValid() {
				// 传一个无效值给 SetMapIndex 就是删除这个键
				target.SetMapIndex(k, reflect.Value{})
			}
		case reflect.Slice:
			idx, ok := isIntegerStr(paths[0])
			if !ok || idx < 0 || idx >= target.Len() {
				return target
			}
			out := reflect.MakeSlice(target.Type(), 0, target.Len()-1)
			out = reflect.AppendSlice(out, target.Slice(0, idx))
			return reflect.AppendSlice(out, target.Slice(idx+1, target.Len()))
		}
		return target
	}

	// 还有更深的层级：先进到下一层，删完把结果写回当前层。
	// map 是引用类型，子容器已经就地改掉了，写回只是为了覆盖「切片删元素换了底层值」这种情况。
	switch target.Kind() {
	case reflect.Map:
		k := mapKeyValue(target, paths[0])
		if !k.IsValid() {
			return target
		}
		next := target.MapIndex(k)
		if !next.IsValid() {
			return target
		}
		target.SetMapIndex(k, bm.deleteAt(next, paths[1:]))
	case reflect.Slice:
		idx, ok := isIntegerStr(paths[0])
		if !ok || idx < 0 || idx >= target.Len() {
			return target
		}
		next := target.Index(idx)
		if newNext := bm.deleteAt(next, paths[1:]); newNext.IsValid() && next.CanSet() {
			next.Set(newNext)
		}
	}
	return target
}

// Raw 给出节点的 JSON 原文。
//
// 与 String() 的区别是 Raw 一定是一段合法的 JSON：
// 字符串会带上引号，没有值时是 null。要把节点原样嵌进另一段 JSON 里用它。
func (bm *BMap) Raw() string {
	v := bm.Value()
	if v == nil {
		return "null"
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return strconv.Quote(bm.String())
}

// Err 给出 Parse 阶段的解析失败原因，没有问题时返回 nil。
//
// 只有一种情况会有值：传进来的文本以 { 或 [ 开头、看起来是 JSON，却解析不了。
// 普通字符串解析不出来是正常的，不算错误。
//
// 数据不会因为解析失败而丢失，原文仍然按字符串留在节点里，
// 所以这个方法用来回答「这段文本到底是不是一份合法的 JSON」。
//
// 方法名是 Err 而不是 Error：叫 Error 会让 *BMap 实现 error 接口，
// 那样 fmt 打印节点时会走这个方法而不是打出内容，排查问题时反而看不清。
func (bm *BMap) Err() error {
	if !bm.badJSON {
		return nil
	}
	return fmt.Errorf("数据看着是 JSON 但解析不了：%s", textSnippet(bm.String(), 120))
}

// textSnippet 截一段文本放进错误信息，避免一个几十 KB 的响应体把日志刷满
func textSnippet(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Copy 复制出一个互不影响的节点。
//
// map 与切片会真的复制一份，所以在副本上 Set / Delete 不会动到源节点。
// 需要拿一份数据试几种改法、又不想影响手上那份数据时用它。
func (bm *BMap) Copy() *BMap {
	return &BMap{
		rvalue:  reflect.ValueOf(deepCopy(bm.Value())),
		TagName: bm.TagName,
		badJSON: bm.badJSON,
	}
}

// deepCopy 递归复制 map 与切片，其它类型原样返回。
//
// 标量与结构体本来就是按值传递的，复制了也是同一份内容，不用管；
// 只有 map 和切片是引用语义，不复制的话两个节点会指向同一块底层数据。
func deepCopy(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if !rv.CanInterface() {
		return v
	}

	switch rv.Kind() {
	case reflect.Map:
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			if !iter.Key().CanInterface() || !iter.Value().CanInterface() {
				continue
			}
			out.SetMapIndex(iter.Key(), reflect.ValueOf(deepCopy(iter.Value().Interface())))
		}
		return out.Interface()
	case reflect.Slice:
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			c := deepCopy(rv.Index(i).Interface())
			if c == nil {
				continue
			}
			if cv := reflect.ValueOf(c); cv.Type().AssignableTo(rv.Type().Elem()) {
				out.Index(i).Set(cv)
			}
		}
		return out.Interface()
	}
	return v
}

// StringOr 给出字符串形态的值，节点不存在时用调用方给的默认值。
//
// 与 String() 的区别是它分得清「值就是空串」和「路径走不通」：
// 前者返回空串，后者返回默认值。
func (bm *BMap) StringOr(def string) string {
	if !bm.IsExists() {
		return def
	}
	return bm.String()
}

// IntOr 给出 int 形态的值，节点不存在时用调用方给的默认值
func (bm *BMap) IntOr(def int) int {
	if !bm.IsExists() {
		return def
	}
	return bm.Int()
}

// FloatOr 给出 float64 形态的值，节点不存在时用调用方给的默认值
func (bm *BMap) FloatOr(def float64) float64 {
	if !bm.IsExists() {
		return def
	}
	return bm.Float()
}

// BoolOr 给出布尔形态的值，节点不存在时用调用方给的默认值
func (bm *BMap) BoolOr(def bool) bool {
	if !bm.IsExists() {
		return def
	}
	return bm.Bool()
}

// ValueOr 给出底层值，节点不存在时用调用方给的默认值
func (bm *BMap) ValueOr(def any) any {
	if v := bm.Value(); v != nil {
		return v
	}
	return def
}
