package gsave

import (
	"testing"
	"time"
)

// 测试用的用户模型
type UserModel struct {
	ID        int       `gorm:"column:id"`
	Name      string    `gorm:"column:name"`
	Age       int       `gorm:"column:age"`
	Height    float64   `gorm:"column:height"`
	IsActive  bool      `gorm:"column:is_active"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// 测试用的复杂类型
type Address struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

type UserWithAddress struct {
	ID      int     `gorm:"column:id"`
	Name    string  `gorm:"column:name"`
	Address Address `gorm:"column:address"`
}

func TestGetUpdateMapping(t *testing.T) {
	// 测试场景1：基础类型匹配
	t.Run("BasicTypeMatch", func(t *testing.T) {
		model := &UserModel{}
		quikSave := NewQuikSave(model)

		// 创建源数据，类型与模型匹配
		src := map[string]any{
			"id":        1,
			"name":      "John",
			"age":       30,
			"height":    1.75,
			"is_active": true,
		}

		mapping := quikSave.GetUpdateMapping(src)

		// 验证结果
		if val, ok := mapping["id"].(int); !ok || val != 1 {
			t.Errorf("Expected id=1, got %v", mapping["id"])
		}
		if val, ok := mapping["name"].(string); !ok || val != "John" {
			t.Errorf("Expected name=John, got %v", mapping["name"])
		}
		if val, ok := mapping["age"].(int); !ok || val != 30 {
			t.Errorf("Expected age=30, got %v", mapping["age"])
		}
		if val, ok := mapping["height"].(float64); !ok || val != 1.75 {
			t.Errorf("Expected height=1.75, got %v", mapping["height"])
		}
		if val, ok := mapping["is_active"].(bool); !ok || val != true {
			t.Errorf("Expected is_active=true, got %v", mapping["is_active"])
		}
	})

	// 测试场景2：类型不匹配但可转换
	t.Run("TypeConversion", func(t *testing.T) {
		model := &UserModel{}
		quikSave := NewQuikSave(model)

		// 创建源数据，类型与模型不匹配但可转换
		src := map[string]any{
			"id":        "2",         // string -> int
			"age":       float64(25), // float64 -> int
			"height":    "1.80",      // string -> float64
			"is_active": "true",      // string -> bool
		}

		mapping := quikSave.GetUpdateMapping(src)

		// 验证结果
		if val, ok := mapping["id"].(int); !ok || val != 2 {
			t.Errorf("Expected id=2 after conversion, got %v", mapping["id"])
		}
		if val, ok := mapping["age"].(int); !ok || val != 25 {
			t.Errorf("Expected age=25 after conversion, got %v", mapping["age"])
		}
		if val, ok := mapping["height"].(float64); !ok || val != 1.80 {
			t.Errorf("Expected height=1.80 after conversion, got %v", mapping["height"])
		}
		if val, ok := mapping["is_active"].(bool); !ok || val != true {
			t.Errorf("Expected is_active=true after conversion, got %v", mapping["is_active"])
		}
	})

	// 测试场景3：time.Time 类型转换
	t.Run("TimeTypeConversion", func(t *testing.T) {
		model := &UserModel{}
		quikSave := NewQuikSave(model)

		// 创建源数据，包含时间字符串
		now := time.Now()
		timeStr := now.Format(time.RFC3339)
		src := map[string]any{
			"created_at": timeStr,
		}

		mapping := quikSave.GetUpdateMapping(src)

		// 验证结果
		if val, ok := mapping["created_at"].(time.Time); !ok {
			t.Errorf("Expected created_at to be time.Time, got %T", mapping["created_at"])
		} else {
			// 验证时间是否接近
			if !val.Equal(now) && val.Format(time.RFC3339) != timeStr {
				t.Errorf("Expected created_at to be %s, got %s", timeStr, val.Format(time.RFC3339))
			}
		}
	})

	// 测试场景4：JSON 字符串输入
	t.Run("JSONStringInput", func(t *testing.T) {
		model := &UserModel{}
		quikSave := NewQuikSave(model)

		// 创建 JSON 字符串
		jsonStr := `{"id": 3, "name": "Alice", "age": 28}`

		mapping := quikSave.GetUpdateMapping(jsonStr)

		// 验证结果
		if val, ok := mapping["id"].(int); !ok || val != 3 {
			t.Errorf("Expected id=3, got %v", mapping["id"])
		}
		if val, ok := mapping["name"].(string); !ok || val != "Alice" {
			t.Errorf("Expected name=Alice, got %v", mapping["name"])
		}
		if val, ok := mapping["age"].(int); !ok || val != 28 {
			t.Errorf("Expected age=28, got %v", mapping["age"])
		}
	})

	// 测试场景5：复杂类型转换
	t.Run("ComplexTypeConversion", func(t *testing.T) {
		model := &UserWithAddress{}
		quikSave := NewQuikSave(model)

		// 创建包含复杂类型的源数据
		addressMap := map[string]any{"city": "Beijing", "country": "China"}
		src := map[string]any{
			"id":      4,
			"name":    "Bob",
			"address": addressMap,
		}

		mapping := quikSave.GetUpdateMapping(src)

		// 验证结果
		if val, ok := mapping["id"].(int); !ok || val != 4 {
			t.Errorf("Expected id=4, got %v", mapping["id"])
		}

		// 验证复杂类型是否正确转换
		if addrVal, ok := mapping["address"].(Address); !ok {
			t.Errorf("Expected address to be Address type, got %T", mapping["address"])
		} else {
			if addrVal.City != "Beijing" || addrVal.Country != "China" {
				t.Errorf("Expected address {city: Beijing, country: China}, got %v", addrVal)
			}
		}
	})
}

// Benchmark 测试
type BenchmarkUser struct {
	ID        int       `gorm:"column:id"`
	Name      string    `gorm:"column:name"`
	Age       int       `gorm:"column:age"`
	Height    float64   `gorm:"column:height"`
	IsActive  bool      `gorm:"column:is_active"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func BenchmarkGetUpdateMapping(b *testing.B) {
	model := &BenchmarkUser{}
	quikSave := NewQuikSave(model)

	src := map[string]any{
		"id":         1,
		"name":       "John Doe",
		"age":        30,
		"height":     1.75,
		"is_active":  true,
		"created_at": time.Now().Format(time.RFC3339),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		quikSave.GetUpdateMapping(src)
	}
}
