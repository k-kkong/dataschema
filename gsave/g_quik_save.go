package gsave

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/k-kkong/dataschema/bmap"
)

type GQuikSave struct {
	model  any
	schema map[string]reflect.Value

	base_tag string
}

func NewQuikSave(model any, tag ...string) *GQuikSave {
	g := &GQuikSave{
		model: model,
	}
	if len(tag) > 0 {
		g.base_tag = tag[0]
	} else {
		g.base_tag = "gorm"
	}

	g.fetchSchema()
	return g
}

// 解析字段到schema中
func (g *GQuikSave) fetchSchema() {
	// 获取模型的反射值和类型，处理指针和接口
	modelValue := reflect.ValueOf(g.model)
	for modelValue.Kind() == reflect.Ptr || modelValue.Kind() == reflect.Interface {
		modelValue = modelValue.Elem()
	}
	modelType := modelValue.Type()

	schema := make(map[string]reflect.Value)
	for i := 0; i < modelType.NumField(); i++ {
		fieldType := modelType.Field(i)
		fieldValue := modelValue.Field(i)

		// 跳过未导出字段
		if fieldType.PkgPath != "" {
			continue
		}

		// 根据base_tag获取字段名
		fieldName := ""
		tagValue := fieldType.Tag.Get(g.base_tag)

		// 如果标签值为"-"，忽略该字段
		if tagValue == "-" {
			continue
		}

		if g.base_tag == "gorm" && tagValue != "" {
			// 处理gorm标签格式 column:xxx
			gormTags := strings.Split(tagValue, ";")
			for _, tag := range gormTags {
				if strings.HasPrefix(tag, "column:") {
					fieldName = strings.Split(tag, "column:")[1]
					// 如果column值为"-"，忽略该字段
					if fieldName == "-" {
						fieldName = ""
						break
					}
					break
				}
			}
		} else if g.base_tag == "json" && tagValue != "" {
			// 处理json标签格式 json:"xxx"
			jsonParts := strings.Split(tagValue, ",")
			if len(jsonParts) > 0 {
				// 如果json标签主值为"-"，忽略该字段
				if jsonParts[0] == "-" {
					continue
				}
				fieldName = jsonParts[0]
			}
		}

		// 如果没有获取到标签名，使用字段名转小写下划线格式
		if fieldName == "" {
			fieldName = g.convertToSnakeCase(fieldType.Name)
		}

		// 将字段值添加到schema中
		schema[fieldName] = fieldValue
	}
	g.schema = schema
}

// convertToSnakeCase 将驼峰命名转换为小写下划线命名
func (g *GQuikSave) convertToSnakeCase(str string) string {
	var result strings.Builder
	for i, char := range str {
		if i > 0 && 'A' <= char && char <= 'Z' {
			if i > 0 && str[i-1] != '_' {
				result.WriteRune('_')
			}
			result.WriteRune(char + 32)
		} else {
			// 保留原字符（包括下划线）
			result.WriteRune(char)
		}
	}
	return result.String()
}

func (g *GQuikSave) GetUpdateMapping(src any) map[string]any {
	var mapping = map[string]any{}
	srcv := bmap.Parse(src)

	// 将 BMap 转换为 map[string]any 以便查找
	srcMap := srcv.Map()

	// 遍历 schema 中的所有字段
	for fieldName, fieldValue := range g.schema {
		// 检查源数据中是否存在该字段
		srcVal, exists := srcMap[fieldName]
		if !exists {
			continue
		}

		// 获取字段的类型
		fieldType := fieldValue.Type()
		srcValType := reflect.TypeOf(srcVal)

		// 如果类型一致，直接设置到 mapping
		if srcValType == fieldType {
			mapping[fieldName] = srcVal
			continue
		}

		// 处理类型不一致的情况，判断是否是基础类型
		var convertedVal any
		// 检查是否是 time.Time 类型
		if fieldType == reflect.TypeOf(time.Time{}) {
			// 直接使用 bmap.Time() 转换
			bm := bmap.Parse(srcVal)
			convertedVal = bm.Time()
		} else {
			switch fieldType.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				// 转换为整型
				bm := bmap.Parse(srcVal)
				if fieldType.Kind() == reflect.Int64 {
					convertedVal = bm.Int64()
				} else {
					convertedVal = bm.Int()
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				// 转换为无符号整型
				bm := bmap.Parse(srcVal)
				convertedVal = uint(bm.Int())
			case reflect.Float32, reflect.Float64:
				// 转换为浮点型
				bm := bmap.Parse(srcVal)
				convertedVal = bm.Float()
			case reflect.String:
				// 转换为字符串
				bm := bmap.Parse(srcVal)
				convertedVal = bm.String()
			case reflect.Bool:
				// 转换为布尔型
				bm := bmap.Parse(srcVal)
				convertedVal = bm.Bool()
			default:
				convertedVal = bmap.Parse(srcVal).String()
				// 非基础类型，使用 JSON 解析的方式处理
				// 创建目标类型的新实例
				// targetVal := reflect.New(fieldType).Elem()
				// 将源值转换为 JSON 字符串
				// jsonBytes, _ := json.Marshal(srcVal)
				// 解析 JSON 到目标类型
				// if err := json.Unmarshal(jsonBytes, targetVal.Addr().Interface()); err == nil {
				// 	convertedVal = targetVal.Interface()
				// } else {
				// 	// 如果 JSON 解析失败，尝试直接赋值
				// 	if targetVal.CanSet() {
				// 		srcValRef := reflect.ValueOf(srcVal)
				// 		if srcValRef.Type().AssignableTo(fieldType) {
				// 			targetVal.Set(srcValRef)
				// 			convertedVal = targetVal.Interface()
				// 		}
				// 	}
				// }
			}
		}

		// 如果成功转换，添加到 mapping
		if convertedVal != nil {
			mapping[fieldName] = convertedVal
		}
	}

	return mapping
}

func (g *GQuikSave) FillWithJson(src any) error {
	json.Unmarshal([]byte(bmap.Parse(src).String()), g.model)
	return nil
}
