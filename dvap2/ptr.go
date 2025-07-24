package dvap2

// 指针
func Ptr[T any](v T) *T {
	return &v
}

// 指针取值
func PtrValue[T any](v *T) T {
	if v == nil {
		return *new(T)
	}
	return *v
}

// 数组转指针数组
func SliceOfPtrs[T any](vv ...T) []*T {
	slc := make([]*T, len(vv))
	for i := range vv {
		slc[i] = Ptr(vv[i])
	}
	return slc
}
