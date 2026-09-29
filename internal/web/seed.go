package web

import (
	"eidosinventory/internal/inventory"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func seed(db *gorm.DB) error {
	var n int64
	if err := db.Model(&inventory.User{}).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		for _, v := range []struct{ username, password, role string }{
			{"admin", "admin1234", inventory.RoleAdmin},
			{"editor", "editor1234", inventory.RoleEditor},
			{"viewer", "viewer1234", inventory.RoleViewer},
		} {
			hash, err := bcrypt.GenerateFromPassword([]byte(v.password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			if err := db.Create(&inventory.User{Username: v.username, PasswordHash: string(hash), Role: v.role}).Error; err != nil {
				return err
			}
		}
	}
	if err := db.Model(&inventory.Asset{}).Count(&n).Error; err != nil {
		return err
	}
	if n != 0 {
		return nil
	}
	// The nine populated rows in the workbook's second sheet. The first row's
	// "靴子" classification is kept as supplied, including its SKM41101 tag.
	rows := []inventory.Asset{
		{Tag: "SKM41101-28-01", Category: "鞋", Name: "魔术贴板鞋", Group: "KIDS 120", Audience: "男", Color: "白色", Size: "28", Keywords: "拍摄;运动;百搭", Location: "A架-02", Status: "在库", ShoeType: "靴子", ShoeHeight: "低帮", Closure: "魔术贴", DesignNo: 1, StyleCode: "41101", SizeCode: "28", ItemNo: 1},
		{Tag: "SKM11101-28-02", Category: "鞋", Name: "魔术贴板鞋", Group: "KIDS 120", Audience: "男", Color: "白色", Size: "28", Keywords: "拍摄;运动;百搭", Location: "A架-02", Status: "在库", ShoeType: "运动/板鞋", ShoeHeight: "低帮", Closure: "魔术贴", DesignNo: 1, StyleCode: "11101", SizeCode: "28", ItemNo: 2},
		{Tag: "SKM11101-28-03", Category: "鞋", Name: "魔术贴板鞋", Group: "KIDS 120", Audience: "男", Color: "黑色", Size: "28", Keywords: "拍摄;运动", Location: "A架-02", Status: "在库", ShoeType: "运动/板鞋", ShoeHeight: "低帮", Closure: "魔术贴", DesignNo: 1, StyleCode: "11101", SizeCode: "28", ItemNo: 3},
		{Tag: "SBF34401-18-01", Category: "鞋", Name: "搭扣凉鞋", Group: "BABY 90", Audience: "女", Color: "米色", Size: "18", Keywords: "夏季;拍摄", Location: "A架-03", Status: "在库", ShoeType: "凉鞋", ShoeHeight: "露趾", Closure: "搭扣", DesignNo: 1, StyleCode: "34401", SizeCode: "18", ItemNo: 1},
		{Tag: "HKU001-52-01", Category: "帽子", Name: "针织帽", Group: "KIDS 120", Audience: "通用", Color: "红色", Size: "52", Keywords: "冬季;针织;拍摄", Location: "B架-01", Status: "在库", DesignNo: 1, StyleCode: "001", SizeCode: "52", ItemNo: 1},
		{Tag: "WBU001-00-01", Category: "围巾", Name: "针织围巾", Group: "BABY 90", Audience: "通用", Color: "灰色", Size: "均码", Keywords: "冬季;柔软;拍摄", Location: "B架-02", Status: "在库", DesignNo: 1, StyleCode: "001", SizeCode: "00", ItemNo: 1},
		{Tag: "BAF001-00-01", Category: "包", Name: "斜挎包", Group: "成人", Audience: "女", Color: "黑色", Size: "均码", Keywords: "日常;斜挎;拍摄", Location: "C架-01", Status: "在库", DesignNo: 1, StyleCode: "001", SizeCode: "00", ItemNo: 1},
		{Tag: "CKF001-120-01", Category: "服装", Name: "儿童外套", Group: "KIDS 120", Audience: "女", Color: "蓝色", Size: "120", Keywords: "外套;秋冬;拍摄", Location: "D架-01", Status: "在库", DesignNo: 1, StyleCode: "001", SizeCode: "120", ItemNo: 1},
		{Tag: "PAU001-00-01", Category: "道具", Name: "木质手提箱", Group: "成人", Audience: "通用", Color: "棕色", Size: "均码", Keywords: "复古;箱子;拍摄", Location: "E架-01", Status: "在库", DesignNo: 1, StyleCode: "001", SizeCode: "00", ItemNo: 1},
	}
	return db.Create(&rows).Error
}
