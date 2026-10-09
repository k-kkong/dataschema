package bmap

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
)

// jsonMarshalerType json.Marshaler 的反射类型，包级复用避免每次调用都重新构造
var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// fieldCacheKey 字段元信息缓存的键。
// 同一个结构体可以用不同的 tag 名解析（json / form / gorm ...），
// 所以缓存必须把 tag 名一起算进键里，否则会串味。
type fieldCacheKey struct {
	typ reflect.Type
	tag string
}

// fieldMeta 一个结构体字段解析出来的元信息。
//
// 反射解析 tag 的成本不低，而同一个结构体往往要被反复解析
// （逐行处理查询结果时尤其明显），所以按类型缓存一次，之后直接查表。
type fieldMeta struct {
	name      string // 对外使用的键名：写了 tag 用 tag 名，否则用字段名
	index     int    // 字段下标，用 Field(index) 取值，避免 FieldByName 的线性查找
	omitEmpty bool
	anonymous bool
	skip      bool // 未导出字段，或 tag 写成 "-"
	hasTag    bool // 是否显式写了 tag 名（匿名字段要靠它决定平铺还是当成一个字段）
}

// structFieldCache 结构体字段元信息缓存：reflect.Type + tag 名 -> 字段清单
var structFieldCache sync.Map

// structFields 返回一个结构体在指定 tag 下的字段元信息，结果带缓存。
//
// 返回的清单里可能含 skip=true 的项，调用方自己跳过；
// 保留它们是为了让 index 与 reflect 的字段下标严格对应。
func structFields(t reflect.Type, tagName string) []fieldMeta {
	key := fieldCacheKey{typ: t, tag: tagName}
	if cached, ok := structFieldCache.Load(key); ok {
		return cached.([]fieldMeta)
	}

	n := t.NumField()
	fields := make([]fieldMeta, 0, n)
	for i := 0; i < n; i++ {
		sf := t.Field(i)
		fm := fieldMeta{
			name:      sf.Name,
			index:     i,
			anonymous: sf.Anonymous,
		}
		// 未导出字段无法取值，直接标记跳过
		if sf.PkgPath != "" {
			fm.skip = true
			fields = append(fields, fm)
			continue
		}
		raw := sf.Tag.Get(tagName)
		if raw == "-" {
			fm.skip = true
			fields = append(fields, fm)
			continue
		}
		name, opts := parseTag(raw)
		if name != "" {
			fm.name = name
			fm.hasTag = true
		}
		fm.omitEmpty = opts.Has("omitempty")
		fields = append(fields, fm)
	}

	structFieldCache.Store(key, fields)
	return fields
}

// maxEmbedDepth 定位字段时往匿名嵌入里递归的最大层数。
// Go 不允许循环嵌入，正常代码到不了这个深度，
// 设一个上限只是为了万一遇到奇怪的嵌套时不会一路走下去。
const maxEmbedDepth = 8

// fieldLocation 一个对外键名在结构体里的位置。
//
// 匿名嵌入且没有自己标签的字段是平铺展开的，
// 所以位置可能是一条穿过若干层嵌入字段的下标链，而不只是一个下标。
type fieldLocation struct {
	path      []int
	omitEmpty bool
}

// locateField 按对外键名定位字段，认得的键名是 Map() 展开结果的超集
// （超集的具体范围见 UnpackValue 的说明）。
//
// 顶层找不到时会往匿名嵌入的字段里递归找：那些字段在展开时是被平铺上来的，
// 这里不跟着找就会出现「Map() 里有这个键、单字段定位却说没有」的不一致。
// 先在顶层扫完再往嵌入里递归，同名字段以浅的一层为准，与 Go 自己的字段提升规则一致。
func locateField(t reflect.Type, tagName, name string, depth int) *fieldLocation {
	if depth > maxEmbedDepth || t == nil || t.Kind() != reflect.Struct {
		return nil
	}

	fields := structFields(t, tagName)
	embedded := make([]int, 0, 2)
	for i := range fields {
		f := &fields[i]
		if f.skip {
			continue
		}
		if f.name == name {
			return &fieldLocation{path: []int{f.index}, omitEmpty: f.omitEmpty}
		}
		if f.anonymous && !f.hasTag {
			embedded = append(embedded, f.index)
		}
	}

	for _, idx := range embedded {
		ft := t.Field(idx).Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if sub := locateField(ft, tagName, name, depth+1); sub != nil {
			return &fieldLocation{
				path:      append([]int{idx}, sub.path...),
				omitEmpty: sub.omitEmpty,
			}
		}
	}
	return nil
}

type StructUnpack struct {
	value   reflect.Value
	TagName string
}

func NewStructUnpack(s any, opts ...string) *StructUnpack {
	var tagname = "json"
	if len(opts) > 0 {
		tagname = opts[0]
	}
	return &StructUnpack{
		value:   strctVal(s),
		TagName: tagname,
	}
}

// 结构体解析 得到的结果是map[string]any 或者 array
func (s *StructUnpack) Unpack() any {
	t := s.value.Type()
	if implementsJSONMarshaler(t) {
		b, err := json.Marshal(s.value.Interface())
		if err != nil {
			return nil
		}
		return gjson.ParseBytes(b).Value()
	}
	return s.Map()
}

// UnpackValue 按对外键名取出结构体里的一个字段值。
//
// 与 Unpack 的区别是它不会把整个结构体拆成 map：
// 走路径取值时往往只要其中一个字段，拆整包的开销会随着字段数增长，
// 而这里是一次定位。
//
// 认得的键名是 Map() 展开结果的超集：Map() 里有的键这里一定认得，
// 包括匿名嵌入被平铺上来的那些；此外还认嵌入字段自己的名字，
// 取到的是整个被嵌入的结构体（Map() 把这一层平铺掉了，所以那里没有这个键）。
// 取超集而不是完全对齐，是为了不缩小能取到的范围——
// Get 在结构体上走的就是这条路，少认一个键名就是一个取值失败。
//
// 被 omitempty 跳过的零值字段同样返回找不到，与 Map() 一致。
// 实现 json.Marshaler 的类型没有字段概念，仍然退回整包解析。
func (s *StructUnpack) UnpackValue(name string) (any, bool) {
	t := s.value.Type()
	if implementsJSONMarshaler(t) {
		unpacked := s.Unpack()
		if m, ok := unpacked.(map[string]any); ok {
			v, ok := m[name]
			return v, ok
		}
		return nil, false
	}

	loc := locateField(t, s.TagName, name, 0)
	if loc == nil {
		return nil, false
	}

	v := s.value
	for _, idx := range loc.path {
		// 嵌入的可能是指针，nil 指针走不下去
		v = deref(v)
		if !v.IsValid() || v.Kind() != reflect.Struct || idx >= v.NumField() {
			return nil, false
		}
		v = v.Field(idx)
	}
	if !v.IsValid() || !v.CanInterface() {
		return nil, false
	}
	// omitempty 的零值字段在展开时是不出现的，这里也要保持一致
	if loc.omitEmpty && v.IsZero() {
		return nil, false
	}
	return v.Interface(), true
}

func (s *StructUnpack) Map() map[string]any {
	t := s.value.Type()
	fields := structFields(t, s.TagName)

	out := make(map[string]any, len(fields))
	for i := range fields {
		fm := fields[i]
		if fm.skip {
			continue
		}
		// 用 Field(index) 而不是 FieldByName：字段下标就在手上，
		// FieldByName 还要再做一次按名字的线性查找，字段越多越慢
		val := s.value.Field(fm.index)

		// 如果omitempty 忽略了零值 ，并且当前值是零值，则跳过
		if fm.omitEmpty && val.IsZero() {
			continue
		}

		finalVal := val.Interface()
		// 匿名字段，并且不是指针
		if fm.anonymous && val.Kind() != reflect.Ptr {
			// 如果写了标签，则当成字段
			if !fm.hasTag {
				upkv := NewStructUnpack(finalVal, s.TagName).Unpack()
				// 如果没有写标签，则平铺
				// 如果解析结果是map[string]any,
				if mapv, ok := upkv.(map[string]any); ok {
					for k, v := range mapv {
						out[k] = v
					}
					continue
				}
				out[fm.name] = upkv
				continue
			}
			out[fm.name] = finalVal
			continue
		}
		out[fm.name] = finalVal
	}

	return out
}

// implementsJSONMarshaler 判断类型是否实现 json.Marshaler
func implementsJSONMarshaler(t reflect.Type) bool {
	if t == nil {
		return false
	}
	return t.Implements(jsonMarshalerType) ||
		reflect.PtrTo(t).Implements(jsonMarshalerType)
}

// strctVal 取出结构体的可反射值，指针会一路解到底
func strctVal(s interface{}) reflect.Value {
	v := reflect.ValueOf(s)

	// 如果是指针，获取指针指向的值
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		panic("not struct")
	}

	return v
}

// trimQuotes 去掉字符串两端的引号。
// 有些来源（例如数据库里存的 JSON 片段）会带着引号，解析时间与布尔时要先摘掉。
func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
