package dvap2

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func catch(err *error, printStack bool) {
	if r := recover(); r != nil {
		// fmt.Println(printStack)
		if printStack {
			printStackTrace()
		}
		*err = fmt.Errorf("%v", r)
		red := "\033[31m"
		reset := "\033[0m"
		fmt.Println(red + "" + fmt.Errorf("%v", r).Error() + reset)
	}
}

func printStackTrace() {
	stack := make([]byte, 1024)
	runtime.Stack(stack, false)
	fmt.Printf("Stack Trace:\n%s\n", stack)
}

func isNonNilData(err *error, label string, data interface{}) bool {
	if data == nil {
		*err = fmt.Errorf("%s cannot be nil", label)
		return false
	}

	return true
}

// 偷看数据 返回数据值，类型，种类，长度
func inspectData(data interface{}) (reflect.Value, reflect.Type, reflect.Kind, int) {
	var dataValue reflect.Value
	var dataValueType reflect.Type
	var dataValueKind reflect.Kind
	dataValueLen := 0

	if data != nil {
		dataValue = reflect.ValueOf(data)
		dataValueType = dataValue.Type()
		dataValueKind = dataValue.Kind()

		if dataValueKind == reflect.Ptr {
			dataValue = dataValue.Elem()
		}

		if dataValueKind == reflect.Slice {
			dataValueLen = dataValue.Len()
		} else if dataValueKind == reflect.Map {
			dataValueLen = len(dataValue.MapKeys())
		}
	}

	return dataValue, dataValueType, dataValueKind, dataValueLen
}

func forEachSlice(slice reflect.Value, sliceLen int, eachCallback func(reflect.Value, int)) {
	forEachSliceStoppable(slice, sliceLen, func(each reflect.Value, i int) bool {
		eachDataValue := slice.Index(i)
		eachCallback(eachDataValue, i)
		return true
	})
}

func forEachSliceStoppable(slice reflect.Value, sliceLen int, eachCallback func(reflect.Value, int) bool) {
	for i := 0; i < sliceLen; i++ {
		eachDataValue := slice.Index(i)
		shouldContinue := eachCallback(eachDataValue, i)

		if !shouldContinue {
			return
		}
	}
}

func callFuncSliceLoop(funcToCall, param reflect.Value, i int, numIn int) []reflect.Value {
	if numIn == 1 {
		return funcToCall.Call([]reflect.Value{param})
	}

	return funcToCall.Call([]reflect.Value{param, reflect.ValueOf(i)})
}

func makeSlice(valueType reflect.Type, args ...int) reflect.Value {
	sliceLen := 0
	sliceCap := 0

	if len(args) > 0 {
		sliceLen = args[0]

		if len(args) > 1 {
			sliceCap = args[1]
		}
	}

	return reflect.MakeSlice(valueType, sliceLen, sliceCap)
}

// VtoJsonString 将值转换为json
func VtoJsonString(value interface{}) string {
	jb, err := json.Marshal(&value)
	if err != nil {
		return ""
	}
	return string(jb)
}

// VtoJson 将值转换为json
func VtoJson(value interface{}) gjson.Result {
	jb, err := json.Marshal(&value)
	if err != nil {
		return gjson.Result{}
	}
	return gjson.ParseBytes(jb)
}

func JArrToInterface(value []gjson.Result) interface{} {

	var result []interface{}

	for _, v := range value {
		result = append(result, v.Value())
	}

	if len(result) < 1 {
		result = make([]interface{}, 0)
	}

	return result
}

func VSetV(parent gjson.Result, sub interface{}, path string) gjson.Result {

	value, _ := sjson.Set(parent.String(), path, sub)

	return gjson.Parse(value)

}

func VSSetV(parent string, sub interface{}, path string) string {

	value, _ := sjson.Set(parent, path, sub)

	return value

}

// 解析gorm字段
func SerializeGormTagToJSON(i interface{}) (string, error) {
	objValue := reflect.ValueOf(i)
	objType := objValue.Type()

	jsonObj := make(map[string]interface{})
	for j := 0; j < objValue.NumField(); j++ {
		fieldValue := objValue.Field(j)
		fieldType := objType.Field(j)

		var fieldTypeName string
		if fieldValue.Kind() == reflect.Interface {
			fieldTypeName = "interface {}"
		} else {
			fieldTypeName = reflect.TypeOf(fieldValue.Interface()).String()
		}

		tag_value := fieldType.Tag.Get("gorm")
		gormtags := strings.Split(tag_value, ";")

		for _, v := range gormtags {
			if strings.HasPrefix(v, "column:") {
				fieldname := strings.Split(v, "column:")[1]
				jsonObj[fieldname] = fieldTypeName
				break
			}
		}

	}

	jsonBytes, err := json.Marshal(jsonObj)
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}
