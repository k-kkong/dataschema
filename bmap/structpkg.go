package bmap

import (
	"encoding/json"
	"reflect"

	"github.com/tidwall/gjson"
)

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

func (s *StructUnpack) Map() map[string]any {

	t := s.value.Type()
	num_field := t.NumField()

	out := make(map[string]any, num_field)
	for i := 0; i < num_field; i++ {
		field := t.Field(i)
		// 忽略未导出的字段
		if field.PkgPath != "" {
			continue
		}

		// 忽略忽略标签的字段
		if tag := field.Tag.Get(s.TagName); tag == "-" {
			continue
		}

		name := field.Name
		val := s.value.FieldByName(field.Name)

		var finalVal any
		tagName, tagOpts := parseTag(field.Tag.Get(s.TagName))
		if tagName != "" {
			name = tagName
		}

		// 如果omitempty 忽略了零值 ，并且当前值是零值，则跳过
		if tagOpts.Has("omitempty") {
			zero := reflect.Zero(val.Type()).Interface()
			current := val.Interface()
			if reflect.DeepEqual(current, zero) {
				continue
			}
		}

		finalVal = val.Interface()
		// 匿名字段，并且不是指针
		if field.Anonymous && val.Kind() != reflect.Ptr {
			// 如果写了标签，则当成字段
			if tagName == "" {
				upkv := NewStructUnpack(val.Interface(), s.TagName).Unpack()
				// 如果没有写标签，则平铺
				// 如果解析结果是map[string]any,
				if mapv, ok := upkv.(map[string]any); ok {
					for k, v := range mapv {
						out[k] = v
					}
				} else {
					out[name] = upkv
				}
			} else {
				out[name] = finalVal
			}
		} else {
			out[name] = finalVal
		}
	}

	return out
}
