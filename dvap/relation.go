package dvap

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

// 关系类型
type RELATION_TYPE = int

const (
	//一对一或多对一，父元素是1个或者多个，每个父元素里有一个子元素
	// 比如：
	/*
		{
			"p_name": "父亲",
			"p_age": "年龄",
			"one_child": {
				"s_name": "子名",
				"c_age": "年龄"
			}
		}
	*/
	// 或者
	/*
		[
			{
				"p_name": "父亲1",
				"p_age": "年龄",
				"one_child": {
					"s_name": "子名1",
					"c_age": "年龄"
				}
			},
			{
				"p_name": "父亲2",
				"p_age": "年龄",
				"one_child": {
					"s_name": "子名2",
					"c_age": "年龄"
				}
			}
		]
	*/
	HAS_ONE RELATION_TYPE = iota

	//加载多个子对象/数组  一对多或多对多，父元素是1个或者多个，每个父元素里有多个子元素
	// 比如 :
	/*
		{
		    "p_name": "父亲",
		    "p_age": "年龄",
		    "children": [
		        {
		            "s_name": "孩子1",
		            "c_age": "年龄"
		        },
		        {
		            "s_name": "孩子2",
		            "c_age": "年龄"
		        }
		    ]
		}
	*/
	// 或者
	/*
		[
		    {
		        "p_name": "父亲1",
		        "p_age": "年龄",
		        "children": [
		            {
		                "s_name": "孩子1",
		                "c_age": "年龄"
		            },
		            {
		                "s_name": "孩子2",
		                "c_age": "年龄"
		            }
		        ]
		    },
		    {
		        "p_name": "父亲2",
		        "p_age": "年龄",
		        "children": [
		            {
		                "s_name": "儿子1",
		                "c_age": "年龄"
		            },
		            {
		                "s_name": "女儿1",
		                "c_age": "年龄"
		            }
		        ]
		    }
		]
	*/
	HAS_MANY

	//直接加载数据给到父节点 一对一
	BELONG_TO
)

type RELARTION_NET map[string]*RelationLoader

// 关系网络
type RelationLoader struct {
	input         interface{}   //输入
	result        interface{}   //输出
	childModel    interface{}   //子模型
	compareFunc   CompareFun    //比较方法
	fakey         string        //父
	sukey         string        //子
	cdb           *gorm.DB      //条件db
	relation_type RELATION_TYPE //关系
	err           error         //错误
	subModifyFunc SubModifyFunc //子数据修改

	Stash             RELARTION_NET //关系网
	OpenPrintErrStack bool          //是否开启打印错误堆栈
}

// GetInput 获取输入
func (r *RelationLoader) GetInput() interface{} {
	return r.input
}

// GetResult 获取输出
func (r *RelationLoader) GetResult() interface{} {
	return r.result
}

// Error 获取错误
func (r *RelationLoader) Error() error {
	return r.err
}

// NewRelationLoader 初始化关系网络
func NewRelationLoader(input interface{}, OpenPrintErrStack bool) *RelationLoader {
	return &RelationLoader{
		input:             input,
		Stash:             RELARTION_NET{},
		OpenPrintErrStack: OpenPrintErrStack,
	}
}

// AddRelationWithOptions 在关系网络上添加一个关系
func (r *RelationLoader) AddRelationWithOptions(opt *RelationOptions) *RelationLoader {
	var err error
	func(err *error) {
		defer catch(err, r.OpenPrintErrStack)
		rls := strings.Split(opt.Relation, ".")
		new_sr := &RelationLoader{
			childModel:    opt.Child,
			fakey:         opt.Fakey,
			sukey:         opt.Sukey,
			relation_type: opt.RelationType,
		}

		if opt.CompareFunc != nil {
			new_sr.compareFunc = opt.CompareFunc
		}
		if opt.Cdb != nil {
			new_sr.cdb = opt.Cdb
		}
		if opt.SubModifyFunc != nil {
			new_sr.subModifyFunc = opt.SubModifyFunc
		}

		r.setRelation(rls, new_sr)
	}(&err)
	if err != nil {
		r.err = err
	}
	return r
}

// AddRelation 在关系网络上添加一个关系
// relation_type 关系类型 HAS_ONE HAS_MANY BELONG_TO
// relation 关系 父.子.子.子...
// fakey 父级数据对应的key 上一级数据表里对应的关联字段
// sukey 子级数据对应的key 当前级数据表里对应的关联字段
// Child 子数据表的模型model
// compareFunc 自定义比较函数 用于连接父数据和子数据的关键判断 非必填
// cdb 自定义查询子数据的条件db 非必填
// anonps 自定义父子数据修改，在连接数据的时候会调用  func(p, s gjson.Result) (gjson.Result, gjson.Result) 非必填
func (r *RelationLoader) AddRelation(relation_type RELATION_TYPE, relation, fakey, sukey string,
	Child interface{}, compareFunc CompareFun, cdb *gorm.DB, anonps ...interface{}) *RelationLoader {
	var err error
	func(err *error) {
		defer catch(err, r.OpenPrintErrStack)
		rls := strings.Split(relation, ".")
		new_sr := &RelationLoader{
			childModel:    Child,
			compareFunc:   compareFunc,
			cdb:           cdb,
			fakey:         fakey,
			sukey:         sukey,
			relation_type: relation_type,
		}
		if len(anonps) > 0 {
			sf, ok := anonps[0].(func(p, s gjson.Result) (gjson.Result, gjson.Result))
			if ok {
				new_sr.subModifyFunc = sf
			}
		}
		r.setRelation(rls, new_sr)
	}(&err)
	if err != nil {
		r.err = err
	}
	return r
}

func (r *RelationLoader) setRelation(relations []string, sr *RelationLoader) *RelationLoader {
	if len(relations) > 1 {
		_r, ok := r.Stash[relations[0]]
		if !ok {
			panic(fmt.Sprintf("欲设置的前置关系[%s]不存在[%s]", relations[0], relations))
		}
		r = _r.setRelation(relations[1:], sr)
	} else {
		if r.Stash == nil {
			r.Stash = RELARTION_NET{}
		}
		r.Stash[relations[0]] = sr
	}
	return r
}

// LoadResult 加载关系数据
func (r *RelationLoader) LoadResult(db *gorm.DB) *RelationLoader {
	var err error
	func(err *error) {
		defer catch(err, r.OpenPrintErrStack)
		r.load(db)
	}(&err)
	if err != nil {
		r.err = err
	}
	return r
}

func (r *RelationLoader) load(db *gorm.DB) {

	input_v := VtoJson(r.input)
	r.result = r.input
	//取key
	var fakeys = map[string][]string{}
	for rk, rv := range r.Stash {

		_dataer := &Dataer{
			Keysunq: map[string]bool{},
		}
		dig_rks := strings.Split(rk, "|")
		dig_key := rv.fakey
		if len(dig_rks) > 1 {
			dig_key = fmt.Sprintf("%s|%s",
				strings.Join(dig_rks[:len(dig_rks)-1], "|"),
				dig_key)
		}
		_dataer.GetKeys(input_v, dig_key)
		fakeys[rk] = _dataer.Keys
	}
	//加载子项
	// var subcollect = map[string]interface{}{}
	for rk, keys := range fakeys {

		rv := r.Stash[rk]
		rv_mt_slice_t := reflect.SliceOf(reflect.TypeOf(rv.childModel))
		rv_silce := reflect.New(rv_mt_slice_t).Interface()

		rvSliceType := reflect.SliceOf(reflect.TypeOf(rv.childModel))
		rvSlicePtr := reflect.New(rvSliceType)

		subcq := db
		if rv.cdb != nil {
			subcq = rv.cdb
		} else {
			subcq = subcq.Model(rv.childModel)
		}
		if len(keys) > 0 {
			subcq.Where(rv.sukey+" in ?", keys).Find(rvSlicePtr.Interface())
			rv_silce = rvSlicePtr.Elem().Interface()

			// rows, err := subcq.Where(rv.sukey+" in ?", keys).
			// 	Rows()
			// if err != nil {
			// 	fmt.Println(err)
			// }
			// defer rows.Close()
			// for rows.Next() {
			// 	element := reflect.New(reflect.TypeOf(rv.childModel)).Interface()
			// 	db.ScanRows(rows, element)
			// 	reflect.ValueOf(rv_silce).Elem().Set(reflect.Append(reflect.ValueOf(rv_silce).Elem(),
			// 		reflect.ValueOf(element).Elem()))
			// }
		}

		//填入结果
		if len(rv.Stash) > 0 {
			rv.input = rv_silce
			rv.load(db)
		} else {
			rv.result = rv_silce
		}

		//生成结果
		if rv.compareFunc == nil {
			rv.compareFunc = func(p, s gjson.Result) bool {
				return p.Get(rv.fakey).String() != "" && p.Get(rv.fakey).String() == s.Get(rv.sukey).String()
			}
		}

		r_rv := VtoJson(r.result)   //父集
		rv_rv := VtoJson(rv.result) //子集
		// dataer := NewDataer(r_rv.String(), rv.compareFunc, rv.subModifyFunc, rv_rv)
		dataer := NewDataer().
			SetMeta(r_rv.String()).
			SetCompareFunc(rv.compareFunc).
			SetSubModifyFunc(rv.subModifyFunc).
			SetSubGroup(rv_rv)

		switch rv.relation_type {
		case HAS_ONE:
			r.result = dataer.HasOne(r_rv, "", rk).GetResult()
			// r.result, _ = HasOneV2(r.result, rv.result, rk, rv.compareFunc, rv.subModifyFunc)
		case HAS_MANY:
			r.result = dataer.HasMany(r_rv, "", rk).GetResult()
			// r.result, _ = HasManyV2(r.result, rv.result, rk, rv.compareFunc, rv.subModifyFunc)
		case BELONG_TO:
			r.result = BelongTo(r.result, rv.result, rk)
		default:
		}
	}

}

// RelationOptions 关系选项
type RelationOptions struct {
	RelationType  RELATION_TYPE //必填 父子关系类型
	Relation      string        //必填 父子关系名
	Fakey         string        //必填 父元素对应的key
	Sukey         string        //必填 子元素对应的key
	Child         interface{}   //必填 子元素模型
	CompareFunc   CompareFun    //可选 自定义父子映射函数
	Cdb           *gorm.DB      //可选 自定义查询子数据的条件db
	SubModifyFunc SubModifyFunc //可选 自定义 父子数据修改函数
}

func NewRelationOptions() *RelationOptions {
	return &RelationOptions{}
}

// SetRelationType 设置关系类型
func (r *RelationOptions) SetRelationType(relation_type RELATION_TYPE) *RelationOptions {
	r.RelationType = relation_type
	return r
}

// SetRelation 设置关系名
func (r *RelationOptions) SetRelation(relation string) *RelationOptions {
	r.Relation = relation
	return r
}

// SetFakey 设置父级数据对应的key
func (r *RelationOptions) SetFakey(fakey string) *RelationOptions {
	r.Fakey = fakey
	return r
}

// SetSukey 设置子级数据对应的key
func (r *RelationOptions) SetSukey(sukey string) *RelationOptions {
	r.Sukey = sukey
	return r
}

// SetChild 设置子数据的model 一般为gorm 对应的struct
func (r *RelationOptions) SetChild(child interface{}) *RelationOptions {
	r.Child = child
	return r
}

// SetCompareFunc 设置自定义比较函数 用于连接父数据和子数据的关键判断
func (r *RelationOptions) SetCompareFunc(compareFunc CompareFun) *RelationOptions {
	r.CompareFunc = compareFunc
	return r
}

// SetCdb 设置自定义查询子数据的条件db
func (r *RelationOptions) SetCdb(cdb *gorm.DB) *RelationOptions {
	r.Cdb = cdb
	return r
}

// SetSubModifyFunc 设置自定义父子数据修改
// 该方法在 最终数据连接的时候执行，用于对父数据或者子数据自定义增加减少字段或者修改字段等，内容可高度自定义
// subModifyFunc(p, s gjson.Result) (gjson.Result, gjson.Result)
// p是父数据 s是子数据 返回 p,s 父子数据，即内容可以自定义修改，但是不要丢失对应的映射key
func (r *RelationOptions) SetSubModifyFunc(subModifyFunc SubModifyFunc) *RelationOptions {
	r.SubModifyFunc = subModifyFunc
	return r
}
