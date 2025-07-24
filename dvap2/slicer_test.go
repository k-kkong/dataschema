package dvap2

import "fmt"

// ExampleNewSlicer 示例
func ExampleNewSlicer() {

	// 使用案例
	type Item struct {
		ID    int
		Value int
		Name  string
	}
	input := []Item{
		{ID: 1, Value: 10, Name: "A"},
		{ID: 2, Value: 20, Name: "B"},
		{ID: 1, Value: 30, Name: "C"},
		{ID: 3, Value: 40, Name: "A"},
		{ID: 2, Value: 50, Name: "D"},
	}

	// 创建切片者对象
	slic := NewSlicer(input)
	slic.PopHead() // 删除并取出第一个元素
	slic.PopTail() // 删除并取出最后一个元素
	slic.PopTail() // 删除并取出最后一个元素
	slic.PopTail() // 删除并取出最后一个元素
	// 在末尾添加多个元素
	slic.Append(Item{ID: 4, Value: 60, Name: "E"}, Item{ID: 4, Value: 60, Name: "E"})
	// 在头部添加多个元素
	slic.Prepend(Item{ID: 5, Value: 70, Name: "F"}, Item{ID: 5, Value: 70, Name: "F"})
	// 升序
	slic.Sort(func(a, b Item) bool {
		return a.Name < b.Name
	})
	// 分页跳过3个元素，取10个元素，偏移后如果不够10个则能取多少就取多少
	slic.Page(3, 10)
	// 在指定位置插入
	slic.InsertIdx(2, Item{ID: 4, Value: 60, Name: "G"}, Item{ID: 4, Value: 60, Name: "G"})
	for _, m := range slic.Data() {
		fmt.Printf("ID: %d, Total Value: %d, Name: %s\n", m.ID, m.Value, m.Name)
	}

}

// ExampleMap Map方法示例
func ExampleMap() {

	// 使用案例
	type Item struct {
		ID    int
		Value int
		Name  string
	}
	input := []Item{
		{ID: 1, Value: 10, Name: "A"},
		{ID: 2, Value: 20, Name: "B"},
		{ID: 1, Value: 30, Name: "C"},
		{ID: 3, Value: 40, Name: "A"},
		{ID: 2, Value: 50, Name: "D"},
	}

	type Jtem struct {
		JtemName string
		ItemId   int
	}
	jtems := Map(input, func(i Item) (j Jtem) {
		return Jtem{
			JtemName: fmt.Sprintf("%s%s", "jtem-", i.Name),
			ItemId:   i.ID,
		}
	})
	for _, m := range jtems {
		fmt.Printf("JtemName: %s, ItemId: %d\n", m.JtemName, m.ItemId)
	}

}

// ExampleDuplicateMerge Map方法示例
func ExampleDuplicateMerge() {

	// 使用案例
	type Item struct {
		ID    int
		Value int
		Name  string
	}
	input := []Item{
		{ID: 1, Value: 10, Name: "A"},
		{ID: 2, Value: 20, Name: "B"},
		{ID: 1, Value: 30, Name: "C"},
		{ID: 3, Value: 40, Name: "A"},
		{ID: 2, Value: 50, Name: "D"},
	}

	type Jtem struct {
		JtemName string
		ItemId   int
	}
	// 将input id 相等的元素合并使其名称以 %s-%s 形式连接
	jtems := DuplicateMerge(input,
		func(i Item) any {
			return i.ID
		},
		func(i Item, acc Jtem) Jtem {
			return Jtem{
				JtemName: fmt.Sprintf("%s-%s", acc.JtemName, i.Name),
				ItemId:   i.ID,
			}
		})
	for _, m := range jtems {
		fmt.Printf("JtemName: %s, ItemId: %d\n", m.JtemName, m.ItemId)
	}
	/*
		JtemName: -A-C, ItemId: 1
		JtemName: -B-D, ItemId: 2
		JtemName: -A, ItemId: 3
	*/
}
