package gslicer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func eqInt(a, b int) bool { return a == b }

// Map 原实现 len=0 时按下标写入会越界 panic
func TestMap(t *testing.T) {
	r := Map([]int{1, 2, 3}, func(v int) string { return fmt.Sprint(v * 2) })
	if !reflect.DeepEqual(r, []string{"2", "4", "6"}) {
		t.Fatalf("Map got %v", r)
	}
	if len(Map([]int{}, func(v int) int { return v })) != 0 {
		t.Fatal("Map empty input should return empty")
	}
}

// GroupBy 原实现向 nil map 写入会 panic; Get 对不存在的 key 会 nil 指针 panic
func TestGroupBy(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3, 4, 5, 6})
	g := s.GroupBy(func(v int) any { return v % 2 })
	if g.Len() != 2 {
		t.Fatalf("group len got %d", g.Len())
	}
	if len(g.Get(0)) != 3 || len(g.Get(1)) != 3 {
		t.Fatal("group size wrong")
	}
	if len(g.Get(999)) != 0 {
		t.Fatal("missing key should return empty slice")
	}
	if !g.Has(0) || g.Has(999) {
		t.Fatal("Has wrong")
	}
	g.Delete(0)
	if g.Has(0) || g.Len() != 1 {
		t.Fatal("Delete wrong")
	}
	// 零值用法兼容
	z := new(GroupData[int])
	z.Set("k", 1)
	if len(z.Get("k")) != 1 {
		t.Fatal("zero-value GroupData should work")
	}
}

// Batch/BatchForeach size<=0 原来会 panic 或死循环
func TestBatchInvalidSize(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3})
	if r := s.Batch(0); len(r) != 1 || len(r[0]) != 3 {
		t.Fatalf("Batch(0) got %v", r)
	}
	if r := s.Batch(-1); len(r) != 1 {
		t.Fatalf("Batch(-1) got %v", r)
	}
	done := false
	s.BatchForeach(func(b []int) bool { done = true; return true }, 0)
	if !done {
		t.Fatal("BatchForeach(0) should process once")
	}
	if r := NewSlicer([]int{}).Batch(0); len(r) != 0 {
		t.Fatal("empty Batch(0) should be empty")
	}
}

// Page 负 offset 原来会越界 panic
func TestPageNegativeOffset(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3, 4})
	s.Page(-5, 2)
	if !reflect.DeepEqual(s.Data(), []int{1, 2}) {
		t.Fatalf("Page got %v", s.Data())
	}
}

// Unique 原来 append 可能写穿调用方共享的底层数组
func TestUniqueNoAliasing(t *testing.T) {
	base := make([]int, 2, 8)
	base[0], base[1] = 1, 2
	s := NewSlicer(base)
	s.Unique(func(v int) any { return v }, []int{2, 3})
	if !reflect.DeepEqual(s.Data(), []int{1, 2, 3}) {
		t.Fatalf("Unique got %v", s.Data())
	}
	if !reflect.DeepEqual(base[:4], []int{1, 2, 0, 0}) {
		t.Fatalf("caller backing array corrupted: %v", base[:4])
	}
}

// SortByField: order 非法类型不 panic; 按给定顺序排序; 未命中的排最后
func TestSortByField(t *testing.T) {
	type item struct{ name string }
	s := NewSlicer([]item{{"c"}, {"a"}, {"x"}, {"b"}})
	s.SortByField(func(v item) any { return v.name }, []string{"b", "a", "c"})
	got := Pluck(s.Data(), func(v item) string { return v.name })
	if !reflect.DeepEqual(got, []string{"b", "a", "c", "x"}) {
		t.Fatalf("SortByField got %v", got)
	}
	// 非法 order 不 panic
	s.SortByField(func(v item) any { return v.name }, 123)
	s.SortByField(func(v item) any { return v.name }, nil)
}

// Filter 非破坏性, Find 保持原破坏性语义
func TestFilterVsFind(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3, 4})
	r := s.Filter(func(v int) bool { return v%2 == 0 })
	if !reflect.DeepEqual(s.Data(), []int{1, 2, 3, 4}) {
		t.Fatal("Filter must not modify source")
	}
	if !reflect.DeepEqual(r.Data(), []int{2, 4}) {
		t.Fatalf("Filter got %v", r.Data())
	}
	s.Find(func(v int) bool { return v > 2 })
	if !reflect.DeepEqual(s.Data(), []int{3, 4}) {
		t.Fatal("Find should modify source (兼容原语义)")
	}
}

func TestAccessMethods(t *testing.T) {
	s := NewSlicer([]int{10, 20, 30})
	if s.First() != 10 || s.Last() != 30 || s.At(1) != 20 {
		t.Fatal("First/Last/At wrong")
	}
	if s.At(9) != 0 || s.At(-1) != 0 {
		t.Fatal("At out of range should return zero value")
	}
	if s.IndexOf(20, eqInt) != 1 || s.IndexOf(99, eqInt) != -1 {
		t.Fatal("IndexOf wrong")
	}
	du := NewSlicer([]int{5, 6, 5})
	if du.LastIndexOf(5, eqInt) != 2 {
		t.Fatal("LastIndexOf wrong")
	}
	if NewSlicer([]int{}).First() != 0 || !NewSlicer([]int{}).IsEmpty() {
		t.Fatal("empty slicer wrong")
	}
	if s.IsEmpty() || !s.IsNotEmpty() {
		t.Fatal("IsEmpty/IsNotEmpty wrong")
	}
}

func TestCloneAndDataCopy(t *testing.T) {
	s := NewSlicer([]int{1, 2})
	c := s.Clone()
	c.Append(3)
	if s.Len() != 2 || c.Len() != 3 {
		t.Fatal("Clone should be independent")
	}
	dc := s.DataCopy()
	dc[0] = 99
	if s.First() != 1 {
		t.Fatal("DataCopy should be a copy")
	}
}

func TestTakeSkip(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3, 4})
	if !reflect.DeepEqual(s.TakeN(2).Data(), []int{1, 2}) {
		t.Fatal("TakeN wrong")
	}
	if !reflect.DeepEqual(s.TakeN(9).Data(), []int{1, 2, 3, 4}) {
		t.Fatal("TakeN overflow wrong")
	}
	if !reflect.DeepEqual(s.SkipN(3).Data(), []int{4}) {
		t.Fatal("SkipN wrong")
	}
	if s.Len() != 4 {
		t.Fatal("TakeN/SkipN must not modify source")
	}
}

func TestUnionAndKeyBy(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3})
	s.Union(func(v int) any { return v }, []int{3, 4}, []int{4, 5})
	if !reflect.DeepEqual(s.Data(), []int{1, 2, 3, 4, 5}) {
		t.Fatalf("Union got %v", s.Data())
	}
	k := NewSlicer([]int{1, 2}).KeyBy(func(v int) any { return fmt.Sprint(v) })
	if k["2"] != 2 || len(k) != 2 {
		t.Fatal("KeyBy wrong")
	}
}

// Concurrency 改为快照执行后, 回调里再调本 Slicer 的方法不会死锁
func TestConcurrencyReentrant(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3})
	out := NewSlicer(make([]int, 0), true)
	done := make(chan struct{})
	go func() {
		s.Concurrency(func(v int) {
			out.Append(v) // 锁外快照执行, 不会死锁
		}, 2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Concurrency deadlock")
	}
	if out.Len() != 3 {
		t.Fatalf("Concurrency out len %d", out.Len())
	}
}

// Concurrency 中 f panic 应在调用方重新抛出, 而不是静默丢失或卡死
func TestConcurrencyPanicRethrow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("panic should be re-raised")
		}
	}()
	NewSlicer([]int{1, 2, 3}).Concurrency(func(v int) {
		panic("boom")
	}, 2)
}

func TestConcurrencyErr(t *testing.T) {
	s := NewSlicer([]int{1, 2, 3, 4, 5})
	var cnt int64
	errBoom := errors.New("boom")
	err := s.ConcurrencyErr(context.Background(), func(ctx context.Context, v int) error {
		atomic.AddInt64(&cnt, 1)
		if v == 3 {
			return errBoom
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	}, 2)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expect errBoom, got %v", err)
	}
	// ctx 取消
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.ConcurrencyErr(ctx, func(ctx context.Context, v int) error { return nil }, 2); err == nil {
		t.Fatal("cancelled ctx should return error")
	}
	// 正常无错
	if err := NewSlicer([]int{1, 2}).ConcurrencyErr(context.Background(), func(ctx context.Context, v int) error { return nil }, 2); err != nil {
		t.Fatalf("expect nil, got %v", err)
	}
}

func TestHelpers(t *testing.T) {
	if !reflect.DeepEqual(Flatten([][]int{{1}, {2, 3}, {}}), []int{1, 2, 3}) {
		t.Fatal("Flatten wrong")
	}
	z := Zip([]int{1, 2, 3}, []string{"a", "b"})
	if len(z) != 2 || z[0].A != 1 || z[1].B != "b" {
		t.Fatal("Zip wrong")
	}
	if !reflect.DeepEqual(Pluck([]int{1, 2}, func(v int) int { return v + 1 }), []int{2, 3}) {
		t.Fatal("Pluck wrong")
	}
}
