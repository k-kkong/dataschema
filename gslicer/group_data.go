package gslicer

type GroupData[T any] struct {
	slicers map[any]*Slicer[T]
	keys    []any
}

// NewGroupData 创建 GroupData 对象
func NewGroupData[T any]() *GroupData[T] {
	return &GroupData[T]{
		slicers: make(map[any]*Slicer[T]),
		keys:    make([]any, 0),
	}
}

// 延迟初始化, 兼容 new(GroupData[T]) 零值用法
func (g *GroupData[T]) init() {
	if g.slicers == nil {
		g.slicers = make(map[any]*Slicer[T])
	}
	if g.keys == nil {
		g.keys = make([]any, 0)
	}
}

func (g *GroupData[T]) Set(key any, value T) {
	g.init()
	if _, ok := g.slicers[key]; !ok {
		g.keys = append(g.keys, key)
		g.slicers[key] = NewSlicer(make([]T, 0))
	}
	g.slicers[key].Append(value)
}

// Get 获取指定 key 的数据, key 不存在时返回空切片
func (g *GroupData[T]) Get(key any) []T {
	if sl, ok := g.slicers[key]; ok {
		return sl.Data()
	}
	return make([]T, 0)
}

// Has 判断 key 是否存在
func (g *GroupData[T]) Has(key any) bool {
	_, ok := g.slicers[key]
	return ok
}

// Delete 删除指定 key 的分组
func (g *GroupData[T]) Delete(key any) {
	if _, ok := g.slicers[key]; !ok {
		return
	}
	delete(g.slicers, key)
	for i, k := range g.keys {
		if k == key {
			g.keys = append(g.keys[:i], g.keys[i+1:]...)
			break
		}
	}
}

func (g *GroupData[T]) Keys() []any {
	return g.keys
}

func (g *GroupData[T]) Values2Dim() [][]T {
	var values [][]T
	for _, key := range g.keys {
		values = append(values, g.slicers[key].Data())
	}
	return values
}

func (g *GroupData[T]) Values() []T {
	var values []T
	for _, key := range g.keys {
		values = append(values, g.slicers[key].Data()...)
	}
	return values
}

func (g *GroupData[T]) ValuesSlic() []*Slicer[T] {
	var values []*Slicer[T]
	for _, key := range g.keys {
		values = append(values, g.slicers[key])
	}
	return values
}

func (g *GroupData[T]) Len() int {
	return len(g.keys)
}

func (g *GroupData[T]) Foreach(foreach func(key any, values *Slicer[T]) bool) {
	for _, key := range g.keys {
		if !foreach(key, g.slicers[key]) {
			break
		}
	}
}
