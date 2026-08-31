package dvap2

import (
	"reflect"
	"testing"

	"github.com/k-kkong/dataschema/bmap"
)

func TestGetKeys(t *testing.T) {
	parent := bmap.Parse([]map[string]any{
		{"id": 1, "body2": map[string]any{"bid": 11}},
		{"id": 2, "body2": map[string]any{"bid": 22}},
		{"id": 3, "body2": map[string]any{"bid": 11}},
		{"id": 4, "body2": map[string]any{"bid": ""}},
	})
	d := NewDataer()
	d.GetKeys(parent, "body2|bid")
	want := []string{"11", "22"}
	if !reflect.DeepEqual(d.Keys, want) {
		t.Fatalf("GetKeys got %v want %v", d.Keys, want)
	}
}

func TestHasOneJoinIndex(t *testing.T) {
	parents := bmap.Parse([]map[string]any{
		{"id": 1, "name": "a"},
		{"id": 2, "name": "b"},
		{"id": 3, "name": "c"},
	})
	children := bmap.Parse([]map[string]any{
		{"user_id": 2, "role": "x"},
		{"user_id": 1, "role": "y"},
		{"user_id": 1, "role": "should-not-take"},
	})
	meta := bmap.Parse(parents.Value())
	NewDataer().
		SetMeta(meta).
		SetJoinKeys("id", "user_id").
		SetSubGroup(children).
		HasOne(meta, "", "child")

	if got := meta.Get("0.child.role").String(); got != "y" {
		t.Fatalf("parent1 child role got %s", got)
	}
	if got := meta.Get("1.child.role").String(); got != "x" {
		t.Fatalf("parent2 child role got %s", got)
	}
	if meta.Get("2.child").IsExists() && meta.Get("2.child.role").String() != "" {
		t.Fatalf("parent3 should have empty child, got %s", meta.Get("2.child").String())
	}
}

func TestHasOneCustomCompare(t *testing.T) {
	parents := bmap.Parse([]map[string]any{
		{"id": 1, "flag": "keep"},
		{"id": 2, "flag": "skip"},
	})
	children := bmap.Parse([]map[string]any{
		{"user_id": 1, "role": "ok"},
		{"user_id": 2, "role": "no"},
	})
	meta := bmap.Parse(parents.Value())
	NewDataer().
		SetMeta(meta).
		SetCompareFunc(func(p, s *bmap.BMap) bool {
			return p.Get("flag").String() == "keep" &&
				p.Get("id").String() == s.Get("user_id").String()
		}).
		SetSubGroup(children).
		HasOne(meta, "", "child")

	if got := meta.Get("0.child.role").String(); got != "ok" {
		t.Fatalf("keep parent should match, got %s", got)
	}
	if meta.Get("1.child.role").String() != "" {
		t.Fatalf("skip parent should not match")
	}
}

func TestHasManyJoinIndex(t *testing.T) {
	parents := bmap.Parse([]map[string]any{
		{"id": 1},
		{"id": 2},
	})
	children := bmap.Parse([]map[string]any{
		{"user_id": 1, "role": "a"},
		{"user_id": 1, "role": "b"},
		{"user_id": 2, "role": "c"},
	})
	meta := bmap.Parse(parents.Value())
	NewDataer().
		SetMeta(meta).
		SetJoinKeys("id", "user_id").
		SetSubGroup(children).
		HasMany(meta, "", "children")

	if got := len(meta.Get("0.children").Array()); got != 2 {
		t.Fatalf("parent1 children len got %d", got)
	}
	if got := len(meta.Get("1.children").Array()); got != 1 {
		t.Fatalf("parent2 children len got %d", got)
	}
}

func TestHasOneNestedRepeatedPath(t *testing.T) {
	parents := bmap.Parse([]map[string]any{
		{"a": map[string]any{"a": map[string]any{"id": 1}}},
		{"a": map[string]any{"a": map[string]any{"id": 2}}},
	})
	children := bmap.Parse([]map[string]any{
		{"pid": 1, "name": "c1"},
		{"pid": 2, "name": "c2"},
	})
	meta := bmap.Parse(parents.Value())
	NewDataer().
		SetMeta(meta).
		SetJoinKeys("id", "pid").
		SetSubGroup(children).
		HasOne(meta, "", "a|a|child")

	if got := meta.Get("0.a.a.child.name").String(); got != "c1" {
		t.Fatalf("first nested child got %s", got)
	}
	if got := meta.Get("1.a.a.child.name").String(); got != "c2" {
		t.Fatalf("second nested child got %s", got)
	}
}

func TestHasOneSubModify(t *testing.T) {
	parents := bmap.Parse([]map[string]any{
		{"id": 1, "name": "a"},
	})
	children := bmap.Parse([]map[string]any{
		{"user_id": 1, "role": "x"},
	})
	meta := bmap.Parse(parents.Value())
	NewDataer().
		SetMeta(meta).
		SetJoinKeys("id", "user_id").
		SetSubModifyFunc(func(p, s *bmap.BMap) (*bmap.BMap, *bmap.BMap) {
			p.Set("tagged", true)
			s.Set("extra", 1)
			return p, s
		}).
		SetSubGroup(children).
		HasOne(meta, "", "child")

	if !meta.Get("0.tagged").Bool() {
		t.Fatal("parent should be tagged")
	}
	if meta.Get("0.child.extra").Int() != 1 {
		t.Fatal("child extra should be set")
	}
}

// TestComplexMultiLayerPipeRelations 覆盖多层 | 路径、多段关系连续挂载、
// 数组套数组、挂到上一跳结果上、自定义比较、SubModify、单对象根节点等组合。
func TestComplexMultiLayerPipeRelations(t *testing.T) {
	schools := bmap.Parse([]map[string]any{
		{
			"id":   1,
			"name": "一中",
			"campus": map[string]any{
				"code": "A",
				"addr": map[string]any{"city": "sz", "zone_id": 10},
			},
			"leader": map[string]any{"user_id": 101, "title": "校长"},
			"classes": []map[string]any{
				{
					"cid":     11,
					"name":    "1班",
					"monitor": map[string]any{"user_id": 201},
					"groups": []map[string]any{
						{"gid": 1101, "name": "一组", "mentor": map[string]any{"user_id": 301}},
						{"gid": 1102, "name": "二组", "mentor": map[string]any{"user_id": 302}},
					},
				},
				{
					"cid":     12,
					"name":    "2班",
					"monitor": map[string]any{"user_id": 202},
					"groups": []map[string]any{
						{"gid": 1201, "name": "一组", "mentor": map[string]any{"user_id": ""}},
					},
				},
			},
		},
		{
			"id":   2,
			"name": "二中",
			"campus": map[string]any{
				"code": "B",
				"addr": map[string]any{"city": "gz", "zone_id": 20},
			},
			"leader": map[string]any{"user_id": 102, "title": "校长"},
			"classes": []map[string]any{
				{
					"cid":     21,
					"name":    "1班",
					"monitor": map[string]any{"user_id": 201},
					"groups": []map[string]any{
						{"gid": 2101, "name": "一组", "mentor": map[string]any{"user_id": 301}},
					},
				},
			},
		},
		{
			"id":   3,
			"name": "空校",
			"campus": map[string]any{
				"code": "C",
				"addr": map[string]any{"city": "bj", "zone_id": 0},
			},
			"leader":  map[string]any{"user_id": 999},
			"classes": []map[string]any{},
		},
	})
	users := bmap.Parse([]map[string]any{
		{"id": 101, "name": "张校长"},
		{"id": 102, "name": "李校长"},
		{"id": 201, "name": "班长甲"},
		{"id": 202, "name": "班长乙"},
		{"id": 301, "name": "导师A"},
		{"id": 302, "name": "导师B"},
	})
	zones := bmap.Parse([]map[string]any{
		{"id": 10, "name": "南山"},
		{"id": 20, "name": "天河"},
	})
	categories := bmap.Parse([]map[string]any{
		{"code": "A", "label": "本部"},
		{"code": "B", "label": "分校"},
	})
	members := bmap.Parse([]map[string]any{
		{"id": 1, "gid": 1101, "name": "学生1"},
		{"id": 2, "gid": 1101, "name": "学生2"},
		{"id": 3, "gid": 1102, "name": "学生3"},
		{"id": 4, "gid": 2101, "name": "学生4"},
		{"id": 5, "gid": 2101, "name": "学生5"},
		{"id": 6, "gid": 2101, "name": "学生6"},
	})
	badges := bmap.Parse([]map[string]any{
		{"uid": 1, "icon": "gold"},
		{"uid": 4, "icon": "silver"},
	})
	subjects := bmap.Parse([]map[string]any{
		{"cid": 11, "name": "数学"},
		{"cid": 11, "name": "语文"},
		{"cid": 12, "name": "英语"},
		{"cid": 21, "name": "物理"},
	})

	meta := bmap.Parse(schools.Value())

	t.Run("GetKeys_deep_pipe_and_dedup", func(t *testing.T) {
		assertKeys := func(path string, want []string) {
			t.Helper()
			d := NewDataer()
			d.GetKeys(meta, path)
			if !reflect.DeepEqual(d.Keys, want) {
				t.Fatalf("GetKeys(%s) got %v want %v", path, d.Keys, want)
			}
		}
		assertKeys("leader|user_id", []string{"101", "102", "999"})
		assertKeys("classes|monitor|user_id", []string{"201", "202"})
		assertKeys("classes|groups|mentor|user_id", []string{"301", "302"})
		assertKeys("classes|groups|gid", []string{"1101", "1102", "1201", "2101"})
		assertKeys("campus|addr|zone_id", []string{"10", "20", "0"})
		assertKeys("classes|name", []string{"1班", "2班"})
		assertKeys("missing|foo", []string{})
		assertKeys("classes|groups|mentor|missing", []string{})

		// 与 RelationLoader 相同：关系名 classes|groups|members + fakey=gid
		// 实际挖 key 路径为 classes|groups|gid
		d := NewDataer()
		d.GetKeys(meta, "classes|groups"+"|"+"gid")
		if !reflect.DeepEqual(d.Keys, []string{"1101", "1102", "1201", "2101"}) {
			t.Fatalf("loader-style dig_key got %v", d.Keys)
		}
	})

	// 1) 挂到对象字段上：leader|user
	NewDataer().SetMeta(meta).SetJoinKeys("user_id", "id").
		SetSubModifyFunc(func(p, s *bmap.BMap) (*bmap.BMap, *bmap.BMap) {
			p.Set("loaded", true)
			return p, s
		}).
		SetSubGroup(users).
		HasOne(meta, "", "leader|user")

	// 2) 三层对象：campus|addr|zone
	NewDataer().SetMeta(meta).SetJoinKeys("zone_id", "id").
		SetSubGroup(zones).
		HasOne(meta, "", "campus|addr|zone")

	// 3) 同级另一条：campus|category
	NewDataer().SetMeta(meta).SetJoinKeys("code", "code").
		SetSubGroup(categories).
		HasOne(meta, "", "campus|category")

	// 4) 数组套对象：classes|monitor|user
	NewDataer().SetMeta(meta).SetJoinKeys("user_id", "id").
		SetSubGroup(users).
		HasOne(meta, "", "classes|monitor|user")

	// 5) 四层：classes|groups|mentor|profile
	NewDataer().SetMeta(meta).SetJoinKeys("user_id", "id").
		SetSubGroup(users).
		HasOne(meta, "", "classes|groups|mentor|profile")

	// 6) 数组套数组 HasMany + SubModify 计数
	NewDataer().SetMeta(meta).SetJoinKeys("gid", "gid").
		SetSubModifyFunc(func(p, s *bmap.BMap) (*bmap.BMap, *bmap.BMap) {
			p.Set("member_cnt", p.Get("member_cnt").Int()+1)
			return p, s
		}).
		SetSubGroup(members).
		HasMany(meta, "", "classes|groups|members")

	// 7) 挂到上一跳 HasMany 的结果上：classes|groups|members|badge
	NewDataer().SetMeta(meta).SetJoinKeys("id", "uid").
		SetSubGroup(badges).
		HasOne(meta, "", "classes|groups|members|badge")

	// 8) 自定义比较：只挂数学/物理
	NewDataer().SetMeta(meta).
		SetCompareFunc(func(p, s *bmap.BMap) bool {
			if p.Get("cid").String() == "" || p.Get("cid").String() != s.Get("cid").String() {
				return false
			}
			n := s.Get("name").String()
			return n == "数学" || n == "物理"
		}).
		SetSubGroup(subjects).
		HasMany(meta, "", "classes|subjects")

	t.Run("nested_has_one_and_empty_match", func(t *testing.T) {
		if got := meta.Get("0.leader.user.name").String(); got != "张校长" {
			t.Fatalf("school1 leader got %s", got)
		}
		if !meta.Get("0.leader.loaded").Bool() {
			t.Fatal("school1 leader should be tagged by SubModify")
		}
		if got := meta.Get("1.leader.user.name").String(); got != "李校长" {
			t.Fatalf("school2 leader got %s", got)
		}
		if meta.Get("2.leader.user.name").String() != "" {
			t.Fatalf("school3 unmatched leader user should be empty, got %s", meta.Get("2.leader.user").String())
		}
		if !meta.Get("2.leader.loaded").Bool() {
			t.Fatal("unmatched leader still goes through SubModify")
		}
	})

	t.Run("three_level_object_and_sibling_relation", func(t *testing.T) {
		if got := meta.Get("0.campus.addr.zone.name").String(); got != "南山" {
			t.Fatalf("school1 zone got %s", got)
		}
		if got := meta.Get("1.campus.addr.zone.name").String(); got != "天河" {
			t.Fatalf("school2 zone got %s", got)
		}
		if meta.Get("2.campus.addr.zone.name").String() != "" {
			t.Fatal("zone_id=0 should not match")
		}
		if got := meta.Get("0.campus.category.label").String(); got != "本部" {
			t.Fatalf("school1 category got %s", got)
		}
		if got := meta.Get("1.campus.category.label").String(); got != "分校" {
			t.Fatalf("school2 category got %s", got)
		}
		if meta.Get("2.campus.category.label").String() != "" {
			t.Fatal("unknown campus code C should not match")
		}
	})

	t.Run("array_object_and_four_level_pipe", func(t *testing.T) {
		if got := meta.Get("0.classes.0.monitor.user.name").String(); got != "班长甲" {
			t.Fatalf("class11 monitor got %s", got)
		}
		if got := meta.Get("0.classes.1.monitor.user.name").String(); got != "班长乙" {
			t.Fatalf("class12 monitor got %s", got)
		}
		if got := meta.Get("1.classes.0.monitor.user.name").String(); got != "班长甲" {
			t.Fatalf("shared monitor got %s", got)
		}
		if got := meta.Get("0.classes.0.groups.0.mentor.profile.name").String(); got != "导师A" {
			t.Fatalf("mentor profile got %s", got)
		}
		if got := meta.Get("0.classes.0.groups.1.mentor.profile.name").String(); got != "导师B" {
			t.Fatalf("mentor profile 2 got %s", got)
		}
		if meta.Get("0.classes.1.groups.0.mentor.profile.name").String() != "" {
			t.Fatal("empty mentor user_id should not match")
		}
		if got := meta.Get("1.classes.0.groups.0.mentor.profile.name").String(); got != "导师A" {
			t.Fatalf("shared mentor got %s", got)
		}
	})

	t.Run("has_many_then_has_one_on_attached_array", func(t *testing.T) {
		if got := len(meta.Get("0.classes.0.groups.0.members").Array()); got != 2 {
			t.Fatalf("group 1101 members got %d", got)
		}
		if got := meta.Get("0.classes.0.groups.0.member_cnt").Int(); got != 2 {
			t.Fatalf("group 1101 member_cnt got %d", got)
		}
		if got := len(meta.Get("0.classes.0.groups.1.members").Array()); got != 1 {
			t.Fatalf("group 1102 members got %d", got)
		}
		if got := len(meta.Get("0.classes.1.groups.0.members").Array()); got != 0 {
			t.Fatalf("group 1201 should have no members, got %d", got)
		}
		if got := len(meta.Get("1.classes.0.groups.0.members").Array()); got != 3 {
			t.Fatalf("group 2101 members got %d", got)
		}
		if got := meta.Get("0.classes.0.groups.0.members.0.badge.icon").String(); got != "gold" {
			t.Fatalf("student1 badge got %s", got)
		}
		if meta.Get("0.classes.0.groups.0.members.1.badge.icon").String() != "" {
			t.Fatal("student2 should have empty badge")
		}
		if got := meta.Get("1.classes.0.groups.0.members.0.badge.icon").String(); got != "silver" {
			t.Fatalf("student4 badge got %s", got)
		}
	})

	t.Run("custom_compare_filters_nested_has_many", func(t *testing.T) {
		s11 := meta.Get("0.classes.0.subjects").Array()
		if len(s11) != 1 || s11[0].Get("name").String() != "数学" {
			t.Fatalf("class11 subjects should only keep 数学, got %s", meta.Get("0.classes.0.subjects").String())
		}
		if got := len(meta.Get("0.classes.1.subjects").Array()); got != 0 {
			t.Fatalf("class12 英语 should be filtered, got %d", got)
		}
		s21 := meta.Get("1.classes.0.subjects").Array()
		if len(s21) != 1 || s21[0].Get("name").String() != "物理" {
			t.Fatalf("class21 subjects should only keep 物理, got %s", meta.Get("1.classes.0.subjects").String())
		}
		if got := len(meta.Get("2.classes").Array()); got != 0 {
			t.Fatalf("empty classes should stay empty, got %d", got)
		}
	})

	t.Run("single_object_root_with_pipe", func(t *testing.T) {
		one := bmap.Parse(map[string]any{
			"id":     1,
			"leader": map[string]any{"user_id": 101, "title": "校长"},
			"classes": []map[string]any{
				{"cid": 11, "groups": []map[string]any{
					{"gid": 1101, "mentor": map[string]any{"user_id": 301}},
				}},
			},
		})
		NewDataer().SetMeta(one).SetJoinKeys("user_id", "id").SetSubGroup(users).
			HasOne(one, "", "leader|user")
		NewDataer().SetMeta(one).SetJoinKeys("user_id", "id").SetSubGroup(users).
			HasOne(one, "", "classes|groups|mentor|profile")
		NewDataer().SetMeta(one).SetJoinKeys("gid", "gid").SetSubGroup(members).
			HasMany(one, "", "classes|groups|members")

		if got := one.Get("leader.user.name").String(); got != "张校长" {
			t.Fatalf("single root leader got %s", got)
		}
		if got := one.Get("classes.0.groups.0.mentor.profile.name").String(); got != "导师A" {
			t.Fatalf("single root mentor got %s", got)
		}
		if got := len(one.Get("classes.0.groups.0.members").Array()); got != 2 {
			t.Fatalf("single root members got %d", got)
		}

		keys := NewDataer()
		keys.GetKeys(one, "classes|groups|mentor|user_id")
		if !reflect.DeepEqual(keys.Keys, []string{"301"}) {
			t.Fatalf("single root GetKeys got %v", keys.Keys)
		}
	})

	t.Run("has_one_takes_first_has_many_keeps_all_on_pipe", func(t *testing.T) {
		parent := bmap.Parse([]map[string]any{
			{"body2": map[string]any{"bid": 11}},
			{"body2": map[string]any{"bid": 333}},
		})
		sub := bmap.Parse([]map[string]any{
			{"bid": "11", "type": "foo"},
			{"bid": "11", "type": "bar"},
			{"bid": "333", "type": "a"},
			{"bid": "333", "type": "b"},
		})
		one := bmap.Parse(parent.Value())
		NewDataer().SetMeta(one).SetJoinKeys("bid", "bid").SetSubGroup(sub).
			HasOne(one, "", "body2|ext")
		if got := one.Get("0.body2.ext.type").String(); got != "foo" {
			t.Fatalf("HasOne should take first match, got %s", got)
		}
		if got := one.Get("1.body2.ext.type").String(); got != "a" {
			t.Fatalf("HasOne second parent got %s", got)
		}

		many := bmap.Parse(parent.Value())
		NewDataer().SetMeta(many).SetJoinKeys("bid", "bid").SetSubGroup(sub).
			HasMany(many, "", "body2|ext")
		if got := len(many.Get("0.body2.ext").Array()); got != 2 {
			t.Fatalf("HasMany bid=11 should keep 2, got %d", got)
		}
		if got := len(many.Get("1.body2.ext").Array()); got != 2 {
			t.Fatalf("HasMany bid=333 should keep 2, got %d", got)
		}
	})
}

func benchParentsChildren(n int) (*bmap.BMap, *bmap.BMap) {
	parents := make([]map[string]any, n)
	children := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		parents[i] = map[string]any{"id": i, "name": "p"}
		children[i] = map[string]any{"user_id": i, "role": "r"}
	}
	return bmap.Parse(parents), bmap.Parse(children)
}

func BenchmarkHasOneIndex(b *testing.B) {
	p, c := benchParentsChildren(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		meta := bmap.Parse(p.Value())
		NewDataer().SetMeta(meta).SetJoinKeys("id", "user_id").SetSubGroup(c).HasOne(meta, "", "child")
	}
}

func BenchmarkHasOneScan(b *testing.B) {
	p, c := benchParentsChildren(2000)
	cf := func(p, s *bmap.BMap) bool {
		pv := p.Get("id").String()
		return pv != "" && pv == s.Get("user_id").String()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		meta := bmap.Parse(p.Value())
		NewDataer().SetMeta(meta).SetCompareFunc(cf).SetSubGroup(c).HasOne(meta, "", "child")
	}
}
