package dvap2

import (
	"fmt"
	"strings"

	"github.com/k-kkong/dataschema/bmap"
	// "github.com/k-kkong/dataschema/dvap"
	// "github.com/tidwall/gjson"
	// "github.com/tidwall/sjson"
)

type SubModifyFunc func(p, s *bmap.BMap) (*bmap.BMap, *bmap.BMap)

type CompareFun func(p, s *bmap.BMap) bool

// Dataer 数据连和处理者
type Dataer struct {
	CF       CompareFun
	Smf      SubModifyFunc
	SubGroup *bmap.BMap

	Meta *bmap.BMap //原始数据

	Keys    []string        //key
	Keysunq map[string]bool //去重
}

// SetMeta 设置要操作的原始数据即父数据 (json 字符串)
func (d *Dataer) SetMeta(meta *bmap.BMap) *Dataer {
	d.Meta = meta
	return d
}

// SetCompareFunc 设置比较函数 用于连接父数据和子数据的关键判断
func (d *Dataer) SetCompareFunc(cf CompareFun) *Dataer {
	d.CF = cf
	return d
}

// SetSubModifyFunc 设置子数据修改函数 可选，可以用于在连接时根据条件修改 子数据和父数据
func (d *Dataer) SetSubModifyFunc(smf SubModifyFunc) *Dataer {
	d.Smf = smf
	return d
}

// SetSubGroup 设置子数据
func (d *Dataer) SetSubGroup(subGroup *bmap.BMap) *Dataer {
	d.SubGroup = subGroup
	return d
}

// NewDataer 创建一个新的Dataer
func NewDataer() *Dataer {
	return &Dataer{

		Keys:    []string{},
		Keysunq: map[string]bool{},
	}
}

// GetResult 获取最终结果
func (d *Dataer) GetResult() *bmap.BMap {
	return d.Meta
}

// GetKeys 获取原属数据中 指定深度的key值，最终会得到一个数组
// - input *bmap.BMap 原始数据
// - dig_key string  要获取的key的深度参数 比如  body|bar 代表获取 input的body下的bar 的值列表
func (d *Dataer) GetKeys(input *bmap.BMap, dig_key string) *Dataer {

	relations := strings.Split(dig_key, "|")
	var _relatin_first = relations[0]

	if input.IsArray() {

		if len(relations) > 1 {
			for _, iv := range input.Array() {
				d.GetKeys(iv, dig_key)
			}
		} else {
			for _, iv := range input.Array() {
				_v := iv.Get(_relatin_first).String()

				// 过滤掉空的 键值
				if _v != "" {
					if _, ok := d.Keysunq[_v]; !ok {
						d.Keysunq[_v] = true
						d.Keys = append(d.Keys, _v)
					}
				}
			}
		}

	} else {

		if len(relations) > 1 {
			d.GetKeys(input.Get(_relatin_first), strings.TrimPrefix(dig_key, fmt.Sprintf("%s|", _relatin_first)))
		} else {
			_v := input.Get(_relatin_first).String()

			// 过滤掉空的 键值
			if _v != "" {
				if _, ok := d.Keysunq[_v]; !ok {
					d.Keysunq[_v] = true
					d.Keys = append(d.Keys, _v)
				}
			}

		}
	}

	return d
}

// HasOne 将subdata arry 中符合条件的单个元素，加入到parent 指定位置中
func (s *Dataer) HasOne(input *bmap.BMap, this_key, relation string) *Dataer {

	relations := strings.Split(relation, "|")
	var _relatin_first = relations[0]
	var w_key = this_key

	if input.IsArray() {

		for k, iv := range input.Array() {

			if this_key == "" {
				w_key = fmt.Sprintf("%d.%s", k, _relatin_first)
			} else {
				w_key = fmt.Sprintf("%s.%d.%s", this_key, k, _relatin_first)
			}

			// fmt.Println(k)
			if len(relations) > 1 {
				meta := iv.Get(_relatin_first)
				relation = strings.TrimPrefix(relation, fmt.Sprintf("%s|", _relatin_first))

				s.HasOne(meta, w_key, relation)
			} else {
				meta := iv
				//最后一个，直接比较
				// var match_v *bmap.BMap
				// SliceFind(s.SubGroup.Array(), &match_v, func(sv *bmap.BMap) bool {
				// 	return s.CF(meta, sv)
				// })
				match_v := NewSlicer(s.SubGroup.Array()).Take(func(b *bmap.BMap) bool {
					return s.CF(meta, b)
				})
				if match_v == nil {
					match_v = &bmap.BMap{}
				}

				if s.Smf != nil {

					_iv, _match_v := s.Smf(iv, match_v)
					match_v = _match_v

					// 先把对应的 数组的元素父值替换掉
					_iv_key := fmt.Sprintf("%s.%d", this_key, k)
					if this_key == "" {
						_iv_key = fmt.Sprintf("%d", k)
					}

					s.Meta.Set(_iv_key, _iv.Value())

					// s.Meta = _meta.String()
				}

				// fmt.Println(w_key)
				// fmt.Println(match_v.String())

				s.Meta.Set(w_key, match_v.Value())

				// fmt.Println(match_v.String())
			}

		}

	} else {
		if this_key == "" {
			w_key = relations[0]
		} else {
			w_key = fmt.Sprintf("%s.%s", this_key, _relatin_first)
		}

		iv := input
		if len(relations) > 1 {
			relation = strings.TrimPrefix(relation, fmt.Sprintf("%s|", _relatin_first))
			meta := input.Get(_relatin_first)
			s.HasOne(meta, w_key, relation)
		} else {
			//最后一个，直接比较
			match_v := NewSlicer(s.SubGroup.Array()).Take(func(b *bmap.BMap) bool {
				return s.CF(iv, b)
			})
			if match_v == nil {
				match_v = &bmap.BMap{}
			}

			if s.Smf != nil {
				_iv, _match_v := s.Smf(iv, match_v)

				// 先把对应的 数组的元素父值替换掉
				if this_key == "" {
					s.Meta = _iv
				} else {
					_iv_key := this_key
					s.Meta.Set(_iv_key, _iv.Value())
				}
				match_v = _match_v
			}

			// fmt.Println(w_key)
			// fmt.Println(match_v.String())
			// s.Meta = VSSetV(s.Meta, match_v.Value(), w_key)
			s.Meta.Set(w_key, match_v.Value())
		}

	}

	return s

}

// HasMany 将subdata arry 中符合条件的多个元素，加入到parent 指定位置中
func (s *Dataer) HasMany(input *bmap.BMap, this_key, relation string) *Dataer {

	relations := strings.Split(relation, "|")
	var _relatin_first = relations[0]
	var w_key = this_key

	if input.IsArray() {

		for k, iv := range input.Array() {

			if this_key == "" {
				w_key = fmt.Sprintf("%d.%s", k, _relatin_first)
			} else {
				w_key = fmt.Sprintf("%s.%d.%s", this_key, k, _relatin_first)
			}
			if len(relations) > 1 {
				meta := iv.Get(_relatin_first)
				relation = strings.TrimPrefix(relation, fmt.Sprintf("%s|", _relatin_first))
				s.HasMany(meta, w_key, relation)
			} else {
				// meta := iv
				// //最后一个，直接比较
				var filter = make([]interface{}, 0)
				for _, sv := range s.SubGroup.Array() {
					if s.CF(iv, sv) {
						if s.Smf != nil {
							_iv, _sv := s.Smf(iv, sv)
							_iv_key := fmt.Sprintf("%s.%d", this_key, k)
							if this_key == "" {
								_iv_key = fmt.Sprintf("%d", k)
							}

							iv = _iv
							// s.Meta, _ = sjson.Set(s.Meta, _iv_key, _iv.Value())
							s.Meta.Set(_iv_key, _iv.Value())

							filter = append(filter, _sv.Value())
						} else {
							filter = append(filter, sv.Value())
						}
					}
				}

				// s.Meta, _ = sjson.Set(s.Meta, w_key, filter)
				s.Meta.Set(w_key, filter)
			}

		}

	} else {
		if this_key == "" {
			w_key = relations[0]
		} else {
			w_key = fmt.Sprintf("%s.%s", this_key, _relatin_first)
		}

		iv := input
		if len(relations) > 1 {
			relation = strings.TrimPrefix(relation, fmt.Sprintf("%s|", _relatin_first))
			iv := input.Get(_relatin_first)
			s.HasMany(iv, w_key, relation)
		} else {

			//最后一个，直接比较
			var filter = make([]interface{}, 0)
			for _, sv := range s.SubGroup.Array() {
				if s.CF(iv, sv) {
					if s.Smf != nil {
						_iv, _sv := s.Smf(iv, sv)

						// 先把对应的 数组的元素父值替换掉
						if this_key == "" {
							s.Meta = _iv
						} else {
							_iv_key := this_key
							s.Meta.Set(_iv_key, _iv.Value())
						}

						// s.Meta = _meta.String()
						filter = append(filter, _sv.Value())
					} else {
						filter = append(filter, sv.Value())
					}
				}
			}

			s.Meta.Set(w_key, filter)
		}

	}

	return s

}
