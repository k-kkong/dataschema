package bmap

import (
	"encoding/json"
	"reflect"
)

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

// 判断类型是否实现 json.Marshaler
func implementsJSONMarshaler(t reflect.Type) bool {
	if t == nil {
		return false
	}
	jsonMarshalerType := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	return t.Implements(jsonMarshalerType) ||
		reflect.PtrTo(t).Implements(jsonMarshalerType)
}
