package inventory

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
)

type User struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"uniqueIndex;size:80;not null"`
	PasswordHash string `gorm:"not null"`
	Role         string `gorm:"size:16;not null"`
	CreatedAt    time.Time
}

type Session struct {
	TokenHash string    `gorm:"primaryKey;size:64"`
	UserID    uint      `gorm:"index;not null"`
	CSRF      string    `gorm:"not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

type Asset struct {
	ID                 uint   `gorm:"primaryKey"`
	Tag                string `gorm:"uniqueIndex;size:40;not null"`
	Category           string `gorm:"index;size:20;not null"`
	Name               string `gorm:"size:160;not null"`
	Group              string `gorm:"size:20;not null"`
	Audience           string `gorm:"size:10;not null"`
	Color              string `gorm:"size:60;not null"`
	Size               string `gorm:"size:30;not null"`
	Keywords           string `gorm:"size:500"`
	Location           string `gorm:"size:120"`
	Status             string `gorm:"index;size:10;not null"`
	ShoeType           string `gorm:"size:20"`
	ShoeHeight         string `gorm:"size:20"`
	Closure            string `gorm:"size:20"`
	DesignNo           int    `gorm:"not null"`
	StyleCode          string `gorm:"size:5;not null"`
	SizeCode           string `gorm:"size:3;not null"`
	ItemNo             int    `gorm:"not null"`
	Photo              string `gorm:"size:100"`
	SameStockOverride  *int64
	ValidationOverride string `gorm:"size:20"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TagReservation keeps every issued label, including labels replaced after an edit.
// Its scope fields support allocating the next item number without recycling old labels.
type TagReservation struct {
	Tag       string `gorm:"primaryKey;size:40"`
	Prefix    string `gorm:"index:idx_tag_scope,priority:1;size:3;not null"`
	StyleCode string `gorm:"index:idx_tag_scope,priority:2;size:5;not null"`
	SizeCode  string `gorm:"index:idx_tag_scope,priority:3;size:3;not null"`
	ItemNo    int    `gorm:"not null"`
	AssetID   uint   `gorm:"index;not null"`
	CreatedAt time.Time
}

type FieldOption struct {
	ID        uint   `gorm:"primaryKey"`
	Field     string `gorm:"uniqueIndex:idx_option_name;uniqueIndex:idx_option_code;size:20;not null"`
	Name      string `gorm:"uniqueIndex:idx_option_name;size:120;not null"`
	Code      string `gorm:"uniqueIndex:idx_option_code;size:32;not null"`
	Active    bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
}

type OptionCodes struct {
	Category   map[string]string
	Group      map[string]string
	Audience   map[string]string
	ShoeType   map[string]string
	ShoeHeight map[string]string
	Closure    map[string]string
	Color      map[string]string
	Size       map[string]string
	Location   map[string]string
}

func DefaultOptionCodes() OptionCodes {
	return OptionCodes{Category: categoryCodes, Group: groupCodes, Audience: audienceCodes, ShoeType: shoeTypeCodes, ShoeHeight: shoeHeightCodes, Closure: closureCodes}
}

var categoryCodes = map[string]string{"服装": "C", "鞋": "S", "帽子": "H", "围巾": "W", "包": "B", "其他配饰": "A", "道具": "P"}
var groupCodes = map[string]string{"KIDS 120": "K", "BABY 90": "B", "成人": "A"}
var audienceCodes = map[string]string{"男": "M", "女": "F", "通用": "U"}
var shoeTypeCodes = map[string]string{"运动/板鞋": "1", "皮鞋": "2", "凉鞋": "3", "靴子": "4"}
var shoeHeightCodes = map[string]string{"低帮": "1", "中帮": "2", "高帮": "3", "露趾": "4"}
var closureCodes = map[string]string{"魔术贴": "1", "鞋带": "2", "套脚": "3", "搭扣": "4"}
var letterSizes = map[string]string{"均码": "00", "S": "01", "M": "02", "L": "03", "XL": "04"}

func CodeParts(a *Asset) (string, string, string, error) {
	return CodePartsWithOptions(a, DefaultOptionCodes())
}

func CodePartsWithOptions(a *Asset, codes OptionCodes) (string, string, string, error) {
	category := codes.Category[a.Category]
	group := codes.Group[a.Group]
	audience := codes.Audience[a.Audience]
	if category == "" || group == "" || audience == "" {
		return "", "", "", errors.New("类别、组别或适用选项无效")
	}
	if a.DesignNo < 1 || a.DesignNo > 99 {
		return "", "", "", errors.New("设计序号须为 1–99")
	}
	style := fmt.Sprintf("%03d", a.DesignNo)
	if category == "S" {
		t, h, c := codes.ShoeType[a.ShoeType], codes.ShoeHeight[a.ShoeHeight], codes.Closure[a.Closure]
		if t == "" || h == "" || c == "" {
			return "", "", "", errors.New("鞋类须选择鞋型、鞋帮和穿脱结构")
		}
		style = fmt.Sprintf("%s%s%s%02d", t, h, c, a.DesignNo)
	} else if a.ShoeType != "" || a.ShoeHeight != "" || a.Closure != "" {
		return "", "", "", errors.New("非鞋类不填写鞋型、鞋帮或穿脱结构")
	}
	if codes.Size != nil && codes.Size[a.Size] == "" {
		return "", "", "", errors.New("尺码/规格选项无效")
	}
	sizeCode, err := SizeCode(a.Size)
	if err != nil {
		return "", "", "", err
	}
	return category + group + audience, style, sizeCode, nil
}

func SizeCode(size string) (string, error) {
	size = strings.TrimSpace(size)
	if size == "" {
		return "", errors.New("尺码/规格不能为空")
	}
	sizeCode := letterSizes[strings.ToUpper(size)]
	if size == "均码" {
		sizeCode = "00"
	}
	if sizeCode == "" {
		n, err := strconv.Atoi(size)
		if err != nil || n < 0 || n > 999 {
			return "", errors.New("尺码须为 0–999、均码或 S/M/L/XL")
		}
		sizeCode = fmt.Sprintf("%02d", n)
	}
	return sizeCode, nil
}

func ValidateAsset(a *Asset) error {
	return ValidateAssetWithOptions(a, DefaultOptionCodes())
}

func ValidateAssetWithOptions(a *Asset, codes OptionCodes) error {
	a.Name = strings.TrimSpace(a.Name)
	a.Color = strings.TrimSpace(a.Color)
	a.Size = strings.TrimSpace(a.Size)
	a.Keywords = strings.TrimSpace(a.Keywords)
	a.Location = strings.TrimSpace(a.Location)
	if a.Name == "" || a.Color == "" {
		return errors.New("名称和颜色不能为空")
	}
	if codes.Color != nil && codes.Color[a.Color] == "" {
		return errors.New("颜色选项无效")
	}
	if a.Location != "" && codes.Location != nil && codes.Location[a.Location] == "" {
		return errors.New("存放位置选项无效")
	}
	if len([]rune(a.Name)) > 160 || len([]rune(a.Color)) > 60 || len([]rune(a.Keywords)) > 500 || len([]rune(a.Location)) > 120 {
		return errors.New("输入内容过长")
	}
	if a.Status != "在库" && a.Status != "停用" {
		return errors.New("状态无效")
	}
	if a.SameStockOverride != nil && *a.SameStockOverride < 0 {
		return errors.New("同款同色同尺码在库数量须为非负整数")
	}
	if a.ValidationOverride != "" && a.ValidationOverride != "正常" && a.ValidationOverride != "异常" && a.ValidationOverride != "待校验" {
		return errors.New("校验状态无效")
	}
	prefix, style, size, err := CodePartsWithOptions(a, codes)
	if err != nil {
		return err
	}
	a.StyleCode, a.SizeCode = style, size
	_ = prefix
	return nil
}

func CanWrite(role string) bool { return role == RoleEditor || role == RoleAdmin }
func CanAdmin(role string) bool { return role == RoleAdmin }
