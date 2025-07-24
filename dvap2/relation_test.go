package dvap2

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"gorm.io/gorm"
)

// 用户
type TblUser struct {
	Id   int    `json:"id" gorm:"column:id"`
	Name string `json:"name" gorm:"column:name"`
}

func (u TblUser) TableName() string {
	return "user"
}

// 学生
type TblStudent struct {
	Id      int    `json:"id" gorm:"column:id"`
	ClassId string `json:"class_id" gorm:"column:class_id"` //班级
	UserId  int    `json:"user_id" gorm:"column:user_id"`   // 用户ID
}

func (s TblStudent) TableName() string {
	return "student"
}

// 老师
type TblTeacher struct {
	Id      int    `json:"id" gorm:"column:id"`
	ClassId string `json:"class_id" gorm:"column:class_id"` //班级
	UserId  int    `json:"user_id" gorm:"column:user_id"`   // 用户ID
	Subject string `json:"subject" gorm:"column:subject"`   // 科目
}

func (t TblTeacher) TableName() string {
	return "teacher"
}

// 班级
type TblClass struct {
	Id   int    `json:"id" gorm:"column:id"`
	Name string `json:"name" gorm:"column:name"`
}

func (c TblClass) TableName() string {
	return "class"
}

// 加载多个表或者多个结构时间的嵌套关系
func ExampleNewRelationLoader() {

	// 首先你有这些表
	// 用户
	type TblUser struct {
		Id   int    `json:"id" gorm:"column:id"`
		Name string `json:"name" gorm:"column:name"`
	}
	// func (u TblUser) TableName() string {
	// 	return "user"
	// }
	// 学生
	type TblStudent struct {
		Id      int    `json:"id" gorm:"column:id"`
		ClassId string `json:"class_id" gorm:"column:class_id"` //班级
		UserId  int    `json:"user_id" gorm:"column:user_id"`   // 用户ID
	}
	// func (s TblStudent) TableName() string {
	// 	return "student"
	// }
	// 老师
	type TblTeacher struct {
		Id      int    `json:"id" gorm:"column:id"`
		ClassId string `json:"class_id" gorm:"column:class_id"` //班级
		UserId  int    `json:"user_id" gorm:"column:user_id"`   // 用户ID
		Subject string `json:"subject" gorm:"column:subject"`   // 科目
	}
	// func (t TblTeacher) TableName() string {
	// 	return "teacher"
	// }
	// 班级
	type TblClass struct {
		Id   int    `json:"id" gorm:"column:id"`
		Name string `json:"name" gorm:"column:name"`
	}
	//	func (c TblClass) TableName() string {
	//		return "class"
	//	}
	// 糖果
	type TblCandy struct {
		Id   int    `json:"id" gorm:"column:id"`
		Name string `json:"name" gorm:"column:name"`
	}
	//	func (c TblCandy) TableName() string {
	//		return "candy"
	//	}
	// 用户拥有糖果
	type TblUserHasCandy struct {
		Id      int `json:"id" gorm:"column:id"`
		CandyId int `json:"candy_id" gorm:"column:candy_id"`
		UserId  int `json:"user_id" gorm:"column:user_id"`
	}
	// func (c TblUserHasCandy) TableName() string {
	// 	return "user_has_candy"
	// }

	// 假设你有如下数据
	/*
		user
		id 	name
		1	花无缺
		2	林方
		3	楚云
		4	叶良辰
		5	西门吹水
		6	瑶池圣母
		7	羽柔仙子
		8	柳如烟
		9	牛斯克
		10	奥巴牛

		student
		id 	class_id	user_id
		1	1			1
		2	2			2
		3	3			3
		4	1			4
		5	2			5

		teacher
		id	class_id	user_id		subject
		1	1			6			化学
		2	1			7			物理
		3	1			8			语文
		4	2			6			化学
		5	2			9			物理
		6	3			10			语文

		class
		id	name
		1	终极一班
		2	究极二班
		3	超级三班

		user_has_candy
		id	user_id	candy_id
		1	1		1
		2	2		2
		3	3		3
		4	4		4
		5	5		5
		6	6		1
		7	7		2
		8	8		3
		9	9		4
		10	10		5
		11	1		2

		candy
		id	name
		1	奶糖
		2	牛蔗糖
		3	雪糕
		4	麻花糖
		5	口香糖

	*/

	db := &gorm.DB{}
	var users []TblUser
	db.Model(&users).Find(&users)

	// ex1 简单加载关系， 所有用户的身份和班级信息
	// 注意 输入的原始数据，不一定要数组，也可以是单个对象
	{
		// 这里可以是 var user TblUser，代表加载该对象的信息关系
		var users []TblUser
		db.Model(&users).Find(&users)

		//加载关系
		rl := NewRelationLoader(users, true)
		// 假设你的需求里， 每个人只有可能 【是或者不是】学生，使用是一对一关系用，has_one
		rl.AddRelation(HAS_ONE, "student", "id", "user_id", TblStudent{}, nil, nil)
		// 假设你的需求里，学生所在的班级是唯一的，使用是一对一关系用，has_one
		// class_id 是表student的字段，id是表class的字段 ，代表以student.class_id关联到class.id , 下面的以此类推
		rl.AddRelation(HAS_ONE, "student.class", "class_id", "id", TblClass{}, nil, nil)
		// 假设你的需求里，如果是老师，则有可能会任职多个班级多个科目，使用是一对多关系用，has_many
		rl.AddRelation(HAS_MANY, "teachers", "id", "user_id", TblTeacher{}, nil, nil)
		// 假设你的需求里， 班级是唯一的，使用是一对一关系用，has_one
		rl.AddRelation(HAS_ONE, "teachers.class", "class_id", "id", TblClass{}, nil, nil)
		result := rl.LoadResult(db).GetResult()
		fmt.Println(VtoJsonString(result))
		//打印结果
		/*
			[
			    {
			        "id": 1,
			        "name": "花无缺",
			        "student": {
			            "class": {
			                "id": 1,
			                "name": "终极一班"
			            },
			            "class_id": "1",
			            "id": 1,
			            "user_id": 1
			        },
			        "teachers": [

			        ]
			    },
			    {
			        "id": 2,
			        "name": "林方",
			        "student": {
			            "class": {
			                "id": 2,
			                "name": "究极二班"
			            },
			            "class_id": "2",
			            "id": 2,
			            "user_id": 2
			        },
			        "teachers": [

			        ]
			    },
			    {
			        "id": 3,
			        "name": "楚云",
			        "student": {
			            "class": {
			                "id": 3,
			                "name": "超级三班"
			            },
			            "class_id": "3",
			            "id": 3,
			            "user_id": 3
			        },
			        "teachers": [

			        ]
			    },
			    {
			        "id": 4,
			        "name": "叶良辰",
			        "student": {
			            "class": {
			                "id": 1,
			                "name": "终极一班"
			            },
			            "class_id": "1",
			            "id": 4,
			            "user_id": 4
			        },
			        "teachers": [

			        ]
			    },
			    {
			        "id": 5,
			        "name": "西门吹水",
			        "student": {
			            "class": {
			                "id": 2,
			                "name": "究极二班"
			            },
			            "class_id": "2",
			            "id": 5,
			            "user_id": 5
			        },
			        "teachers": [

			        ]
			    },
			    {
			        "id": 6,
			        "name": "瑶池圣母",
			        "student": null,
			        "teachers": [
			            {
			                "class": {
			                    "id": 1,
			                    "name": "终极一班"
			                },
			                "class_id": "1",
			                "id": 1,
			                "subject": "化学",
			                "user_id": 6
			            },
			            {
			                "class": {
			                    "id": 2,
			                    "name": "究极二班"
			                },
			                "class_id": "2",
			                "id": 4,
			                "subject": "化学",
			                "user_id": 6
			            }
			        ]
			    },
			    {
			        "id": 7,
			        "name": "羽柔仙子",
			        "student": null,
			        "teachers": [
			            {
			                "class": {
			                    "id": 1,
			                    "name": "终极一班"
			                },
			                "class_id": "1",
			                "id": 2,
			                "subject": "物理",
			                "user_id": 7
			            }
			        ]
			    },
			    {
			        "id": 8,
			        "name": "柳如烟",
			        "student": null,
			        "teachers": [
			            {
			                "class": {
			                    "id": 1,
			                    "name": "终极一班"
			                },
			                "class_id": "1",
			                "id": 3,
			                "subject": "语文",
			                "user_id": 8
			            }
			        ]
			    },
			    {
			        "id": 9,
			        "name": "牛斯克",
			        "student": null,
			        "teachers": [
			            {
			                "class": {
			                    "id": 2,
			                    "name": "究极二班"
			                },
			                "class_id": "2",
			                "id": 5,
			                "subject": "物理",
			                "user_id": 9
			            }
			        ]
			    },
			    {
			        "id": 10,
			        "name": "奥巴牛",
			        "student": null,
			        "teachers": [
			            {
			                "class": {
			                    "id": 3,
			                    "name": "超级三班"
			                },
			                "class_id": "3",
			                "id": 6,
			                "subject": "语文",
			                "user_id": 10
			            }
			        ]
			    }
			]
		*/

	}

	// ex2
	// 要求从班级开始
	// 1、加载班级的化学和物理老师，
	// 2、加载班级所有学生，
	// 3、统计班级里的学生+老师的数量，
	// 4、加载这些人身上的所有糖果数据
	{

		var classes []TblClass
		db.Model(&classes).Find(&classes)

		rl := NewRelationLoader(classes, true)
		// 加载所有班级的化学和物理老师，并且计数老师数量
		rl.AddRelation(HAS_MANY, "teachers", "id", "class_id", TblTeacher{}, nil,
			db.Model(&TblTeacher{}).Where("subject in (?)", []string{"化学", "物理"}), //物理化学
			func(p, s gjson.Result) (gjson.Result, gjson.Result) { //计数
				var cnt_user = p.Get("cnt_user").Int()
				pv := p.String()
				pv, _ = sjson.Set(pv, "cnt_user", cnt_user+1)
				return gjson.Parse(pv), s
			},
		)
		// 获取每一个老师的信息
		rl.AddRelation(HAS_ONE, "teachers.user", "user_id", "id", TblUser{}, nil, nil)
		// 获取每一个老师身上的糖果信息
		rl.AddRelation(HAS_MANY, "teachers.user.candies", "id", "user_id", TblUserHasCandy{}, nil, nil)
		// 获取每一个糖果的详细信息
		rl.AddRelation(HAS_ONE, "teachers.user.candies.candy", "candy_id", "id", TblCandy{}, nil, nil)

		// 加载所有班级的学生
		rl.AddRelation(HAS_MANY, "students", "id", "class_id", TblStudent{}, nil, nil,
			func(p, s gjson.Result) (gjson.Result, gjson.Result) { //计数
				var cnt_user = p.Get("cnt_user").Int()
				pv := p.String()
				pv, _ = sjson.Set(pv, "cnt_user", cnt_user+1)
				return gjson.Parse(pv), s
			},
		)
		// rl.AddRelation(HAS_ONE, "students.user", "user_id", "id", TblUser{}, nil, nil)
		// 也可以这样添加关系
		rl.AddRelationWithOptions(&RelationOptions{
			RelationType: HAS_ONE,
			Relation:     "students.user",
			Fakey:        "user_id",
			Sukey:        "id",
			Child:        TblUser{},
		})
		rl.AddRelation(HAS_MANY, "students.user.candies", "id", "user_id", TblUserHasCandy{}, nil, nil)
		rl.AddRelation(HAS_ONE, "students.user.candies.candy", "candy_id", "id", TblCandy{}, nil, nil)
		result := rl.LoadResult(db).GetResult()
		fmt.Println(VtoJsonString(result))

		// 打印结果
		/*
			[
				{
					"cnt_user": 4,
					"id": 1,
					"name": "终极一班",
					"students": [
						{
							"class_id": "1",
							"id": 1,
							"user": {
								"candies": [
									{
										"candy": {
											"id": 1,
											"name": "奶糖"
										},
										"candy_id": 1,
										"id": 1,
										"user_id": 1
									},
									{
										"candy": {
											"id": 2,
											"name": "牛蔗糖"
										},
										"candy_id": 2,
										"id": 11,
										"user_id": 1
									}
								],
								"id": 1,
								"name": "花无缺"
							},
							"user_id": 1
						},
						{
							"class_id": "1",
							"id": 4,
							"user": {
								"candies": [
									{
										"candy": {
											"id": 4,
											"name": "麻花糖"
										},
										"candy_id": 4,
										"id": 4,
										"user_id": 4
									}
								],
								"id": 4,
								"name": "叶良辰"
							},
							"user_id": 4
						}
					],
					"teachers": [
						{
							"class_id": "1",
							"id": 1,
							"subject": "化学",
							"user": {
								"candies": [
									{
										"candy": {
											"id": 1,
											"name": "奶糖"
										},
										"candy_id": 1,
										"id": 6,
										"user_id": 6
									}
								],
								"id": 6,
								"name": "瑶池圣母"
							},
							"user_id": 6
						},
						{
							"class_id": "1",
							"id": 2,
							"subject": "物理",
							"user": {
								"candies": [
									{
										"candy": {
											"id": 2,
											"name": "牛蔗糖"
										},
										"candy_id": 2,
										"id": 7,
										"user_id": 7
									}
								],
								"id": 7,
								"name": "羽柔仙子"
							},
							"user_id": 7
						}
					]
				},
				{
					"cnt_user": 4,
					"id": 2,
					"name": "究极二班",
					"students": [
						{
							"class_id": "2",
							"id": 2,
							"user": {
								"candies": [
									{
										"candy": {
											"id": 2,
											"name": "牛蔗糖"
										},
										"candy_id": 2,
										"id": 2,
										"user_id": 2
									}
								],
								"id": 2,
								"name": "林方"
							},
							"user_id": 2
						},
						{
							"class_id": "2",
							"id": 5,
							"user": {
								"candies": [
									{
										"candy": {
											"id": 5,
											"name": "口香糖"
										},
										"candy_id": 5,
										"id": 5,
										"user_id": 5
									}
								],
								"id": 5,
								"name": "西门吹水"
							},
							"user_id": 5
						}
					],
					"teachers": [
						{
							"class_id": "2",
							"id": 4,
							"subject": "化学",
							"user": {
								"candies": [
									{
										"candy": {
											"id": 1,
											"name": "奶糖"
										},
										"candy_id": 1,
										"id": 6,
										"user_id": 6
									}
								],
								"id": 6,
								"name": "瑶池圣母"
							},
							"user_id": 6
						},
						{
							"class_id": "2",
							"id": 5,
							"subject": "物理",
							"user": {
								"candies": [
									{
										"candy": {
											"id": 4,
											"name": "麻花糖"
										},
										"candy_id": 4,
										"id": 9,
										"user_id": 9
									}
								],
								"id": 9,
								"name": "牛斯克"
							},
							"user_id": 9
						}
					]
				},
				{
					"cnt_user": 1,
					"id": 3,
					"name": "超级三班",
					"students": [
						{
							"class_id": "3",
							"id": 3,
							"user": {
								"candies": [
									{
										"candy": {
											"id": 3,
											"name": "雪糕"
										},
										"candy_id": 3,
										"id": 3,
										"user_id": 3
									}
								],
								"id": 3,
								"name": "楚云"
							},
							"user_id": 3
						}
					],
					"teachers": [

					]
				}
			]

		*/
	}

}
