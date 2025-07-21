package dvap

import (
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type CompareFun func(p, s gjson.Result) bool

// HasMany 准备废弃，请使用替代方法 NewDataer().HasMany
func HasMany(input interface{}, subGroup interface{}, relation string, f CompareFun) (interface{}, error) {

	input_v := VtoJson(input)
	sub_g_v := VtoJson(subGroup)

	if !sub_g_v.IsArray() {
		// return input, fmt.Errorf("拆分数据必须是一个切片")
		sub_g_v = gjson.Parse("[]")
	}

	var result []interface{}

	if input_v.IsArray() {
		for _, iv := range input_v.Array() {

			// fmt.Println(sub_g_v.String())
			// break
			// var remove []gjson.Result
			// var filter []gjson.Result
			// FilterStructSlice(sub_g_v.Array(), &remove, &filter, func(sv gjson.Result) bool {
			// 	return f(iv, sv)
			// })
			filter := NewSlicer(sub_g_v.Array()).
				Find(func(v gjson.Result) bool {
					return f(input_v, v)
				}).Data()
			// fmt.Println(len(filter))
			result = append(result, VSetV(iv, JArrToInterface(filter), relation).Value())
		}

		var ret interface{} = result
		return ret, nil
	} else {
		// var remove []gjson.Result
		// var filter []gjson.Result
		// FilterStructSlice(sub_g_v.Array(), &remove, &filter, func(sv gjson.Result) bool {
		// 	return f(input_v, sv)
		// })
		filter := NewSlicer(sub_g_v.Array()).
			Find(func(v gjson.Result) bool {
				return f(input_v, v)
			}).Data()

		return VSetV(input_v, JArrToInterface(filter), relation).Value(), nil
	}

}

// HasOne 准备废弃，请使用替代方法 NewDataer().HasOne
func HasOne(input interface{}, subGroup interface{}, relation string, f CompareFun) (interface{}, error) {
	input_v := VtoJson(input)
	sub_g_v := VtoJson(subGroup)

	if !sub_g_v.IsArray() {
		// return input, fmt.Errorf("拆分数据必须是一个切片")
		sub_g_v = gjson.Parse("[]")

	}

	var result []interface{}

	if input_v.IsArray() {
		for _, iv := range input_v.Array() {

			// fmt.Println(sub_g_v.String())
			// break
			// var remove []gjson.Result
			// var filter []gjson.Result
			// helper_value.FilterStructSlice(sub_g_v.Array(), &remove, &filter, func(sv gjson.Result) bool {
			// 	return f(iv, sv)
			// })

			var match_v gjson.Result
			SliceFind(sub_g_v.Array(), &match_v, func(sv gjson.Result) bool {
				return f(iv, sv)
			})
			result = append(result, VSetV(iv, match_v.Value(), relation).Value())
			// fmt.Println(len(filter))
		}

		var ret interface{} = result
		return ret, nil
	} else {
		// var remove []gjson.Result
		// var filter []gjson.Result
		// helper_value.FilterStructSlice(sub_g_v.Array(), &remove, &filter, func(sv gjson.Result) bool {
		// 	return f(input_v, sv)
		// })

		var match_v gjson.Result
		SliceFind(sub_g_v.Array(), &match_v, func(sv gjson.Result) bool {
			return f(input_v, sv)
		})
		return VSetV(input_v, match_v.Value(), relation).Value(), nil

	}
}

func BelongTo(input interface{}, subgroup interface{}, relation string) interface{} {
	jv := VtoJson(input)
	jv = VSetV(jv, subgroup, relation)
	return jv.Value()
}

// GetUpdateMapping  根据model的有效数据库字段解析reqdata参数，返回用于更新数据库的mapping
func GetUpdateMapping(model interface{}, reqdata gjson.Result) (map[string]interface{}, error) {

	useMapping, err := SerializeGormTagToJSON(model)
	if err != nil {
		return nil, err
	}
	// fmt.Println(useMapping)
	var mapping = map[string]interface{}{}
	gjson.Parse(useMapping).ForEach(func(key, value gjson.Result) bool {
		if reqdata.Get(key.String()).Exists() {

			_strv := reqdata.Get(key.String()).String()
			if strings.Contains(value.String(), "*") {
				if _strv == "" || _strv == "null" {
					mapping[key.String()] = nil
				}
			}
			// fmt.Println()
			v := strings.ReplaceAll(value.String(), "*", "")
			switch v {
			case "int", "int64":
				mapping[key.String()] = reqdata.Get(key.String()).Int()
			case "float", "float64", "float32":
				mapping[key.String()] = reqdata.Get(key.String()).Float()
			case "string", "models.JSONStringColumn":
				mapping[key.String()] = reqdata.Get(key.String()).String()
			case "models.JSONTime", "time.Time", "gorm.DeletedAt", "gorm.UpdatedAt", "gorm.CreatedAt":
				tv := StringToDate(_strv)
				if Validate(tv) {
					mapping[key.String()] = tv
				}
			default:
				mapping[key.String()] = reqdata.Get(key.String()).String()
			}
		}
		// fmt.Println(value.String())
		return true
	})
	return mapping, nil
}

// GetCreateMapping 根据model的有效数据库字段解析reqdata参数，返回用于创建数据库的mapping, 默认值初始化
func GetCreateMapping(model interface{}, reqdata gjson.Result) (map[string]interface{}, error) {

	useMapping, err := SerializeGormTagToJSON(model)
	if err != nil {
		return nil, err
	}
	// fmt.Println(useMapping)
	var mapping = map[string]interface{}{}
	gjson.Parse(useMapping).ForEach(func(key, value gjson.Result) bool {
		if key.String() == "id" {
			return true
		}
		if reqdata.Get(key.String()).Exists() {
			_strv := reqdata.Get(key.String()).String()
			if strings.Contains(value.String(), "*") {
				if _strv == "" || _strv == "null" {
					mapping[key.String()] = nil
				}
			}
			// fmt.Println()
			v := strings.ReplaceAll(value.String(), "*", "")
			switch v {
			case "int", "int64":
				mapping[key.String()] = reqdata.Get(key.String()).Int()
			case "float", "float64", "float32":
				mapping[key.String()] = reqdata.Get(key.String()).Float()
			case "string", "models.JSONStringColumn":
				mapping[key.String()] = reqdata.Get(key.String()).String()
			case "models.JSONTime", "time.Time", "gorm.DeletedAt", "gorm.UpdatedAt", "gorm.CreatedAt":
				tv := StringToDate(_strv)
				if Validate(tv) {
					mapping[key.String()] = tv
				}
			default:
				mapping[key.String()] = reqdata.Get(key.String()).String()
			}
		} else {
			if strings.Contains(value.String(), "*") {
				mapping[key.String()] = nil
			} else {
				v := strings.ReplaceAll(value.String(), "*", "")
				switch v {
				case "int", "int64", "float", "float64", "float32":
					mapping[key.String()] = 0
				case "string", "models.JSONStringColumn":
					mapping[key.String()] = ""
				case "gorm.DeletedAt":
				case "models.JSONTime", "time.Time", "gorm.UpdatedAt", "gorm.CreatedAt":
					mapping[key.String()] = time.Now()
				default:
					mapping[key.String()] = ""
				}
			}
		}
		// fmt.Println(value.String())
		return true
	})
	return mapping, nil
}
