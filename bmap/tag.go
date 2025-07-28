package bmap

import "strings"

func parseTag(tag string) (string, tagOptions) {
	// 结构体标签如:
	// ""
	// "name"
	// "name,opt"
	// "name,opt,opt2"
	// ",opt"

	res := strings.Split(tag, ",")
	return res[0], res[1:]
}

// 结构体标签切片
type tagOptions []string

// 判断是否包含标签
func (t tagOptions) Has(opt string) bool {
	for _, tagOpt := range t {
		if tagOpt == opt {
			return true
		}
	}
	return false
}
