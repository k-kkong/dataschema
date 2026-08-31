package dvap2

import (
	"strconv"
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

	Keys    []string            //key
	Keysunq map[string]struct{} //去重

	// 默认等值关联时使用索引，避免 HasOne/HasMany 对每个父元素线性扫描全部子元素
	joinFaKey string
	joinSuKey string

	subArr       []*bmap.BMap
	subPrepared  bool
	subIndexOne  map[string]*bmap.BMap
	subIndexMany map[string][]*bmap.BMap
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
	d.subArr = nil
	d.subPrepared = false
	d.subIndexOne = nil
	d.subIndexMany = nil
	return d
}

// SetJoinKeys 设置默认等值关联键，启用子数据索引匹配（O(n+m)）。
// 自定义 CompareFunc 时不要调用，否则会跳过自定义比较逻辑。
func (d *Dataer) SetJoinKeys(faKey, suKey string) *Dataer {
	d.joinFaKey = faKey
	d.joinSuKey = suKey
	d.subIndexOne = nil
	d.subIndexMany = nil
	return d
}

// NewDataer 创建一个新的Dataer
func NewDataer() *Dataer {
	return &Dataer{

		Keys:    make([]string, 0, 10),
		Keysunq: map[string]struct{}{},
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
	if d.Keysunq == nil {
		d.Keysunq = make(map[string]struct{})
	}
	d.getKeys(input, strings.Split(dig_key, "|"))
	return d
}

func (d *Dataer) getKeys(input *bmap.BMap, parts []string) {
	if input == nil {
		return
	}
	if input.IsArray() {
		for _, iv := range input.Array() {
			d.getKeys(iv, parts)
		}
		return
	}
	if len(parts) > 1 {
		d.getKeys(input.Get(parts[0]), parts[1:])
		return
	}
	_v := input.Get(parts[0]).String()
	if _v == "" {
		return
	}
	if _, ok := d.Keysunq[_v]; ok {
		return
	}
	d.Keysunq[_v] = struct{}{}
	d.Keys = append(d.Keys, _v)
}

func (s *Dataer) prepareSub() {
	if s.subPrepared {
		return
	}
	s.subPrepared = true
	if s.SubGroup == nil {
		s.subArr = []*bmap.BMap{}
		return
	}
	s.subArr = s.SubGroup.Array()
}

func (s *Dataer) ensureSubIndex(many bool) {
	s.prepareSub()
	if s.joinSuKey == "" {
		return
	}
	if many {
		if s.subIndexMany != nil {
			return
		}
		idx := make(map[string][]*bmap.BMap, len(s.subArr))
		for _, sv := range s.subArr {
			k := sv.Get(s.joinSuKey).String()
			if k == "" {
				continue
			}
			idx[k] = append(idx[k], sv)
		}
		s.subIndexMany = idx
		return
	}
	if s.subIndexOne != nil {
		return
	}
	idx := make(map[string]*bmap.BMap, len(s.subArr))
	for _, sv := range s.subArr {
		k := sv.Get(s.joinSuKey).String()
		if k == "" {
			continue
		}
		if _, ok := idx[k]; !ok {
			idx[k] = sv
		}
	}
	s.subIndexOne = idx
}

func (s *Dataer) matchOne(parent *bmap.BMap) *bmap.BMap {
	if s.joinFaKey != "" {
		s.ensureSubIndex(false)
		pVal := parent.Get(s.joinFaKey).String()
		if pVal == "" {
			return nil
		}
		return s.subIndexOne[pVal]
	}
	s.prepareSub()
	if s.CF == nil {
		return nil
	}
	for _, b := range s.subArr {
		if s.CF(parent, b) {
			return b
		}
	}
	return nil
}

func (s *Dataer) matchMany(parent *bmap.BMap) []*bmap.BMap {
	if s.joinFaKey != "" {
		s.ensureSubIndex(true)
		pVal := parent.Get(s.joinFaKey).String()
		if pVal == "" {
			return nil
		}
		return s.subIndexMany[pVal]
	}
	s.prepareSub()
	if s.CF == nil {
		return nil
	}
	out := make([]*bmap.BMap, 0)
	for _, b := range s.subArr {
		if s.CF(parent, b) {
			out = append(out, b)
		}
	}
	return out
}

func pathIndexField(prefix string, idx int, field string) string {
	n := strconv.Itoa(idx)
	if prefix == "" {
		return n + "." + field
	}
	return prefix + "." + n + "." + field
}

func pathIndex(prefix string, idx int) string {
	if prefix == "" {
		return strconv.Itoa(idx)
	}
	return prefix + "." + strconv.Itoa(idx)
}

func pathField(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

func emptyBMap(v *bmap.BMap) *bmap.BMap {
	if v == nil {
		return &bmap.BMap{}
	}
	return v
}

// HasOne 将subdata arry 中符合条件的单个元素，加入到parent 指定位置中
func (s *Dataer) HasOne(input *bmap.BMap, this_key, relation string) *Dataer {
	s.hasOne(input, this_key, strings.Split(relation, "|"))
	return s
}

func (s *Dataer) hasOne(input *bmap.BMap, this_key string, relations []string) {
	rel0 := relations[0]
	rest := relations[1:]
	last := len(rest) == 0

	if input.IsArray() {
		for k, iv := range input.Array() {
			wKey := pathIndexField(this_key, k, rel0)
			if !last {
				s.hasOne(iv.Get(rel0), wKey, rest)
				continue
			}
			matchV := emptyBMap(s.matchOne(iv))
			if s.Smf != nil {
				_iv, _matchV := s.Smf(iv, matchV)
				matchV = _matchV
				s.Meta.Set(pathIndex(this_key, k), _iv.Value())
			}
			s.Meta.Set(wKey, matchV.Value())
		}
		return
	}

	wKey := pathField(this_key, rel0)
	if !last {
		s.hasOne(input.Get(rel0), wKey, rest)
		return
	}
	matchV := emptyBMap(s.matchOne(input))
	if s.Smf != nil {
		_iv, _matchV := s.Smf(input, matchV)
		if this_key == "" {
			s.Meta = _iv
		} else {
			s.Meta.Set(this_key, _iv.Value())
		}
		matchV = _matchV
	}
	s.Meta.Set(wKey, matchV.Value())
}

// HasMany 将subdata arry 中符合条件的多个元素，加入到parent 指定位置中
func (s *Dataer) HasMany(input *bmap.BMap, this_key, relation string) *Dataer {
	s.hasMany(input, this_key, strings.Split(relation, "|"))
	return s
}

func (s *Dataer) hasMany(input *bmap.BMap, this_key string, relations []string) {
	rel0 := relations[0]
	rest := relations[1:]
	last := len(rest) == 0

	if input.IsArray() {
		for k, iv := range input.Array() {
			wKey := pathIndexField(this_key, k, rel0)
			if !last {
				s.hasMany(iv.Get(rel0), wKey, rest)
				continue
			}
			matches := s.matchMany(iv)
			filter := make([]interface{}, 0, len(matches))
			for _, sv := range matches {
				if s.Smf != nil {
					_iv, _sv := s.Smf(iv, sv)
					iv = _iv
					s.Meta.Set(pathIndex(this_key, k), _iv.Value())
					filter = append(filter, _sv.Value())
				} else {
					filter = append(filter, sv.Value())
				}
			}
			s.Meta.Set(wKey, filter)
		}
		return
	}

	wKey := pathField(this_key, rel0)
	if !last {
		s.hasMany(input.Get(rel0), wKey, rest)
		return
	}
	matches := s.matchMany(input)
	filter := make([]interface{}, 0, len(matches))
	for _, sv := range matches {
		if s.Smf != nil {
			_iv, _sv := s.Smf(input, sv)
			if this_key == "" {
				s.Meta = _iv
			} else {
				s.Meta.Set(this_key, _iv.Value())
			}
			filter = append(filter, _sv.Value())
		} else {
			filter = append(filter, sv.Value())
		}
	}
	s.Meta.Set(wKey, filter)
}
