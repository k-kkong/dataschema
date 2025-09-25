package gslicer

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"sync"
)

// Slicer  切片处理
type Slicer[T any] struct {
	data []T

	lock sync.Mutex
}

// NewSlicer 从切片创建切片对象
func NewSlicer[T any](input []T, is_copy ...bool) *Slicer[T] {
	v := &Slicer[T]{
		lock: sync.Mutex{},
	}
	if len(is_copy) > 0 && is_copy[0] {
		v.data = make([]T, len(input))
		copy(v.data, input)
	} else {
		v.data = input
	}
	return v
}

// Batch 将切片按指定大小分割
func (s *Slicer[T]) Batch(size int) [][]T {
	s.lock.Lock()
	defer s.lock.Unlock()
	var r = make([][]T, 0, len(s.data)/size+1)
	for i := 0; i < len(s.data); i += size {
		end := min(i+size, len(s.data))
		r = append(r, s.data[i:end])
	}
	return r
}

// BatchForeach 分批处理
// f 返回false时停止处理, f里的参数是分批后的切片
func (s *Slicer[T]) BatchForeach(f func([]T) bool, size int) {
	s.lock.Lock()
	defer s.lock.Unlock()
	// var r = make([][]T, 0, len(s.data)/size+1)
	for i := 0; i < len(s.data); i += size {
		end := min(i+size, len(s.data))
		r := s.data[i:end]
		if !f(r) {
			break
		}
	}
}

func (s *Slicer[T]) Len() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return len(s.data)
}

func (s *Slicer[T]) Data() []T {
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.data) == 0 {
		return make([]T, 0)
	}
	return s.data
}

// func (s *Slicer[T]) ResSet() *Slicer[T] {
// 	if s.resSet == nil {
// 		s.resSet = NewSlicer(make([]T, 0))
// 	}
// 	return s.resSet
// }

// InSilce  判断元素是否在数组中的方法
func (s *Slicer[T]) InSilce(item T, equal func(a, b T) bool) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, v := range s.data {
		if equal(v, item) {
			return true
		}
	}
	return false
}

// Contains 判断是否有元素符合条件
func (s *Slicer[T]) Contains(equal func(b T) bool) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, v := range s.data {
		if equal(v) {
			return true
		}
	}
	return false
}

// Count 根据条件返回符合条件的元素数量
func (s *Slicer[T]) Count(_f func(T) bool) int {
	s.lock.Lock()
	defer s.lock.Unlock()

	count := 0
	for _, v := range s.data {
		if _f(v) {
			count++
		}
	}
	return count
}

// Divide 分割数组
func (s *Slicer[T]) Divide(_f_div func(T) bool) (hit *Slicer[T], miss *Slicer[T]) {
	s.lock.Lock()
	defer s.lock.Unlock()

	hits := make([]T, 0, len(s.data))
	misses := make([]T, 0, len(s.data))
	for _, v := range s.data {
		if _f_div(v) {
			hits = append(hits, v)
		} else {
			misses = append(misses, v)
		}
	}
	return NewSlicer(hits), NewSlicer(misses)
}

// Take 根据条件返回第一个符合条件的元素
func (s *Slicer[T]) Take(_f func(T) bool) T {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, v := range s.data {
		if _f(v) {
			return v
		}
	}
	return *new(T)
}

// Find 查找数组中符合条件的元素
func (s *Slicer[T]) Find(_f func(T) bool) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	var matches = make([]T, 0, len(s.data))
	for _, v := range s.data {
		if _f(v) {
			matches = append(matches, v)
		}
	}
	s.data = matches
	return s
}

func (s *Slicer[T]) _pop_idx(idx int) T {
	if idx < 0 || idx >= len(s.data) {
		return *new(T)
	}
	x := s.data[idx]
	if len(s.data) == 1 {
		s.data = make([]T, 0)
	} else {
		var new = make([]T, len(s.data)-1)
		copy(new[:idx], s.data[:idx])
		if idx < len(s.data)-1 {
			copy(new[idx:], s.data[idx+1:])
		}
		s.data = new
	}
	return x
}

// PopIdx 取出指定位置的元素并且在切片中删除
func (s *Slicer[T]) PopIdx(idx int) T {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s._pop_idx(idx)
}

// PopWhere 取出指定位置的元素并且在切片中删除
func (s *Slicer[T]) PopWhere(_f func(T) bool) T {
	s.lock.Lock()
	defer s.lock.Unlock()
	for idx, v := range s.data {
		if _f(v) {
			return s._pop_idx(idx)
		}
	}
	return *new(T)
}

// PopHead 取出第一个元素并且在切片中删除
func (s *Slicer[T]) PopHead() T {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s._pop_idx(0)
}

// PopHead 取出最后一个元素并且在切片中删除
func (s *Slicer[T]) PopTail() T {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s._pop_idx(len(s.data) - 1)
}

// Remove 根据条件删除元素
func (s *Slicer[T]) Remove(_f func(T) bool) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	var ndata = make([]T, 0, len(s.data))
	for _, v := range s.data {
		if !_f(v) {
			ndata = append(ndata, v)
		}
	}
	s.data = ndata
	return s
}

// Append 向切片尾部添加元素
func (s *Slicer[T]) Append(item ...T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	var new = make([]T, len(s.data)+len(item))
	copy(new, s.data)
	copy(new[len(s.data):], item)
	s.data = new
	return s
}

// Prepend 向切片头部添加元素
func (s *Slicer[T]) Prepend(item ...T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	var new = make([]T, len(s.data)+len(item))
	copy(new[len(item):], s.data)
	copy(new, item)
	s.data = new
	return s
}

// Delete 删除切片中的指定索引元素
func (s *Slicer[T]) RemoveByIdx(idx int) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	s._pop_idx(idx)
	return s
}

// InsertIdx 在指定索引位置插入元素
// |idx<=0 从头部插入
// |idx>=len(s.data) 从尾部插入
// |idx=1 表示从索引1之前插入 以此类推
func (s *Slicer[T]) InsertIdx(idx int, item ...T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	var new = make([]T, 0, len(s.data)+len(item))
	if idx < 0 {
		new = append(new, item...)
		new = append(new, s.data...)
	} else if idx >= len(s.data) {
		new = append(new, s.data...)
		new = append(new, item...)
	} else {
		new = append(new, s.data[:idx]...)
		new = append(new, item...)
		if idx < len(s.data) {
			new = append(new, s.data[idx:]...)
		}
	}
	s.data = new
	return s
}

// Page 分页
// |offset 偏移量，跳过前offset个
// |limit 每页数量
func (s *Slicer[T]) Page(offset, limit int) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	if offset >= len(s.data) || limit <= 0 {
		s.data = make([]T, 0)
		return s
	}
	if offset > 0 {
		s.data = s.data[offset:]
	}
	if limit >= len(s.data) {
		return s
	}
	s.data = s.data[:limit]
	return s
}

// Sort 排序
// |a>b 降序
// |a<b 升序
func (s *Slicer[T]) Sort(_f func(a, b T) bool) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	sort.Slice(s.data, func(i, j int) bool {
		return _f(s.data[i], s.data[j])
	})
	return s
}

// SortByField 将指定字段值，按照所给顺序排序
// _get_field_value 获取字段值
// order 切片字段值顺序
func (s *Slicer[T]) SortByField(_get_field_value func(T) any, order any) *Slicer[T] {

	// sort := "desc"
	// if len(sorts) > 0 {
	// 	sort = sorts[0]
	// }

	// side := "head"
	// if sort == "asc" {
	// 	side = "tail"
	// }

	// s.data  是slice
	var mapsort = map[string]int{}
	orderv := reflect.ValueOf(order)
	for i := 0; i < orderv.Len(); i++ {
		mapsort[fmt.Sprint(orderv.Index(i).Interface())] = i
	}

	s.Sort(func(a, b T) bool {
		av_k := fmt.Sprint(_get_field_value(a))
		bv_k := fmt.Sprint(_get_field_value(b))

		av_v, ok := mapsort[av_k]
		if !ok {
			av_v = 9999999999
		}
		bv_v, ok := mapsort[bv_k]
		if !ok {
			bv_v = 9999999999
		}
		return av_v < bv_v
	})

	return s
}

// Reverse 反转
func (s *Slicer[T]) Reverse() *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i, j := 0, len(s.data)-1; i < j; i, j = i+1, j-1 {
		s.data[i], s.data[j] = s.data[j], s.data[i]
	}
	return s
}

// Shuffle 随机打乱顺序
func (s *Slicer[T]) Shuffle(source int64) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	r := rand.New(rand.NewSource(source))
	r.Shuffle(len(s.data), func(i, j int) {
		s.data[i], s.data[j] = s.data[j], s.data[i]
	})
	return s
}

// Rand 随机打乱顺序
func (s *Slicer[T]) Rand(source int64) *Slicer[T] {
	s.Shuffle(source)
	return s
}

// Unique 去重
// 根据 keyFun 对本地data和ats中的数据进行去重
func (s *Slicer[T]) Unique(keyFun func(itm T) any, ats ...[]T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	newData := s.data
	for _, at := range ats {
		newData = append(newData, at...)
	}

	seen := make(map[any]struct{}, len(newData))
	unqdata := make([]T, 0, len(newData))
	for _, item := range newData {
		key := keyFun(item)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			unqdata = append(unqdata, item)
		}
	}
	s.data = unqdata
	return s
}

// Intersection 求所有集合中的交集
// 使用 keyFun 将元素转换为可比较的 key，并求基准集合与所有 ats 集合的交集。
// 只保留首次出现的满足条件的元素，结果赋值给 s.data。
func (s *Slicer[T]) Intersection(keyFun func(itm T) any, ats ...[]T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	// 对于每个 ats 切片，构建一个 key 集合 map
	setList := make([]map[any]struct{}, len(ats))
	for i, at := range ats {
		set := make(map[any]struct{})
		for _, item := range at {
			// keyFun 的返回值必须是可比较的类型
			key := keyFun(item)
			set[key] = struct{}{}
		}
		setList[i] = set
	}

	// 对基准集合 s.data 筛选，只保留所有 ats 中都存在的元素
	intersection := make([]T, 0, len(s.data))
	// 用于确保最终结果中相同 key 只保留一份
	seen := make(map[any]struct{}, len(s.data))
	for _, item := range s.data {
		key := keyFun(item)
		// 如果已经添加过，则跳过
		if _, exists := seen[key]; exists {
			continue
		}

		// 检查所有的 ats 集合中，都包含此 key
		foundInAll := true
		for _, set := range setList {
			if _, ok := set[key]; !ok {
				foundInAll = false
				break
			}
		}

		if foundInAll {
			intersection = append(intersection, item)
			seen[key] = struct{}{}
		}
	}

	s.data = intersection
	return s
}

// SymmetricDifference 对称差集：
// 保留只出现在 s.data 或 at 其中一个集合中的元素。
// keyFun 用于生成每个元素的唯一标识，需要返回可比较的类型。
func (s *Slicer[T]) SymmetricDifference(keyFun func(itm T) any, at []T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	// 构建 s.data 的键集合
	aSet := make(map[any]T, len(s.data))
	for _, item := range s.data {
		key := keyFun(item)
		aSet[key] = item
	}

	// 构建 at 切片的键集合
	bSet := make(map[any]T, len(at))
	for _, item := range at {
		key := keyFun(item)
		bSet[key] = item
	}

	result := make([]T, 0, len(s.data)+len(at))
	for key, item := range aSet {
		if _, exists := bSet[key]; !exists {
			result = append(result, item)
		}
	}

	for key, item := range bSet {
		if _, exists := aSet[key]; !exists {
			result = append(result, item)
		}
	}
	s.data = result
	return s
}

// Difference 返回本地集合 s.data 中不在 at 中的元素。
// 注意：这里要求 keyFun 返回的值必须是可比较的类型
func (s *Slicer[T]) Difference(keyFun func(itm T) any, at []T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	atMap := make(map[any]struct{}, len(at))
	for _, item := range at {
		key := keyFun(item)
		atMap[key] = struct{}{}
	}
	result := make([]T, 0, len(s.data))
	for _, item := range s.data {
		key := keyFun(item)
		if _, exists := atMap[key]; !exists {
			result = append(result, item)
		}
	}
	s.data = result
	return s
}

// func (s *Slicer[T]) Map(transform func(T) any) any {
// 	s.lock.Lock()
// 	defer s.lock.Unlock()
// 	result := make([]any, 0, len(s.data))
// 	for i, v := range s.data {
// 		result[i] = transform(v)
// 	}
// 	return result
// }

// Map 方法
func Map[T, R any](input []T, transform func(T) R) []R {
	result := make([]R, 0, len(input))
	for i, v := range input {
		result[i] = transform(v)
	}
	return result
}

// DuplicateMerge 合并重复元素
// 使用dupf判断元素重复
// 再使用transform合并重复的元素，生成新的元素
// 新旧元素可以是不同类型
func DuplicateMerge[T, R any](input []T,
	dupf func(T) any,
	transform func(T, R) R,
) []R {
	seen := make(map[any]R)
	order := make([]any, 0, len(input))
	for _, v := range input {
		key := dupf(v)
		if _, ok := seen[key]; !ok {
			seen[key] = *new(R)
			order = append(order, key)
		}
		seen[key] = transform(v, seen[key])
	}

	result := make([]R, 0, len(seen))
	for _, v := range order {
		result = append(result, seen[v])
	}
	return result
}

// Reduce 方法
func Reduce[T, R any](input []T, transform func(R, T) R) R {
	result := *new(R)
	for _, v := range input {
		result = transform(result, v)
	}
	return result
}
