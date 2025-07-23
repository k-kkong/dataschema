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

type DataUnit map[string]any

func isIntegerStr(s string) (int, bool) {
	val, err := strconv.Atoi(s)
	return val, err == nil
}

type BMap struct {
	rvalue reflect.Value
}

func Parse(data any) *BMap {
	rv := reflect.ValueOf(data)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		rv = reflect.ValueOf(NewStructsUnpack(data).Map())
	case reflect.String:
		jv := gjson.Parse(data.(string)).Value()
		rv = reflect.ValueOf(jv)
	default:
	}
	return &BMap{rvalue: rv}
}

func (bm *BMap) Get(key string) *BMap {
	paths := strings.Split(key, ".")
	curVal := bm.rvalue
	for _, p := range paths {
		if !curVal.IsValid() {
			return &BMap{rvalue: reflect.Value{}}
		}
		switch curVal.Kind() {
		case reflect.Ptr, reflect.Interface:
			curVal = curVal.Elem()
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
		default:
			return &BMap{rvalue: reflect.Value{}}
		}
	}
	return &BMap{rvalue: curVal}
}
func (bm *BMap) Set(key string, value any) *BMap {
	paths := strings.Split(key, ".")
	if len(paths) == 0 {
		return bm
	}
	// 设置值并获取结果，如果返回新值则更新
	bm.rvalue = setValue(bm.rvalue, paths, value)
	return bm
}

func setValue(target reflect.Value, paths []string, value any) reflect.Value {

	for target.Kind() == reflect.Ptr || target.Kind() == reflect.Interface {
		target = target.Elem()
	}

	// fmt.Println(target.Kind())
	// fmt.Println(target.Interface())
	// 如果是invlaid类型，需要初始化一下
	if !target.IsValid() {
		target = reflect.ValueOf(DataUnit{})
	}

	ori_target_kind := target.Kind()
	if idx, ok := isIntegerStr(paths[0]); ok {
		// 判断类型，如果不是 []any，则转换复制
		if target.Type() != reflect.TypeOf([]any{}) {
			tv := make([]any, 0, target.Len())
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

		// 判断长度
		// 如果数组长度足够，则直接设置值，否则给target填充null，直到下标idx
		if idx >= 0 && idx < target.Len() {
			target.Index(idx).Set(reflect.ValueOf(value))
		} else {
			l := target.Len()
			elemType := target.Type().Elem()
			zeroVal := reflect.Zero(elemType)
			// 填充零值直到idx位置
			for l <= idx {
				target = reflect.Append(target, zeroVal)
				l++
			}
			// 不需要 多加， idx 下标
			// target = reflect.Append(target, zeroVal)
		}
		if len(paths) == 1 {
			// 设置值
			target.Index(idx).Set(reflect.ValueOf(value))
		} else {
			next := target.Index(idx)
			nextv := setValue(next, paths[1:], value)
			target.Index(idx).Set(nextv)
		}

	} else {
		// 判断类型，如果不是 map[string]any，则转换复制
		if target.Type() != reflect.TypeOf(DataUnit{}) {
			tv := make(map[string]any)
			if ori_target_kind == reflect.Map {
				for _, key := range target.MapKeys() {
					tv[fmt.Sprint(key.Interface())] = target.MapIndex(key).Interface()
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
			nextv := setValue(next, paths[1:], value)
			target.SetMapIndex(reflect.ValueOf(paths[0]), nextv)
		}
	}
	return target
}

func (bm *BMap) Value() any {
	if !bm.rvalue.IsValid() {
		return nil
	}
	return bm.rvalue.Interface()
}

func (bm *BMap) Map() map[string]any {
	v, ok := bm.Value().(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return v
}

func (bm *BMap) String() string {
	var value string
	switch bm.rvalue.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		b, _ := json.Marshal(bm.Value())
		value = string(b)
	case reflect.String:
		value = bm.Value().(string)
	default:
		value = fmt.Sprint(bm.Value())
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

func (bm *BMap) TimeFormat(format string) time.Time {
	var value time.Time
	value, _ = time.ParseInLocation(format, bm.String(), time.Local)
	return value
}

func (bm *BMap) Time() time.Time {
	var value time.Time
	var err error
	value, err = time.Parse(time.DateTime, bm.String())
	if err != nil {
		value, err = time.Parse(time.DateOnly, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC3339, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC3339Nano, bm.String())
	}
	if err != nil {
		value, _ = time.Parse(time.TimeOnly, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.ANSIC, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.UnixDate, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RubyDate, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC822, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC822Z, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC850, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC1123, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.RFC1123Z, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.Kitchen, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.Stamp, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.StampMilli, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.StampMicro, bm.String())
	}
	if err != nil {
		value, err = time.Parse(time.StampNano, bm.String())
	}
	if err != nil {
		value, _ = time.Parse(time.Layout, bm.String())
	}
	return value
}
