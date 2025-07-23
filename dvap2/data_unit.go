package dvap2

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func isIntegerStr(s string) (int, bool) {
	val, err := strconv.Atoi(s)
	return val, err == nil
}

type DataUnit struct {
	rvalue reflect.Value
}

func NewDataUnit(data any) *DataUnit {
	return &DataUnit{rvalue: reflect.ValueOf(data)}
}

func (dm *DataUnit) Get(key string) *DataUnit {
	paths := strings.Split(key, ".")
	curVal := dm.rvalue
	for _, p := range paths {
		if !curVal.IsValid() {
			return &DataUnit{rvalue: reflect.Value{}}
		}
		switch curVal.Kind() {
		case reflect.Ptr, reflect.Interface:
			curVal = curVal.Elem()
		}
		switch curVal.Kind() {
		case reflect.Map:
			mv := curVal.MapIndex(reflect.ValueOf(p))
			if !mv.IsValid() {
				return &DataUnit{rvalue: reflect.Value{}}
			}
			curVal = mv
		case reflect.Slice, reflect.Array:
			if idx, ok := isIntegerStr(p); ok {
				if idx >= 0 && idx < curVal.Len() {
					curVal = curVal.Index(idx)
				} else {
					return &DataUnit{rvalue: reflect.Value{}}
				}
			} else {
				return &DataUnit{rvalue: reflect.Value{}}
			}
		default:
			return &DataUnit{rvalue: reflect.Value{}}
		}
	}
	return &DataUnit{rvalue: curVal}
}
func (dm *DataUnit) Set(key string, value any) *DataUnit {
	paths := strings.Split(key, ".")
	if len(paths) == 0 {
		return dm
	}
	// 设置值并获取结果，如果返回新值则更新
	dm.rvalue = setValue(dm.rvalue, paths, value)
	return dm
}

func setValue(target reflect.Value, paths []string, value any) reflect.Value {

	for target.Kind() == reflect.Ptr || target.Kind() == reflect.Interface {
		target = target.Elem()
	}

	// fmt.Println(target.Kind())
	// fmt.Println(target.Interface())
	// 如果是invlaid类型，需要初始化一下
	if !target.IsValid() {
		target = reflect.ValueOf(map[string]any{})
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
		if target.Type() != reflect.TypeOf(map[string]any{}) {
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

func (dm *DataUnit) Interface() interface{} {
	if !dm.rvalue.IsValid() {
		return nil
	}
	return dm.rvalue.Interface()
}
