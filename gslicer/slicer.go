package gslicer

import (
	"context"
	"fmt"
	"math"
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
// size<=0时按整批返回一批
func (s *Slicer[T]) Batch(size int) [][]T {
	s.lock.Lock()
	defer s.lock.Unlock()
	if size <= 0 {
		if len(s.data) == 0 {
			return [][]T{}
		}
		return [][]T{s.data}
	}
	var r = make([][]T, 0, len(s.data)/size+1)
	for i := 0; i < len(s.data); i += size {
		end := min(i+size, len(s.data))
		r = append(r, s.data[i:end])
	}
	return r
}

// BatchForeach 分批处理
// f 返回false时停止处理, f里的参数是分批后的切片
// size<=0时按整批处理一次
func (s *Slicer[T]) BatchForeach(f func([]T) bool, size int) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if size <= 0 {
		if len(s.data) > 0 {
			f(s.data)
		}
		return
	}
	for i := 0; i < len(s.data); i += size {
		end := min(i+size, len(s.data))
		r := s.data[i:end]
		if !f(r) {
			break
		}
	}
}

// Concurrency 并发处理
// f 并发处理函数 , size 并发数量
// 使用数据快照执行, f 中回调本 Slicer 的方法不会死锁
// f 内的 panic 会被捕获, 并在所有任务结束后在调用方重新抛出
func (s *Slicer[T]) Concurrency(f func(T), size int) {
	s.lock.Lock()
	data := make([]T, len(s.data))
	copy(data, s.data)
	s.lock.Unlock()

	if size <= 0 {
		size = 1
	}
	var (
		wg       = &sync.WaitGroup{}
		limit    = make(chan struct{}, size)
		po       = &sync.Once{}
		panicked bool
		panicVal any
	)
	for _, v := range data {
		wg.Add(1)
		limit <- struct{}{}
		go func(_v T) {
			defer func() {
				if r := recover(); r != nil {
					po.Do(func() {
						panicked = true
						panicVal = r
					})
				}
				wg.Done()
				<-limit
			}()
			f(_v)
		}(v)
	}
	wg.Wait()
	if panicked {
		panic(panicVal)
	}
}

// ConcurrencyIdx 并发处理
// f并发处理函数, _ele元素项, _idx索引
// size并发数量
// 使用数据快照执行, f 中回调本 Slicer 的方法不会死锁
// f 内的 panic 会被捕获, 并在所有任务结束后在调用方重新抛出
func (s *Slicer[T]) ConcurrencyIdx(f func(_ele T, _idx int), size int) {
	s.lock.Lock()
	data := make([]T, len(s.data))
	copy(data, s.data)
	s.lock.Unlock()

	if size <= 0 {
		size = 1
	}
	var (
		wg       = &sync.WaitGroup{}
		limit    = make(chan struct{}, size)
		po       = &sync.Once{}
		panicked bool
		panicVal any
	)
	for i, v := range data {
		wg.Add(1)
		limit <- struct{}{}
		go func(_v T, _i int) {
			defer func() {
				if r := recover(); r != nil {
					po.Do(func() {
						panicked = true
						panicVal = r
					})
				}
				wg.Done()
				<-limit
			}()
			f(_v, _i)
		}(v, i)
	}
	wg.Wait()
	if panicked {
		panic(panicVal)
	}
}

// ConcurrencyErr 支持错误收集与取消的并发处理
// f 并发处理函数, size 并发数量
// 任一任务返回 error 或 ctx 被取消时停止剩余任务
// 返回首个 error; ctx 被取消且无任务错误时返回 ctx.Err()
func (s *Slicer[T]) ConcurrencyErr(ctx context.Context, f func(context.Context, T) error, size int) error {
	s.lock.Lock()
	data := make([]T, len(s.data))
	copy(data, s.data)
	s.lock.Unlock()

	if len(data) == 0 {
		return nil
	}
	if size <= 0 {
		size = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       = &sync.WaitGroup{}
		idxCh    = make(chan int)
		errOnce  = &sync.Once{}
		firstErr error
	)
	for w := 0; w < size; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idxCh {
				if ctx.Err() != nil {
					return
				}
				if err := f(ctx, data[i]); err != nil {
					errOnce.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
			}
		}()
	}
	for i := range data {
		select {
		case idxCh <- i:
		case <-ctx.Done():
		}
	}
	close(idxCh)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
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

// DataCopy 返回数据的副本, 外部修改不影响内部数据, 可安全在锁外使用
func (s *Slicer[T]) DataCopy() []T {
	s.lock.Lock()
	defer s.lock.Unlock()
	data := make([]T, len(s.data))
	copy(data, s.data)
	return data
}

// IsEmpty 是否为空切片
func (s *Slicer[T]) IsEmpty() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return len(s.data) == 0
}

// IsNotEmpty 是否非空切片
func (s *Slicer[T]) IsNotEmpty() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return len(s.data) > 0
}

// Clone 克隆一个数据独立的新 Slicer
func (s *Slicer[T]) Clone() *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	data := make([]T, len(s.data))
	copy(data, s.data)
	return NewSlicer(data)
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

// InSlice 判断元素是否在数组中 (InSilce 的正确拼写别名)
func (s *Slicer[T]) InSlice(item T, equal func(a, b T) bool) bool {
	return s.InSilce(item, equal)
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

// All 判断是否所有元素都符合条件
func (s *Slicer[T]) All(_f func(T) bool) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, v := range s.data {
		if !_f(v) {
			return false
		}
	}
	return true
}

// None 判断是否没有元素符合条件
func (s *Slicer[T]) None(_f func(T) bool) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, v := range s.data {
		if _f(v) {
			return false
		}
	}
	return true
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

// First 返回第一个元素, 空切片返回零值
func (s *Slicer[T]) First() T {
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.data) == 0 {
		return *new(T)
	}
	return s.data[0]
}

// Last 返回最后一个元素, 空切片返回零值
func (s *Slicer[T]) Last() T {
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.data) == 0 {
		return *new(T)
	}
	return s.data[len(s.data)-1]
}

// At 返回指定索引的元素, 越界返回零值
func (s *Slicer[T]) At(idx int) T {
	s.lock.Lock()
	defer s.lock.Unlock()
	if idx < 0 || idx >= len(s.data) {
		return *new(T)
	}
	return s.data[idx]
}

// IndexOf 返回第一个符合条件元素的索引, 未找到返回-1
func (s *Slicer[T]) IndexOf(item T, equal func(a, b T) bool) int {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i, v := range s.data {
		if equal(v, item) {
			return i
		}
	}
	return -1
}

// LastIndexOf 返回最后一个符合条件元素的索引, 未找到返回-1
func (s *Slicer[T]) LastIndexOf(item T, equal func(a, b T) bool) int {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i := len(s.data) - 1; i >= 0; i-- {
		if equal(s.data[i], item) {
			return i
		}
	}
	return -1
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

// Filter 过滤出符合条件的元素, 返回新的 Slicer, 不修改当前数据
func (s *Slicer[T]) Filter(_f func(T) bool) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	var matches = make([]T, 0, len(s.data))
	for _, v := range s.data {
		if _f(v) {
			matches = append(matches, v)
		}
	}
	return NewSlicer(matches)
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

// PopTail 取出最后一个元素并且在切片中删除
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

// RemoveByIdx 删除切片中的指定索引元素
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
	if offset < 0 {
		offset = 0
	}
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

// TakeN 取前n个元素, 返回新的 Slicer, 不修改当前数据
func (s *Slicer[T]) TakeN(n int) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	if n <= 0 {
		return NewSlicer(make([]T, 0))
	}
	if n > len(s.data) {
		n = len(s.data)
	}
	data := make([]T, n)
	copy(data, s.data[:n])
	return NewSlicer(data)
}

// SkipN 跳过前n个元素, 返回新的 Slicer, 不修改当前数据
func (s *Slicer[T]) SkipN(n int) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	if n < 0 {
		n = 0
	}
	if n >= len(s.data) {
		return NewSlicer(make([]T, 0))
	}
	data := make([]T, len(s.data)-n)
	copy(data, s.data[n:])
	return NewSlicer(data)
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
// order 切片字段值顺序, 必须为 slice/array, 否则不排序直接返回
func (s *Slicer[T]) SortByField(_get_field_value func(T) any, order any) *Slicer[T] {
	orderv := reflect.ValueOf(order)
	if order == nil || (orderv.Kind() != reflect.Slice && orderv.Kind() != reflect.Array) {
		return s
	}
	rank := make(map[string]int, orderv.Len())
	for i := 0; i < orderv.Len(); i++ {
		rank[fmt.Sprint(orderv.Index(i).Interface())] = i
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	// 预先计算每个元素的排序权重, 避免每次比较都执行 fmt.Sprint
	keys := make([]int, len(s.data))
	for i, v := range s.data {
		rk, ok := rank[fmt.Sprint(_get_field_value(v))]
		if !ok {
			rk = math.MaxInt
		}
		keys[i] = rk
	}
	idxs := make([]int, len(s.data))
	for i := range idxs {
		idxs[i] = i
	}
	sort.SliceStable(idxs, func(a, b int) bool {
		return keys[idxs[a]] < keys[idxs[b]]
	})
	ndata := make([]T, len(s.data))
	for i, idx := range idxs {
		ndata[i] = s.data[idx]
	}
	s.data = ndata
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

	newData := make([]T, 0, len(s.data))
	newData = append(newData, s.data...)
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
// 使用 keyFun 将元素转换为可比较的 key，计算基准集合与所有 ats 集合的交集。
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

// Union 求并集：基准集合 s.data 的元素 + ats 中 key 不重复的元素
// 相同 key 只保留首次出现的元素，结果赋值给 s.data。
// 注意：这里要求 keyFun 返回的值必须是可比较的类型
func (s *Slicer[T]) Union(keyFun func(itm T) any, ats ...[]T) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()

	seen := make(map[any]struct{}, len(s.data))
	result := make([]T, 0, len(s.data))
	for _, item := range s.data {
		key := keyFun(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	for _, at := range ats {
		for _, item := range at {
			key := keyFun(item)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, item)
		}
	}
	s.data = result
	return s
}

// Group 根据条件进行分组 成数组
// 注意：这里要求 keyFun 返回的值必须是可比较的类型
func (s *Slicer[T]) GroupBy(keyFun func(itm T) any) *GroupData[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	groupdata := NewGroupData[T]()
	for _, item := range s.data {
		groupdata.Set(keyFun(item), item)
	}
	return groupdata
}

// KeyBy 按 key 将元素索引成 map
// key 冲突时后者覆盖前者
// 注意：这里要求 keyFun 返回的值必须是可比较的类型
func (s *Slicer[T]) KeyBy(keyFun func(itm T) any) map[any]T {
	s.lock.Lock()
	defer s.lock.Unlock()
	result := make(map[any]T, len(s.data))
	for _, item := range s.data {
		result[keyFun(item)] = item
	}
	return result
}

// Foreach 遍历每个元素
// foreach 返回false时，中断遍历
func (s *Slicer[T]) Foreach(foreach func(idx int, itm T) bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i, item := range s.data {
		if !foreach(i, item) {
			break
		}
	}
}

// ForeachModify 遍历每个元素并修改
// foreach 返回false时，中断遍历
func (s *Slicer[T]) ForeachModify(foreach func(idx int, itm T) (T, bool)) *Slicer[T] {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i, item := range s.data {
		if itm, ok := foreach(i, item); ok {
			s.data[i] = itm
		} else {
			break
		}
	}
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

// Map 批量将 T 类型元素转换为 R 类型元素
func Map[T, R any](input []T, transform func(T) R) []R {
	result := make([]R, len(input))
	for i, v := range input {
		result[i] = transform(v)
	}
	return result
}

// Pluck 批量提取元素的字段值 (Map 的语义化别名)
func Pluck[T, R any](input []T, get func(T) R) []R {
	return Map(input, get)
}

// Flatten 将二维切片展平为一维
func Flatten[T any](input [][]T) []T {
	total := 0
	for _, v := range input {
		total += len(v)
	}
	result := make([]T, 0, total)
	for _, v := range input {
		result = append(result, v...)
	}
	return result
}

// ZipPair Zip 的结果元素
type ZipPair[A, B any] struct {
	A A
	B B
}

// Zip 将两个切片按索引配对, 结果长度为两个输入长度的较小值
func Zip[A, B any](a []A, b []B) []ZipPair[A, B] {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	result := make([]ZipPair[A, B], 0, n)
	for i := 0; i < n; i++ {
		result = append(result, ZipPair[A, B]{A: a[i], B: b[i]})
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
