package bmap

import (
	"fmt"
	"reflect"
	"time"
)

// timeLayouts Time() 依次尝试的时间布局。
//
// 顺序是有讲究的，改动前先想清楚：
// 常用的、歧义小的排在前面，第一个解析成功的就返回；
// 带日期又带时间的必须排在「只有日期」或「只有时间」的前面，
// 否则 "2026-10-09 12:30:45" 会被更短更宽松的布局截走，只剩下半截信息。
var timeLayouts = []string{
	time.DateTime, // 2006-01-02 15:04:05
	time.DateOnly, // 2006-01-02
	time.RFC3339,
	time.RFC3339Nano,
	`2006-01-02 15:04:05 -0700 MST`,
	time.TimeOnly, // 15:04:05
	time.ANSIC,
	time.UnixDate,
	time.RubyDate,
	time.RFC822,
	time.RFC822Z,
	time.RFC850,
	time.RFC1123,
	time.RFC1123Z,
	time.Kitchen,
	time.Stamp,
	time.StampMilli,
	time.StampMicro,
	time.StampNano,
	time.Layout,
}

// Time 按常见布局解析出时间，认不出来就是零值。
//
// 依次尝试 timeLayouts 里的布局，第一个成功的就返回；
// 解析按本地时区进行，所以不带时区信息的文本（"2026-10-09 12:30:45"）
// 会被当成本地时间。
//
// 底层本来就是 time.Time 时直接拿来用，不走文本解析。
// 文本两端带着引号时会先摘掉——数据库里存的 JSON 片段常是这个样子。
//
// 要区分「时间就是零值」和「这段文本认不出来」，用 TimeE。
func (bm *BMap) Time() time.Time {
	t, _ := bm.TimeE()
	return t
}

// TimeE 与 Time 一样按布局链解析，区别是解析失败会返回 error。
//
// 零值时间在业务上是有意义的（表示「没有设置」），
// 光看 Time() 的结果分不清是「本来就是零值」还是「格式没认出来」，
// 需要区分的场合用这个方法。
func (bm *BMap) TimeE() (time.Time, error) {
	if t, ok := bm.timeValue(); ok {
		return t, nil
	}

	str := trimQuotes(bm.String())
	if str == "" {
		return time.Time{}, fmt.Errorf("值为空，解析不成时间")
	}

	var lastErr error
	for _, layout := range timeLayouts {
		t, err := time.ParseInLocation(layout, str, time.Local)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, fmt.Errorf("认不出 %q 是哪种时间格式，最后一次的错误：%w", str, lastErr)
}

// TimeLayout 用指定的布局解析时间，认不出来就是零值。
//
// 数据源的时间格式是已知的时候用它，比 Time() 挨个试要快，也更明确。
// 解析按本地时区进行。
func (bm *BMap) TimeLayout(layout string) time.Time {
	t, _ := bm.TimeLayoutE(layout)
	return t
}

// TimeLayoutE 与 TimeLayout 一样，区别是解析失败会返回 error
func (bm *BMap) TimeLayoutE(layout string) (time.Time, error) {
	if t, ok := bm.timeValue(); ok {
		return t, nil
	}
	return time.ParseInLocation(layout, trimQuotes(bm.String()), time.Local)
}

// timeValue 底层本来就是 time.Time 时直接拿出来，不走文本解析
func (bm *BMap) timeValue() (time.Time, bool) {
	v := deref(bm.rvalue)
	if !v.IsValid() || v.Kind() != reflect.Struct || v.Type() != typeTime || !v.CanInterface() {
		return time.Time{}, false
	}
	return v.Interface().(time.Time), true
}

// UnixTime 把值当秒级时间戳解释。
//
// 数据库里的 int 列常存着时间戳，所以单独给一个方法，
// 而不是让 Time() 去猜一个整数到底是不是时间戳——
// Time() 在数字上仍然返回零值，不会自己发挥。
//
// 值为 0 时返回零值时间，因为分不清「就是 1970 年」和「根本没填」。
func (bm *BMap) UnixTime() time.Time {
	n := bm.Int64()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0).In(time.Local)
}

// UnixMilliTime 把值当毫秒级时间戳解释，其余规则与 UnixTime 一致
func (bm *BMap) UnixMilliTime() time.Time {
	n := bm.Int64()
	if n == 0 {
		return time.Time{}
	}
	return time.UnixMilli(n).In(time.Local)
}
