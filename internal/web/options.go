package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"eidosinventory/internal/inventory"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type optionField struct {
	Key          string
	Title        string
	MaxLen       int
	CodeRequired bool
	Options      []inventory.FieldOption
}

var optionFields = []optionField{
	{Key: "category", Title: "类别", MaxLen: 20, CodeRequired: true},
	{Key: "group", Title: "组别", MaxLen: 20, CodeRequired: true},
	{Key: "audience", Title: "适用", MaxLen: 10, CodeRequired: true},
	{Key: "shoe_type", Title: "鞋型", MaxLen: 20, CodeRequired: true},
	{Key: "shoe_height", Title: "鞋帮", MaxLen: 20, CodeRequired: true},
	{Key: "closure", Title: "穿脱结构", MaxLen: 20, CodeRequired: true},
	{Key: "color", Title: "颜色", MaxLen: 60},
	{Key: "size", Title: "尺码/规格", MaxLen: 30},
	{Key: "location", Title: "存放位置", MaxLen: 120},
}

var initialOptions = []inventory.FieldOption{
	{Field: "category", Name: "服装", Code: "C", Active: true}, {Field: "category", Name: "鞋", Code: "S", Active: true},
	{Field: "category", Name: "帽子", Code: "H", Active: true}, {Field: "category", Name: "围巾", Code: "W", Active: true},
	{Field: "category", Name: "包", Code: "B", Active: true}, {Field: "category", Name: "其他配饰", Code: "A", Active: true},
	{Field: "category", Name: "道具", Code: "P", Active: true},
	{Field: "group", Name: "KIDS 120", Code: "K", Active: true}, {Field: "group", Name: "BABY 90", Code: "B", Active: true}, {Field: "group", Name: "成人", Code: "A", Active: true},
	{Field: "audience", Name: "男", Code: "M", Active: true}, {Field: "audience", Name: "女", Code: "F", Active: true}, {Field: "audience", Name: "通用", Code: "U", Active: true},
	{Field: "shoe_type", Name: "运动/板鞋", Code: "1", Active: true}, {Field: "shoe_type", Name: "皮鞋", Code: "2", Active: true},
	{Field: "shoe_type", Name: "凉鞋", Code: "3", Active: true}, {Field: "shoe_type", Name: "靴子", Code: "4", Active: true},
	{Field: "shoe_height", Name: "低帮", Code: "1", Active: true}, {Field: "shoe_height", Name: "中帮", Code: "2", Active: true},
	{Field: "shoe_height", Name: "高帮", Code: "3", Active: true}, {Field: "shoe_height", Name: "露趾", Code: "4", Active: true},
	{Field: "closure", Name: "魔术贴", Code: "1", Active: true}, {Field: "closure", Name: "鞋带", Code: "2", Active: true},
	{Field: "closure", Name: "套脚", Code: "3", Active: true}, {Field: "closure", Name: "搭扣", Code: "4", Active: true},
}

func seedOptions(db *gorm.DB) error {
	for _, option := range initialOptions {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
			return err
		}
	}
	sizes := make([]string, 0, 51)
	sizes = append(sizes, "均码", "S", "M", "L", "XL")
	for i := range 29 {
		sizes = append(sizes, strconv.Itoa(18+i))
	}
	for i := range 7 {
		sizes = append(sizes, strconv.Itoa(48+i*2))
	}
	for i := range 10 {
		sizes = append(sizes, strconv.Itoa(80+i*10))
	}
	locations := make([]string, 0, 15)
	for _, shelf := range []string{"A", "B", "C", "D", "E"} {
		for i := range 3 {
			locations = append(locations, fmt.Sprintf("%s架-%02d", shelf, i+1))
		}
	}
	for _, field := range []struct {
		key   string
		names []string
	}{
		{"color", []string{"白色", "黑色", "灰色", "米色", "红色", "蓝色", "棕色", "绿色", "黄色", "粉色", "紫色", "橙色", "藏青色", "卡其色", "多色"}},
		{"size", sizes},
		{"location", locations},
	} {
		for i, name := range field.names {
			option := inventory.FieldOption{Field: field.key, Name: name, Code: strconv.Itoa(i + 1), Active: true}
			if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
				return err
			}
		}
		var names []string
		if err := db.Model(&inventory.Asset{}).Distinct(assetColumn(field.key)).Where(assetColumn(field.key)+" <> ?", "").Pluck(assetColumn(field.key), &names).Error; err != nil {
			return err
		}
		for _, name := range names {
			var count int64
			if err := db.Model(&inventory.FieldOption{}).Where("field = ? AND name = ?", field.key, name).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				continue
			}
			code, err := randomHex(16)
			if err != nil {
				return err
			}
			if err := db.Create(&inventory.FieldOption{Field: field.key, Name: name, Code: code, Active: true}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) options() (map[string][]inventory.FieldOption, inventory.OptionCodes, error) {
	var rows []inventory.FieldOption
	if err := s.db.Where("active = ?", true).Order("id").Find(&rows).Error; err != nil {
		return nil, inventory.OptionCodes{}, err
	}
	options := make(map[string][]inventory.FieldOption)
	codes := inventory.OptionCodes{Category: map[string]string{}, Group: map[string]string{}, Audience: map[string]string{}, ShoeType: map[string]string{}, ShoeHeight: map[string]string{}, Closure: map[string]string{}, Color: map[string]string{}, Size: map[string]string{}, Location: map[string]string{}}
	for _, row := range rows {
		options[row.Field] = append(options[row.Field], row)
		switch row.Field {
		case "category":
			codes.Category[row.Name] = row.Code
		case "group":
			codes.Group[row.Name] = row.Code
		case "audience":
			codes.Audience[row.Name] = row.Code
		case "shoe_type":
			codes.ShoeType[row.Name] = row.Code
		case "shoe_height":
			codes.ShoeHeight[row.Name] = row.Code
		case "closure":
			codes.Closure[row.Name] = row.Code
		case "color":
			codes.Color[row.Name] = row.Code
		case "size":
			codes.Size[row.Name] = row.Code
		case "location":
			codes.Location[row.Name] = row.Code
		}
	}
	return options, codes, nil
}

func optionNames(db *gorm.DB, field string) []string {
	var names []string
	if err := db.Model(&inventory.FieldOption{}).Where("field = ? AND active = ?", field, true).Order("id").Pluck("name", &names).Error; err != nil {
		return nil
	}
	return names
}

func optionNameByCode(db *gorm.DB, field, code string) string {
	var option inventory.FieldOption
	if err := db.Where("field = ? AND code = ?", field, code).First(&option).Error; err != nil {
		return ""
	}
	return option.Name
}

func optionCode(db *gorm.DB, field, name string) string {
	var option inventory.FieldOption
	if err := db.Where("field = ? AND name = ?", field, name).First(&option).Error; err != nil {
		return ""
	}
	return option.Code
}

func (s *Server) optionPage(w http.ResponseWriter, u *inventory.User, csrf, errorMessage, notice string) {
	var rows []inventory.FieldOption
	if err := s.db.Order("id").Find(&rows).Error; err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	fields := make([]optionField, len(optionFields))
	copy(fields, optionFields)
	for _, row := range rows {
		for i := range fields {
			if fields[i].Key == row.Field {
				fields[i].Options = append(fields[i].Options, row)
				break
			}
		}
	}
	s.render(w, "options.html", page{Title: "可选值管理", User: u, CSRF: csrf, OptionFields: fields, Error: errorMessage, Notice: notice})
}

func validOptionField(field string) bool {
	for _, v := range optionFields {
		if v.Key == field {
			return true
		}
	}
	return false
}

func assetColumn(field string) string {
	switch field {
	case "category":
		return "category"
	case "group":
		return "group"
	case "audience":
		return "audience"
	case "shoe_type":
		return "shoe_type"
	case "shoe_height":
		return "shoe_height"
	case "closure":
		return "closure"
	case "color":
		return "color"
	case "size":
		return "size"
	case "location":
		return "location"
	}
	return ""
}

func (s *Server) optionIndex(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	if r.Method == http.MethodGet {
		s.optionPage(w, u, csrf, "", r.URL.Query().Get("notice"))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", 405)
		return
	}
	field, name, code := r.FormValue("field"), strings.TrimSpace(r.FormValue("name")), strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	if !validOptionField(field) || !validOptionName(field, name) || ((field != "color" && field != "size" && field != "location") && !validOptionCode(field, code)) {
		s.optionPage(w, u, csrf, "字段、名称或编码无效", "")
		return
	}
	if field == "color" || field == "size" || field == "location" {
		var err error
		code, err = randomHex(16)
		if err != nil {
			http.Error(w, "服务器错误", 500)
			return
		}
	}
	option := inventory.FieldOption{Field: field, Name: name, Code: code, Active: true}
	if err := s.db.Create(&option).Error; err != nil {
		s.optionPage(w, u, csrf, "该字段的名称或编码已存在", "")
		return
	}
	http.Redirect(w, r, "/options?notice=可选值已新增", http.StatusSeeOther)
}

func validOptionName(field, name string) bool {
	for _, option := range optionFields {
		if option.Key == field {
			if field == "size" {
				if _, err := inventory.SizeCode(name); err != nil {
					return false
				}
			}
			return name != "" && len([]rune(name)) <= option.MaxLen
		}
	}
	return false
}
func validOptionCode(field, code string) bool {
	if len(code) != 1 {
		return false
	}
	if field == "category" {
		return code >= "A" && code <= "Z" && code != "S"
	}
	if field == "group" || field == "audience" {
		return code >= "A" && code <= "Z"
	}
	return code >= "1" && code <= "9"
}

func (s *Server) optionUpdate(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", 405)
		return
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/options/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	active := r.FormValue("active") == "1"
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var option inventory.FieldOption
		if err := tx.First(&option, id).Error; err != nil {
			return err
		}
		if !validOptionName(option.Field, name) {
			return errors.New("名称为空或超过字段长度限制")
		}
		if !active && (option.Field == "category" && option.Code == "S") {
			return errors.New("鞋类选项不能停用")
		}
		var used int64
		if err := tx.Model(&inventory.Asset{}).Where(assetColumn(option.Field)+" = ?", option.Name).Count(&used).Error; err != nil {
			return err
		}
		if !active && used > 0 {
			return errors.New("已有资产使用此选项，不能停用")
		}
		if name != option.Name {
			if option.Field == "size" && used > 0 {
				oldCode, oldErr := inventory.SizeCode(option.Name)
				newCode, newErr := inventory.SizeCode(name)
				if oldErr != nil || newErr != nil || oldCode != newCode {
					return errors.New("尺码/规格影响标签编号，请新增选项后在资产编辑页修改")
				}
			}
			if err := tx.Model(&inventory.Asset{}).Where(assetColumn(option.Field)+" = ?", option.Name).Update(assetColumn(option.Field), name).Error; err != nil {
				return err
			}
		}
		option.Name, option.Active = name, active
		return tx.Save(&option).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.optionPage(w, u, csrf, fmt.Sprintf("保存失败：%v", err), "")
		return
	}
	http.Redirect(w, r, "/options?notice=可选值已保存", http.StatusSeeOther)
}
