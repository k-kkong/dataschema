package dvap2

import (
	"fmt"
	"reflect"

	gubrak1 "github.com/novalagung/gubrak"
	"github.com/novalagung/gubrak/v2"
)

// 根据条件 将 sslice 划分成符合条件的filters 和不符合条件的removes
func FilterStructSlice(sslice interface{}, removes interface{}, filters interface{}, ff interface{}) error {
	// 检查 sslice 是否为切片类型
	var err error
	func(err *error) {
		defer catch(err, false)
		sliceValue := reflect.ValueOf(sslice)
		if sliceValue.Kind() != reflect.Slice {
			*err = fmt.Errorf("sslice 必须是切片类型")
			return
		}

		// 检查 removes 和 filters 是否为指针类型
		removesValue := reflect.ValueOf(removes)
		if removesValue.Kind() != reflect.Ptr {
			*err = fmt.Errorf("removes 必须是指针类型")
			return
		}
		filtersValue := reflect.ValueOf(filters)
		if filtersValue.Kind() != reflect.Ptr {
			*err = fmt.Errorf("filters 必须是指针类型")
			return
		}
		// 检查 removes 和 filters 是否为可修改的指针
		if !removesValue.Elem().CanSet() {
			*err = fmt.Errorf("removes 必须是可修改的指针")
			return
		}
		if !filtersValue.Elem().CanSet() {
			*err = fmt.Errorf("filters 必须是可修改的指针")
			return
		}

		// 检查 ff 是否为可调用的函数
		funcValue := reflect.ValueOf(ff)
		if funcValue.Kind() != reflect.Func {
			*err = fmt.Errorf("ff 必须是函数类型")
			return
		}
		var rem, fil interface{}
		rem, fil, *err = gubrak1.Remove(sslice, ff)
		reflect.Indirect(removesValue).Set(reflect.ValueOf(rem))
		reflect.Indirect(filtersValue).Set(reflect.ValueOf(fil))
	}(&err)
	return err

}

// 根据条件 将 sslice 划分成符合条件的filters 和不符合条件的removes
func FilterStructSliceGetFil(sslice interface{}, filters interface{}, ff interface{}) error {

	var err error
	func(err *error) {
		defer catch(err, false)
		// 检查 sslice 是否为切片类型
		sliceValue := reflect.ValueOf(sslice)
		if sliceValue.Kind() != reflect.Slice {
			*err = fmt.Errorf("sslice 必须是切片类型")
			return
		}

		filtersValue := reflect.ValueOf(filters)
		if filtersValue.Kind() != reflect.Ptr {
			*err = fmt.Errorf("filters 必须是指针类型")
			return

		}
		// 检查 removes 和 filters 是否为可修改的指针
		if !filtersValue.Elem().CanSet() {
			*err = fmt.Errorf("filters 必须是可修改的指针")
			return
		}

		// 检查 ff 是否为可调用的函数
		funcValue := reflect.ValueOf(ff)
		if funcValue.Kind() != reflect.Func {
			*err = fmt.Errorf("ff 必须是函数类型")
			return
		}

		var fil interface{}
		_, fil, *err = gubrak1.Remove(sslice, ff)
		reflect.Indirect(filtersValue).Set(reflect.ValueOf(fil))

	}(&err)

	return err
}

// 对数组进行去重然后合并  得到一个新的数组
// getkeyfunc 至少有一个返回值 获取唯一去重的key
// mergeFunc 合并函数  参数1 上一次合并结果,当前迭代项  返回合并结果值
// 复杂度 < 2n
// 使用方法
/*
result, err := helper_value.UniqueMergeArray(inputArray, func(a humen) string {
		return a.Name
	}, func(perv string, current humen) string {
		return perv + current.Name
	})
*/
func UniqueMergeArray(arr interface{}, getkeyFunc interface{}, mergeFunc interface{}) (interface{}, error) {
	var result interface{}
	var err error

	func(err *error) {
		defer catch(err, false)
		// 检查传入的切片是否为 nil
		if !isNonNilData(err, "arr", arr) {
			return
		}
		//解包
		// arrValue, arrType, _, arrValueLen := inspectData(arr)
		arrValue, _, _, arrValueLen := inspectData(arr)
		// 创建一个 map 用于存储去重后的元素
		uniqueElements := make(map[interface{}]reflect.Value)
		originSortKey := make([]interface{}, 0)
		// 获取比较和合并函数的反射值
		getkeyFuncValue, mergeFuncValue := reflect.ValueOf(getkeyFunc), reflect.ValueOf(mergeFunc)

		outType := mergeFuncValue.Type().Out(0)
		// fmt.Println(outType)
		// 遍历切片
		forEachSlice(arrValue, arrValueLen, func(each reflect.Value, i int) {
			//调用匿名函数获取唯一健
			getkeyResults := getkeyFuncValue.Call([]reflect.Value{each})
			if len(getkeyResults) < 1 {
				*err = fmt.Errorf("getkeyFunc must return at least one value")
				return
			}
			unqkey := getkeyResults[0].Interface()
			value, ok := uniqueElements[unqkey]
			if ok {
				mergedElem := mergeFuncValue.Call([]reflect.Value{value, each})[0]
				uniqueElements[unqkey] = mergedElem
			} else {
				mergedElem := mergeFuncValue.Call([]reflect.Value{reflect.New(outType).Elem(), each})[0]
				uniqueElements[unqkey] = mergedElem
				originSortKey = append(originSortKey, unqkey)
			}
		})
		// 创建一个切片用于存储结果
		resultSliceType := reflect.SliceOf(outType)
		// resultSlice := makeSlice(outType) 需要先获取该类型的切片类型
		resultSlice := reflect.MakeSlice(resultSliceType, 0, len(originSortKey))
		for _, unqkey := range originSortKey {
			value, ok := uniqueElements[unqkey]
			if ok {
				resultSlice = reflect.Append(resultSlice, value)
			}
		}
		// 返回结果切片的接口值
		result = resultSlice.Interface()
	}(&err)

	return result, err
}

// 切片中是否包含值
func HasValueInSlice(slices interface{}, value interface{}, match_f interface{}) (bool, error) {
	var err error
	var result bool
	func(err *error) {
		defer catch(err, false)
		match_func := reflect.ValueOf(match_f)
		rt := reflect.TypeOf(slices)
		rv := reflect.ValueOf(slices)
		if rt.Kind() == reflect.Slice {
			matchv := reflect.ValueOf(value)
			for i := 0; i < rv.Len(); i++ {
				indexv := rv.Index(i)
				if match_func.Call([]reflect.Value{indexv, matchv})[0].Bool() {
					result = true
					return
				}
			}
		}
	}(&err)
	return result, err
}

// 切片中是否包含值
func IsValueInSlice(slices interface{}, match_f interface{}) bool {
	var err error
	var result bool
	func(err *error) {
		defer catch(err, false)
		match_func := reflect.ValueOf(match_f)
		rt := reflect.TypeOf(slices)
		rv := reflect.ValueOf(slices)
		if rt.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				indexv := rv.Index(i)
				if match_func.Call([]reflect.Value{indexv})[0].Bool() {
					result = true
					return
				}
			}
		}
	}(&err)
	if err != nil {
		return false
	}
	return result
}

// 结构体切片中是否包含指定值
func HasFieldValueInStructSlice(slices interface{}, field string, match interface{}) bool {
	rt := reflect.TypeOf(slices)
	rv := reflect.ValueOf(slices)
	if rt.Kind() == reflect.Slice {
		matchv := reflect.ValueOf(match)
		for i := 0; i < rv.Len(); i++ {
			indexv := rv.Index(i)
			if indexv.Kind() == reflect.Struct {
				fieldv := indexv.FieldByName(field)
				if matchv.Kind() == fieldv.Kind() {
					if reflect.DeepEqual(fieldv.Interface(), matchv.Interface()) {
						return true
					}
				}
			}
		}
	}
	return false
}

// 寻找切片中复合条件的元素
func SliceFind(sslice interface{}, dest interface{}, ff interface{}) error {

	var err error
	func(err *error) {
		// 检查 sslice 是否为切片类型
		sliceValue := reflect.ValueOf(sslice)
		if sliceValue.Kind() != reflect.Slice {
			*err = fmt.Errorf("sslice 必须是切片类型")
			return
		}
		// 检查 ff 是否为可调用的函数
		funcValue := reflect.ValueOf(ff)
		if funcValue.Kind() != reflect.Func {
			*err = fmt.Errorf("ff 必须是可调用的函数")
			return
		}

		// 检查 dest 是否为指针类型
		destValue := reflect.ValueOf(dest)
		if destValue.Kind() != reflect.Ptr {
			*err = fmt.Errorf("dest 必须是指针类型")
			return
		}

		if !destValue.Elem().CanSet() {
			*err = fmt.Errorf("dest 必须是可修改的指针")
			return
		}

		value := gubrak.From(sslice).Find(ff)
		if value.Result() == nil {
			*err = fmt.Errorf("没有找到结果：%s", value.Error())
			return
		}

		// 检查结果类型是否与 dest 参数类型匹配
		resultValue := reflect.ValueOf(value.Result())
		if !resultValue.Type().AssignableTo(destValue.Elem().Type()) {
			*err = fmt.Errorf("结果类型与 dest 参数类型不匹配")
			return
		}
		reflect.Indirect(destValue).Set(resultValue)
	}(&err)
	return err
}
