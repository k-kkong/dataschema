package bmap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func isIntegerStr(s string) (int, bool) {
	val, err := strconv.Atoi(s)
	return val, err == nil
}

type BMap struct {
	rvalue  reflect.Value
	TagName string
}

func Parse(data any, opts ...string) *BMap {
	var tagname = "json"
	if len(opts) > 0 {
		tagname = opts[0]
	}

	rv := reflect.ValueOf(data)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		unpk := NewStructUnpack(data, tagname)
		rv = reflect.ValueOf(unpk.Unpack())
	case reflect.String:
		jv := gjson.Parse(data.(string)).Value()
		if jv != nil {
			rv = reflect.ValueOf(jv)
		}
	default:
	}
	return &BMap{
		rvalue:  rv,
		TagName: tagname,
	}
}

func (bm *BMap) Get(key string) *BMap {
	paths := strings.Split(key, ".")
	curVal := bm.rvalue
	for _, p := range paths {

		for curVal.Kind() == reflect.Ptr || curVal.Kind() == reflect.Interface {
			curVal = curVal.Elem()
		}
		if !curVal.IsValid() {
			return &BMap{rvalue: reflect.Value{}}
		}

		switch curVal.Kind() {
		case reflect.Map:
			mv := curVal.MapIndex(reflect.ValueOf(p))
			if !mv.IsValid() {
				return &BMap{rvalue: reflect.Value{}}
			}
			curVal = mv
		case reflect.Slice, reflect.Array:
			if idx, ok := isIntegerStr(p); ok {
				if idx >= 0 && idx < curVal.Len() {
					curVal = curVal.Index(idx)
				} else {
					return &BMap{rvalue: reflect.Value{}}
				}
			} else {
				return &BMap{rvalue: reflect.Value{}}
			}
		case reflect.Struct:
			unpkg := NewStructUnpack(curVal.Interface(), bm.TagName).Unpack()
			unpkgv := reflect.ValueOf(unpkg)
			for unpkgv.Kind() == reflect.Ptr || unpkgv.Kind() == reflect.Interface {
				unpkgv = unpkgv.Elem()
			}
			switch unpkgv.Kind() {
			case reflect.Map:
				curVal = unpkgv.MapIndex(reflect.ValueOf(p))
			case reflect.Slice, reflect.Array:
				if idx, ok := isIntegerStr(p); ok {
					if idx >= 0 && idx < unpkgv.Len() {
						curVal = unpkgv.Index(idx)
					} else {
						return &BMap{rvalue: reflect.Value{}}
					}
				} else {
					return &BMap{rvalue: reflect.Value{}}
				}
			default:
				return &BMap{rvalue: reflect.Value{}}
			}

		default:
			return &BMap{rvalue: reflect.Value{}}
		}
	}
	return &BMap{rvalue: curVal}
}

func (bm *BMap) IsExists() bool {
	return bm.rvalue.IsValid()
}

func (bm *BMap) Set(key string, value any) *BMap {
	paths := strings.Split(key, ".")
	if len(paths) == 0 {
		return bm
	}
	if value == nil {
		value = new(any)
	}
	// 设置值并获取结果，如果返回新值则更新
	bm.rvalue = bm.setValue(bm.rvalue, paths, value)
	return bm
}

func (bm *BMap) setValue(target reflect.Value, paths []string, value any) reflect.Value {

	for target.Kind() == reflect.Ptr || target.Kind() == reflect.Interface {
		target = target.Elem()
	}

	// fmt.Println(target.Kind())
	// fmt.Println(target.Interface())
	// 如果是invlaid类型，需要初始化一下
	if !target.IsValid() {
		target = reflect.ValueOf(map[string]any{})
	}
	// fmt.Println(target.Interface())

	ori_target_kind := target.Kind()
	if idx, ok := isIntegerStr(paths[0]); ok {
		// 判断类型，如果不是 []any，则转换复制
		if target.Type() != reflect.TypeOf([]any{}) {

			var ltv = 1
			if ori_target_kind == reflect.Slice {
				ltv = target.Len()
			}
			tv := make([]any, 0, ltv)
			// 如果是数据切片，则将数据复制到新的切片中，否则将其作为第一个元素
			switch ori_target_kind {
			case reflect.Slice, reflect.Array:
				for i := 0; i < target.Len(); i++ {
					tv = append(tv, target.Index(i).Interface())
				}
			default:
				tv = append(tv, target.Interface())
			}
			target = reflect.ValueOf(tv)
		}

		// 判断长度 如果数组长度不够，需要填充null值到idx
		if idx >= 0 && idx < target.Len() {
			// target.Index(idx).Set(reflect.ValueOf(value))
		} else {
			l := target.Len()
			elemType := target.Type().Elem()
			zeroVal := reflect.Zero(elemType)
			// 填充零值直到idx位置
			for l <= idx {
				target = reflect.Append(target, zeroVal)
				l++
			}
			// target.Index(idx).Set(reflect.ValueOf(value))
		}
		if len(paths) == 1 {
			target.Index(idx).Set(reflect.ValueOf(value))
		} else {
			next := target.Index(idx)
			// fmt.Println(next.Interface())
			nextv := bm.setValue(next, paths[1:], value)
			target.Index(idx).Set(nextv)
		}

	} else {
		if target.Type() != reflect.TypeOf(map[string]any{}) {
			tv := make(map[string]any)

			// 只有map或者slice类型，才需要复制数据
			switch ori_target_kind {
			case reflect.Map:
				for _, key := range target.MapKeys() {
					tv[fmt.Sprint(key.Interface())] = target.MapIndex(key).Interface()
				}
			case reflect.Struct:
				// reflect.Slice, reflect.Array
				unpv := NewStructUnpack(target.Interface(), bm.TagName).Unpack()
				var suc bool
				tv, suc = unpv.(map[string]any)
				if !suc {
					tv = map[string]any{}
				}
			}

			target = reflect.ValueOf(tv)
		}

		if len(paths) == 1 {
			target.SetMapIndex(reflect.ValueOf(paths[0]), reflect.ValueOf(value))
		} else {
			next := target.MapIndex(reflect.ValueOf(paths[0]))
			if !next.IsValid() || next.IsNil() {
				// 如果下一级是 nil，则根据path创建新的 map 或 slice
				if _, ok := isIntegerStr(paths[0]); ok {
					tv := make([]any, 0)
					next = reflect.ValueOf(tv)
				} else {
					tv := make(map[string]any)
					next = reflect.ValueOf(tv)
				}
			}
			nextv := bm.setValue(next, paths[1:], value)
			target.SetMapIndex(reflect.ValueOf(paths[0]), nextv)
		}
	}
	return target
}

func (bm *BMap) Value() any {
	if !bm.rvalue.IsValid() {
		return nil
	}

	for bm.rvalue.Kind() == reflect.Ptr || bm.rvalue.Kind() == reflect.Interface {
		bm.rvalue = bm.rvalue.Elem()
	}

	if bm.rvalue.IsValid() {
		return bm.rvalue.Interface()
	}
	return nil

}

func (bm *BMap) Map() map[string]any {
	v, ok := bm.Value().(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return v
}

func (bm *BMap) IsArray() bool {
	return bm.rvalue.Kind() == reflect.Slice || bm.rvalue.Kind() == reflect.Array
}
func (bm *BMap) IsNil() bool {
	return bm.Value() == nil
}

func (bm *BMap) Array() []*BMap {
	var values []*BMap
	switch bm.rvalue.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < bm.rvalue.Len(); i++ {
			values = append(values, Parse(bm.rvalue.Index(i).Interface()))
		}
	default:
		values = append(values, bm)
	}
	return values
}

func (bm *BMap) String() string {
	bv := bm.Value()
	if bv == nil {
		return ""
	}
	var value string
	switch bm.rvalue.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		b, _ := json.Marshal(bv)
		value = string(b)
	case reflect.String:
		value = bv.(string)
	default:
		if t, ok := bv.(time.Time); ok {
			value = t.Format(time.RFC3339)
		} else {
			value = fmt.Sprint(bv)
		}
	}
	return value
}

func (bm *BMap) Int() int {
	var value int
	value, _ = strconv.Atoi(bm.String())
	return value
}

func (bm *BMap) Float() float64 {
	var value float64
	value, _ = strconv.ParseFloat(bm.String(), 64)
	return value
}

func (bm *BMap) Int64() int64 {
	var value int64
	value, _ = strconv.ParseInt(bm.String(), 10, 64)
	return value
}

func (bm *BMap) Bool() bool {
	var value bool
	value, _ = strconv.ParseBool(bm.String())
	return value
}

func (bm *BMap) TimeLayout(layout string) time.Time {
	var value time.Time
	value, _ = time.Parse(layout, bm.String())
	return value
}

func (bm *BMap) Time() time.Time {
	var value time.Time
	var err error
	str := bm.String()
	value, err = time.Parse(time.DateTime, str)
	if err != nil {
		value, err = time.Parse(time.DateOnly, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC3339, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC3339Nano, str)
	}
	if err != nil {
		value, err = time.Parse(`2006-01-02 15:04:05 -0700 MST`, str)
	}
	if err != nil {
		value, _ = time.Parse(time.TimeOnly, str)
	}
	if err != nil {
		value, err = time.Parse(time.ANSIC, str)
	}
	if err != nil {
		value, err = time.Parse(time.UnixDate, str)
	}
	if err != nil {
		value, err = time.Parse(time.RubyDate, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC822, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC822Z, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC850, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC1123, str)
	}
	if err != nil {
		value, err = time.Parse(time.RFC1123Z, str)
	}
	if err != nil {
		value, err = time.Parse(time.Kitchen, str)
	}
	if err != nil {
		value, err = time.Parse(time.Stamp, str)
	}
	if err != nil {
		value, err = time.Parse(time.StampMilli, str)
	}
	if err != nil {
		value, err = time.Parse(time.StampMicro, str)
	}
	if err != nil {
		value, err = time.Parse(time.StampNano, str)
	}
	if err != nil {
		value, _ = time.Parse(time.Layout, str)
	}
	return value
}
