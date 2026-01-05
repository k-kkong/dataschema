package gslicer

type GroupData[T any] struct {
	slicers map[any]*Slicer[T]
	keys    []any
}

func (g *GroupData[T]) Set(key any, value T) {
	if _, ok := g.slicers[key]; !ok {
		g.keys = append(g.keys, key)
		g.slicers[key] = new(Slicer[T])
	}
	g.slicers[key].Append(value)
}

func (g *GroupData[T]) Get(key any) []T {
	return g.slicers[key].Data()
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
