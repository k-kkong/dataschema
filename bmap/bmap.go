package bmap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func isIntegerStr(s string) (int, bool) {
	val, err := strconv.Atoi(s)
	return val, err == nil
}

type BMap struct {
	rvalue  reflect.Value
	TagName string
}

// 实现json序列化方法
func (t BMap) MarshalJSON() ([]byte, error) {
	return []byte(t.String()), nil
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
	// k := rv.Kind()
	// fmt.Println(k)
	switch rv.Kind() {
	case reflect.Struct:
		unpk := NewStructUnpack(data, tagname)
		rv = reflect.ValueOf(unpk.Unpack())
	case reflect.String:
		var sv any
		if json.Unmarshal([]byte(data.(string)), &sv) == nil {
			rv = reflect.ValueOf(sv)
		}
	case reflect.Slice, reflect.Array:
		if (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) &&
			rv.Type().Elem().Kind() == reflect.Uint8 {
			// fmt.Println(string(rv.Bytes()))
			// fmt.Println(rv.Bytes())
			var sv any
			if json.Unmarshal(rv.Bytes(), &sv) == nil {
				rv = reflect.ValueOf(sv)
			}
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
	brv := bm.rvalue
	for brv.Kind() == reflect.Ptr || brv.Kind() == reflect.Interface {
		brv = brv.Elem()
	}

	var values []*BMap
	switch brv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < brv.Len(); i++ {
			values = append(values, Parse(brv.Index(i).Interface()))
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
	case reflect.Slice, reflect.Array:
		// 如果是字节流数组
		if bm.rvalue.Type().Elem().Kind() == reflect.Uint8 {
			value = string(bm.rvalue.Bytes())
		} else {
			b, _ := json.Marshal(bv)
			value = string(b)
		}
	case reflect.Map, reflect.Struct:
		b, _ := json.Marshal(bv)
		value = string(b)
	case reflect.String:
		value = bv.(string)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value = strconv.FormatInt(bm.rvalue.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value = strconv.FormatInt(int64(bm.rvalue.Uint()), 10)
	case reflect.Float32, reflect.Float64:
		value = strconv.FormatFloat(bm.rvalue.Float(), 'f', -1, 64)
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

	bv := bm.Value()
	if bv == nil {
		return 0
	}
	var value int
	switch bm.rvalue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value = int(bm.rvalue.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value = int(bm.rvalue.Uint())
	case reflect.Float32, reflect.Float64:
		value = int(bm.rvalue.Float())
	default:
		value, _ = strconv.Atoi(bm.String())
	}
	return value
}

func (bm *BMap) Float() float64 {
	bv := bm.Value()
	if bv == nil {
		return 0
	}
	var value float64
	switch bm.rvalue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value = float64(bm.rvalue.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value = float64(bm.rvalue.Uint())
	case reflect.Float32, reflect.Float64:
		value = bm.rvalue.Float()
	default:
		value, _ = strconv.ParseFloat(bm.String(), 64)
	}
	return value
}

func (bm *BMap) Int64() int64 {
	bv := bm.Value()
	if bv == nil {
		return 0
	}
	var value int64
	switch bm.rvalue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value = bm.rvalue.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value = int64(bm.rvalue.Uint())
	case reflect.Float32, reflect.Float64:
		value = int64(bm.rvalue.Float())
	default:
		value, _ = strconv.ParseInt(bm.String(), 10, 64)
	}
	return value
}

func (bm *BMap) Bool() bool {
	var value bool
	value, _ = strconv.ParseBool(bm.String())
	return value
}

func (bm *BMap) TimeLayout(layout string) time.Time {
	var value time.Time
	value, _ = time.ParseInLocation(layout, bm.String(), time.Local)
	return value
}

func (bm *BMap) Time() time.Time {
	var value time.Time
	var err error
	str := bm.String()
	value, err = time.ParseInLocation(time.DateTime, str, time.Local)
	if err != nil {
		value, err = time.ParseInLocation(time.DateOnly, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC3339, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC3339Nano, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(`2006-01-02 15:04:05 -0700 MST`, str, time.Local)
	}
	if err != nil {
		value, _ = time.ParseInLocation(time.TimeOnly, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.ANSIC, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.UnixDate, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RubyDate, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC822, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC822Z, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC850, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC1123, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.RFC1123Z, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.Kitchen, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.Stamp, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.StampMilli, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.StampMicro, str, time.Local)
	}
	if err != nil {
		value, err = time.ParseInLocation(time.StampNano, str, time.Local)
	}
	if err != nil {
		value, _ = time.ParseInLocation(time.Layout, str, time.Local)
	}
	return value
}
