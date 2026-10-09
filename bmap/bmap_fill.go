package bmap

import (
	"encoding/json"
	"reflect"
)

// Fill 把数据回填到目标，目标必须是指针。
//
// 目标是结构体时按字段逐个填，键名取自 TagName 指定的标签
// （默认 json，Parse 时指定了别的标签就按别的来），没写标签的字段用字段名。
// 目标是指向标量、切片、map 的指针时，整个节点就是它的值。
//
//	bmap.Parse(row).Fill(&user)      // row 是 map[string]any、JSON 文本或结构体都行
//
// 回填用的是 bmap 的宽容转换：字符串形态的数字能填进 int 字段，
// "true" 能填进 bool 字段。数据里没有的键不会动对应字段，原值保留。
//
// 与 Scan 的分工：Fill 认 TagName、转换宽容、失败静默；
// Scan 走 encoding/json、只认 json 标签、类型不匹配会返回 error。
func (bm *BMap) Fill(src any) {
	v := reflect.ValueOf(src)
	if v.Kind() != reflect.Ptr {
		panic("src must be a pointer to struct")
	}

	elem := v.Elem()
	if elem.Kind() == reflect.Struct {
		bm.fillStruct(elem)
		return
	}
	bm.fillField(elem)
}

// fillStruct 按目标结构体的字段逐个回填
func (bm *BMap) fillStruct(v reflect.Value) {
	t := v.Type()
	for _, fm := range structFields(t, bm.tagName()) {
		// 未导出字段拿不到可设置的值，tag 写成 "-" 的字段是明确说了不参与
		if fm.skip {
			continue
		}
		field := v.Field(fm.index)
		if !field.CanSet() {
			continue
		}

		// 匿名嵌入且没有自己的标签：展开阶段它的字段被平铺到了上一层，
		// 回填也要对着同一份数据递归进去，否则这些字段永远是零值。
		if fm.anonymous && !fm.hasTag {
			bm.fillEmbedded(field)
			continue
		}

		sub := bm.Get(fm.name)
		if !sub.IsExists() {
			continue
		}
		sub.fillField(field)
	}
}

// fillEmbedded 回填一个匿名嵌入的字段。
//
// 数据是平铺的，所以这里传下去的还是当前节点，不去找那个并不存在的键名。
// 嵌入的是指针时先把它建出来，否则字段没处可写。
func (bm *BMap) fillEmbedded(field reflect.Value) {
	switch field.Kind() {
	case reflect.Struct:
		bm.fillStruct(field)
	case reflect.Ptr:
		if field.Type().Elem().Kind() != reflect.Struct {
			return
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		bm.fillStruct(field.Elem())
	}
}

// fillField 填充一个字段，指针字段会先建出指向的值再填
func (bm *BMap) fillField(field reflect.Value) {
	if field.Kind() == reflect.Ptr {
		elemType := field.Type().Elem()
		ptr := reflect.New(elemType)
		// 指向的还是指针时由 fillField 继续往下建，类型每递归一层就少一层，不会打转
		bm.fillField(ptr.Elem())
		field.Set(ptr)
		return
	}
	bm.fillValue(field)
}

// fillValue 按目标类型把值写进去，写不了的类型直接跳过
func (bm *BMap) fillValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(bm.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(bm.Int64())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// 负数不写：无符号字段填一个负数没有意义，保持零值更容易发现问题
		if val := bm.Int64(); val >= 0 {
			v.SetUint(uint64(val))
		}
	case reflect.Float32, reflect.Float64:
		v.SetFloat(bm.Float())
	case reflect.Bool:
		v.SetBool(bm.Bool())
	case reflect.Struct:
		if v.Type() == typeTime {
			v.Set(reflect.ValueOf(bm.Time()))
			return
		}
		nested := reflect.New(v.Type())
		bm.Fill(nested.Interface())
		v.Set(nested.Elem())
	case reflect.Slice:
		if !bm.IsArray() {
			return // 不是数组就不动这个字段
		}
		elems := bm.Array()
		newSlice := reflect.MakeSlice(v.Type(), len(elems), len(elems))
		for i, itemBm := range elems {
			if itemBm == nil {
				continue
			}
			itemBm.fillField(newSlice.Index(i))
		}
		v.Set(newSlice)
	case reflect.Map:
		if !bm.IsObject() {
			return // 不是对象就不动这个字段
		}
		mapType := v.Type()
		// 只支持 map[string]T，与 JSON 的对象键限制一致
		if mapType.Key().Kind() != reflect.String {
			return
		}
		elemType := mapType.Elem()
		newMap := reflect.MakeMap(mapType)
		bm.Foreach(func(key string, value *BMap) bool {
			mapVal := reflect.New(elemType).Elem()
			value.fillField(mapVal)
			newMap.SetMapIndex(reflect.ValueOf(key).Convert(mapType.Key()), mapVal)
			return true
		})
		v.Set(newMap)
	case reflect.Interface:
		goVal := bm.Value()
		if goVal == nil {
			v.Set(reflect.Zero(v.Type()))
			return
		}
		rv := reflect.ValueOf(goVal)
		// 目标是有具体方法的接口时，值不一定满足它，硬 Set 会 panic
		if rv.Type().AssignableTo(v.Type()) {
			v.Set(rv)
		}
	default:
		// 其它类型（chan、func、复杂数……）不支持，跳过
	}
}

// tagName 结构体展开与回填用的标签名。
//
// 零值 BMap（例如 var bm BMap 之后直接 UnmarshalJSON）没有标签名，
// 这时按 json 处理，与 Parse 的默认值保持一致。
func (bm *BMap) tagName() string {
	if bm.TagName == "" {
		return "json"
	}
	return bm.TagName
}

// Scan 用 encoding/json 的口径把数据回填到目标。
//
// 与 Fill 的区别：
//   - Scan 只认 json 标签，TagName 不参与；
//   - Scan 是严格的，类型对不上会返回 error，不会尽力转换；
//   - Scan 支持 encoding/json 的全部能力（自定义 UnmarshalJSON、-,string 选项等）。
//
// 目标是指针。节点没有值时不报错，目标保持原样。
func (bm *BMap) Scan(dst any) error {
	v := bm.Value()
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
