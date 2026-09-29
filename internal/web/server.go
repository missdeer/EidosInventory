package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"eidosinventory/internal/inventory"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Server struct {
	db        *gorm.DB
	dataDir   string
	templates *template.Template
}

type page struct {
	Title          string
	User           *inventory.User
	CSRF           string
	Error          string
	Notice         string
	Assets         []inventory.Asset
	AssetRows      []assetRow
	Asset          inventory.Asset
	Users          []inventory.User
	Counts         map[string]int64
	CategoryCounts []categoryCount
	Query          string
	Category       string
	Status         string
	Page           int
	HasNext        bool
	SameStock      int64
	Validation     string
	OldTags        []string
	View           string
	Options        map[string][]inventory.FieldOption
	OptionFields   []optionField
}

type categoryCount struct {
	Name  string
	Count int64
}

type assetRow struct {
	Asset      inventory.Asset
	SameStock  int64
	Validation string
}

func New(db *gorm.DB, dataDir string) (*Server, error) {
	if err := db.AutoMigrate(&inventory.User{}, &inventory.Session{}, &inventory.Asset{}, &inventory.TagReservation{}, &inventory.FieldOption{}); err != nil {
		return nil, err
	}
	if err := seed(db); err != nil {
		return nil, err
	}
	if err := seedOptions(db); err != nil {
		return nil, err
	}
	if err := reserveExistingTags(db); err != nil {
		return nil, err
	}
	t, err := template.New("").Funcs(template.FuncMap{
		"eq":              func(a, b any) bool { return a == b },
		"listCategories":  func() []string { return optionNames(db, "category") },
		"listGroups":      func() []string { return optionNames(db, "group") },
		"listAudiences":   func() []string { return optionNames(db, "audience") },
		"listShoeTypes":   func() []string { return optionNames(db, "shoe_type") },
		"listShoeHeights": func() []string { return optionNames(db, "shoe_height") },
		"listClosures":    func() []string { return optionNames(db, "closure") },
		"shoeName":        func() string { return optionNameByCode(db, "category", "S") },
		"isShoe":          func(name string) bool { return optionCode(db, "category", name) == "S" },
		"canWrite":        inventory.CanWrite,
		"canAdmin":        inventory.CanAdmin,
		"add":             func(a, b int) int { return a + b },
		"sub":             func(a, b int) int { return a - b },
	}).ParseFS(templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{db: db, dataDir: dataDir, templates: t}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("panic: %v", p)
			http.Error(w, "服务器错误", 500)
		}
	}()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	}
	if r.URL.Path == "/login" {
		s.login(w, r)
		return
	}
	user, session, ok := s.current(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/uploads/") {
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(r.URL.Path)}))
		}
		http.StripPrefix("/uploads/", http.FileServer(http.Dir(filepath.Join(s.dataDir, "uploads")))).ServeHTTP(w, r)
		return
	}
	if r.Method == http.MethodPost && (r.FormValue("csrf") == "" || r.FormValue("csrf") != session.CSRF) {
		http.Error(w, "表单已过期，请刷新页面", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/logout" && r.Method == http.MethodPost:
		s.db.Delete(&session)
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		s.assets(w, r, user, session.CSRF)
	case r.URL.Path == "/assets/new" && inventory.CanWrite(user.Role):
		s.assetNew(w, r, user, session.CSRF)
	case strings.HasPrefix(r.URL.Path, "/assets/") && strings.HasSuffix(r.URL.Path, "/edit") && inventory.CanWrite(user.Role):
		s.assetEdit(w, r, user, session.CSRF)
	case strings.HasPrefix(r.URL.Path, "/assets/") && r.Method == http.MethodGet:
		s.assetDetail(w, r, user, session.CSRF)
	case r.URL.Path == "/users" && inventory.CanAdmin(user.Role):
		s.users(w, r, user, session.CSRF)
	case r.URL.Path == "/options" && inventory.CanAdmin(user.Role):
		s.optionIndex(w, r, user, session.CSRF)
	case strings.HasPrefix(r.URL.Path, "/options/") && inventory.CanAdmin(user.Role):
		s.optionUpdate(w, r, user, session.CSRF)
	case strings.HasPrefix(r.URL.Path, "/users/") && inventory.CanAdmin(user.Role):
		s.userUpdate(w, r, user, session.CSRF)
	default:
		http.NotFound(w, r)
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func hashToken(token string) string {
	d := sha256.Sum256([]byte(token))
	return hex.EncodeToString(d[:])
}

func (s *Server) current(r *http.Request) (*inventory.User, inventory.Session, bool) {
	c, err := r.Cookie("session")
	if err != nil || len(c.Value) != 64 {
		return nil, inventory.Session{}, false
	}
	var sess inventory.Session
	if err = s.db.Where("token_hash = ? AND expires_at > ?", hashToken(c.Value), time.Now()).First(&sess).Error; err != nil {
		return nil, sess, false
	}
	var u inventory.User
	if err = s.db.First(&u, sess.UserID).Error; err != nil {
		return nil, sess, false
	}
	return &u, sess, true
}

func (s *Server) render(w http.ResponseWriter, name string, p page) {
	options, _, err := s.options()
	if err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	p.Options = options
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, p); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, _, ok := s.current(r); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		s.render(w, "login.html", page{Title: "登录"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "表单无效", 400)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	var u inventory.User
	err := s.db.Where("username = ?", username).First(&u).Error
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		time.Sleep(250 * time.Millisecond)
		s.render(w, "login.html", page{Title: "登录", Error: "账号或密码错误"})
		return
	}
	token, e1 := randomHex(32)
	csrf, e2 := randomHex(32)
	if e1 != nil || e2 != nil {
		http.Error(w, "服务器错误", 500)
		return
	}
	sess := inventory.Session{TokenHash: hashToken(token), UserID: u.ID, CSRF: csrf, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	if err := s.db.Create(&sess).Error; err != nil {
		http.Error(w, "服务器错误", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", MaxAge: 7 * 24 * 3600, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) assets(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	cat := r.URL.Query().Get("category")
	status := r.URL.Query().Get("status")
	view := r.URL.Query().Get("view")
	if view != "full" {
		view = "compact"
	}
	pageNo, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageNo < 1 {
		pageNo = 1
	}
	if pageNo > 10000 {
		pageNo = 10000
	}
	query := s.db.Model(&inventory.Asset{})
	if q != "" {
		pat := "%" + strings.NewReplacer("%", "\\%", "_", "\\_").Replace(q) + "%"
		query = query.Where("tag LIKE ? ESCAPE '\\' OR name LIKE ? ESCAPE '\\' OR color LIKE ? ESCAPE '\\' OR keywords LIKE ? ESCAPE '\\' OR location LIKE ? ESCAPE '\\'", pat, pat, pat, pat, pat)
	}
	if cat != "" {
		query = query.Where("category = ?", cat)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var assets []inventory.Asset
	if err := query.Order("id DESC").Offset((pageNo - 1) * 20).Limit(21).Find(&assets).Error; err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	hasNext := len(assets) > 20
	if hasNext {
		assets = assets[:20]
	}
	var assetRows []assetRow
	if view == "full" {
		for _, a := range assets {
			stock, validation, err := s.assetMetrics(&a)
			if err != nil {
				http.Error(w, "查询失败", 500)
				return
			}
			assetRows = append(assetRows, assetRow{Asset: a, SameStock: stock, Validation: validation})
		}
	}
	counts := map[string]int64{"total": 0, "在库": 0, "停用": 0}
	var total int64
	s.db.Model(&inventory.Asset{}).Count(&total)
	counts["total"] = total
	for _, st := range []string{"在库", "停用"} {
		var n int64
		s.db.Model(&inventory.Asset{}).Where("status = ?", st).Count(&n)
		counts[st] = n
	}
	options, _, err := s.options()
	if err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	var categoryCounts []categoryCount
	for _, option := range options["category"] {
		name := option.Name
		var n int64
		s.db.Model(&inventory.Asset{}).Where("category = ? AND status = ?", name, "在库").Count(&n)
		categoryCounts = append(categoryCounts, categoryCount{Name: name, Count: n})
	}
	s.render(w, "assets.html", page{Title: "库存台账", User: u, CSRF: csrf, Assets: assets, AssetRows: assetRows, Counts: counts, CategoryCounts: categoryCounts, Query: q, Category: cat, Status: status, View: view, Page: pageNo, HasNext: hasNext, Notice: r.URL.Query().Get("notice")})
}

func (s *Server) readAsset(r *http.Request) (inventory.Asset, error) {
	design, err := strconv.Atoi(r.FormValue("design_no"))
	if err != nil {
		return inventory.Asset{}, errors.New("设计序号须为数字")
	}
	itemNo, err := requestedItemNo(r.FormValue("item_no"), 0)
	if err != nil {
		return inventory.Asset{}, err
	}
	a := inventory.Asset{Category: r.FormValue("category"), Name: r.FormValue("name"), Group: r.FormValue("group"), Audience: r.FormValue("audience"), Color: r.FormValue("color"), Size: r.FormValue("size"), Keywords: r.FormValue("keywords"), Location: r.FormValue("location"), Status: r.FormValue("status"), ShoeType: r.FormValue("shoe_type"), ShoeHeight: r.FormValue("shoe_height"), Closure: r.FormValue("closure"), DesignNo: design, ItemNo: itemNo}
	if err := readAssetMetrics(r, &a); err != nil {
		return a, err
	}
	_, codes, err := s.options()
	if err != nil {
		return a, err
	}
	err = inventory.ValidateAssetWithOptions(&a, codes)
	return a, err
}

func readAssetMetrics(r *http.Request, a *inventory.Asset) error {
	if r.PostForm.Has("same_stock") {
		raw := strings.TrimSpace(r.FormValue("same_stock"))
		a.SameStockOverride = nil
		if raw != "" {
			stock, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || stock < 0 {
				return errors.New("同款同色同尺码在库数量须为非负整数")
			}
			a.SameStockOverride = &stock
		}
	}
	if r.PostForm.Has("validation") {
		a.ValidationOverride = strings.TrimSpace(r.FormValue("validation"))
	}
	return nil
}

func requestedItemNo(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 99 {
		return 0, errors.New("单件序号须为 1–99")
	}
	return n, nil
}

func (s *Server) assetNew(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	if r.Method == http.MethodGet {
		s.renderAssetForm(w, u, csrf, inventory.Asset{Status: "在库"}, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", 405)
		return
	}
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		http.Error(w, "表单过大", 400)
		return
	}
	a, err := s.readAsset(r)
	if err == nil {
		a.Photo, err = s.savePhoto(r)
	}
	if err == nil {
		err = s.createAsset(&a)
	}
	if err != nil {
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/assets/%d?notice=资产已新增", a.ID), http.StatusSeeOther)
}

func (s *Server) createAsset(a *inventory.Asset) error {
	_, codes, err := s.options()
	if err != nil {
		return err
	}
	prefix, style, size, err := inventory.CodePartsWithOptions(a, codes)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if codes.Category[a.Category] == "S" && a.Status == "在库" {
			if err := shoeLimit(tx, a, 0); err != nil {
				return err
			}
		}
		itemNo := a.ItemNo
		if itemNo == 0 {
			itemNo, err = nextItemNo(tx, prefix, style, size)
			if err != nil {
				return err
			}
		}
		a.ItemNo = itemNo
		a.Tag = fmt.Sprintf("%s%s-%s-%02d", prefix, style, size, itemNo)
		if err := ensureTagAvailable(tx, a.Tag); err != nil {
			return err
		}
		if err := tx.Create(a).Error; err != nil {
			return err
		}
		return tx.Create(&inventory.TagReservation{Tag: a.Tag, Prefix: prefix, StyleCode: style, SizeCode: size, ItemNo: itemNo, AssetID: a.ID}).Error
	})
}

func ensureTagAvailable(tx *gorm.DB, tag string) error {
	var n int64
	if err := tx.Model(&inventory.TagReservation{}).Where("tag = ?", tag).Count(&n).Error; err != nil {
		return err
	}
	if n != 0 {
		return errors.New("该单件序号对应的标签编号已使用，请选择其他序号")
	}
	return nil
}

func nextItemNo(tx *gorm.DB, prefix, style, size string) (int, error) {
	var max int
	if err := tx.Model(&inventory.TagReservation{}).Select("COALESCE(MAX(item_no),0)").Where("prefix = ? AND style_code = ? AND size_code = ?", prefix, style, size).Scan(&max).Error; err != nil {
		return 0, err
	}
	if max >= 99 {
		return 0, errors.New("该款式和规格的单件序号已用完")
	}
	return max + 1, nil
}

func reserveExistingTags(db *gorm.DB) error {
	var assets []inventory.Asset
	if err := db.Find(&assets).Error; err != nil {
		return err
	}
	for _, a := range assets {
		if len(a.Tag) < 3 {
			return fmt.Errorf("资产 %d 的标签编号无效", a.ID)
		}
		reservation := inventory.TagReservation{Tag: a.Tag, Prefix: a.Tag[:3], StyleCode: a.StyleCode, SizeCode: a.SizeCode, ItemNo: a.ItemNo, AssetID: a.ID}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&reservation).Error; err != nil {
			return err
		}
	}
	return nil
}

func shoeLimit(tx *gorm.DB, a *inventory.Asset, exclude uint) error {
	var n int64
	q := tx.Model(&inventory.Asset{}).Where("category = ? AND `group` = ? AND audience = ? AND style_code = ? AND color = ? AND size_code = ? AND status = ?", a.Category, a.Group, a.Audience, a.StyleCode, a.Color, a.SizeCode, "在库")
	if exclude != 0 {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n >= 4 {
		return errors.New("同款、同色、同尺码鞋在库最多 4 双")
	}
	return nil
}

func (s *Server) assetDetail(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	id, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/assets/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var a inventory.Asset
	if err := s.db.First(&a, id).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	sameStock, validation, err := s.assetMetrics(&a)
	if err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	var oldTags []string
	if err := s.db.Model(&inventory.TagReservation{}).Where("asset_id = ? AND tag <> ?", a.ID, a.Tag).Order("created_at DESC").Pluck("tag", &oldTags).Error; err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	s.render(w, "asset_view.html", page{Title: "资产详情", User: u, CSRF: csrf, Asset: a, SameStock: sameStock, Validation: validation, OldTags: oldTags, Notice: r.URL.Query().Get("notice")})
}

func (s *Server) assetMetrics(a *inventory.Asset) (int64, string, error) {
	sameStock, validation, err := s.calculatedAssetMetrics(a)
	if err != nil {
		return 0, "", err
	}
	if a.SameStockOverride != nil {
		sameStock = *a.SameStockOverride
	}
	if a.ValidationOverride != "" {
		validation = a.ValidationOverride
	}
	return sameStock, validation, nil
}

func (s *Server) calculatedAssetMetrics(a *inventory.Asset) (int64, string, error) {
	var sameStock int64
	if err := s.db.Model(&inventory.Asset{}).Where("category = ? AND `group` = ? AND audience = ? AND style_code = ? AND color = ? AND size_code = ? AND status = ?", a.Category, a.Group, a.Audience, a.StyleCode, a.Color, a.SizeCode, "在库").Count(&sameStock).Error; err != nil {
		return 0, "", err
	}
	_, codes, err := s.options()
	if err != nil {
		return 0, "", err
	}
	check := *a
	check.SameStockOverride = nil
	check.ValidationOverride = ""
	if err := inventory.ValidateAssetWithOptions(&check, codes); err != nil {
		return sameStock, err.Error(), nil
	}
	if a.ItemNo < 1 || a.ItemNo > 99 {
		return sameStock, "单件序号超出 1–99", nil
	}
	prefix, style, size, err := inventory.CodePartsWithOptions(&check, codes)
	if err != nil || a.StyleCode != style || a.SizeCode != size || a.Tag != fmt.Sprintf("%s%s-%s-%02d", prefix, style, size, a.ItemNo) {
		return sameStock, "标签编号与组成字段不一致", nil
	}
	if codes.Category[a.Category] == "S" && sameStock > 4 {
		return sameStock, "同款同色同尺码鞋超过 4 双", nil
	}
	return sameStock, "正常", nil
}

func (s *Server) renderAssetForm(w http.ResponseWriter, u *inventory.User, csrf string, a inventory.Asset, errorMessage string) {
	title := "新增资产"
	validation := "保存后更新"
	var sameStock int64
	if a.ID != 0 {
		title = "编辑资产"
		if errorMessage == "" {
			var err error
			sameStock, validation, err = s.calculatedAssetMetrics(&a)
			if err != nil {
				http.Error(w, "查询失败", 500)
				return
			}
		}
	}
	s.render(w, "asset_form.html", page{Title: title, User: u, CSRF: csrf, Asset: a, SameStock: sameStock, Validation: validation, Error: errorMessage})
}

func (s *Server) assetEdit(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	id, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/assets/"), "/edit"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var a inventory.Asset
	if err := s.db.First(&a, id).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		s.renderAssetForm(w, u, csrf, a, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", 405)
		return
	}
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		http.Error(w, "表单过大", 400)
		return
	}
	original := a
	designNo, err := strconv.Atoi(r.FormValue("design_no"))
	if err != nil {
		s.renderAssetForm(w, u, csrf, a, "设计序号须为数字")
		return
	}
	itemNo, err := requestedItemNo(r.FormValue("item_no"), original.ItemNo)
	if err != nil {
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	a.DesignNo = designNo
	a.ItemNo = itemNo
	a.Size = r.FormValue("size")
	a.Category = r.FormValue("category")
	a.Group = r.FormValue("group")
	a.Audience = r.FormValue("audience")
	a.ShoeType = r.FormValue("shoe_type")
	a.ShoeHeight = r.FormValue("shoe_height")
	a.Closure = r.FormValue("closure")
	a.Name = strings.TrimSpace(r.FormValue("name"))
	a.Color = strings.TrimSpace(r.FormValue("color"))
	a.Keywords = strings.TrimSpace(r.FormValue("keywords"))
	a.Location = strings.TrimSpace(r.FormValue("location"))
	a.Status = r.FormValue("status")
	if err := readAssetMetrics(r, &a); err != nil {
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	_, codes, err := s.options()
	if err != nil {
		http.Error(w, "查询失败", 500)
		return
	}
	if err := inventory.ValidateAssetWithOptions(&a, codes); err != nil {
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	photo, err := s.savePhoto(r)
	if err != nil {
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	if photo != "" {
		a.Photo = photo
	} else if r.FormValue("remove_photo") == "1" {
		a.Photo = ""
	}
	prefix, style, size, err := inventory.CodePartsWithOptions(&a, codes)
	if err != nil {
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	oldPrefix, _, _, err := inventory.CodePartsWithOptions(&original, codes)
	if err != nil {
		http.Error(w, "原编号无效", 500)
		return
	}
	scopeChanged := prefix != oldPrefix || style != original.StyleCode || size != original.SizeCode
	itemChanged := a.ItemNo != original.ItemNo
	if scopeChanged && !itemChanged {
		a.ItemNo = 0
	}
	codeChanged := scopeChanged || itemChanged
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if codes.Category[a.Category] == "S" && a.Status == "在库" {
			if err := shoeLimit(tx, &a, a.ID); err != nil {
				return err
			}
		}
		if codeChanged {
			if a.ItemNo == 0 {
				a.ItemNo, err = nextItemNo(tx, prefix, style, size)
				if err != nil {
					return err
				}
			}
			a.Tag = fmt.Sprintf("%s%s-%s-%02d", prefix, style, size, a.ItemNo)
			if err := ensureTagAvailable(tx, a.Tag); err != nil {
				return err
			}
			if err := tx.Create(&inventory.TagReservation{Tag: a.Tag, Prefix: prefix, StyleCode: style, SizeCode: size, ItemNo: a.ItemNo, AssetID: a.ID}).Error; err != nil {
				return err
			}
		}
		return tx.Save(&a).Error
	})
	if err != nil {
		a.Tag = original.Tag
		a.ItemNo = original.ItemNo
		s.renderAssetForm(w, u, csrf, a, err.Error())
		return
	}
	notice := "资产已保存"
	if codeChanged {
		notice = "资产已保存；编号已更新，请重新贴标"
	}
	http.Redirect(w, r, fmt.Sprintf("/assets/%d?notice=%s", a.ID, notice), http.StatusSeeOther)
}

func (s *Server) savePhoto(r *http.Request) (string, error) {
	f, header, err := r.FormFile("photo")
	if errors.Is(err, http.ErrMissingFile) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	if header.Size > 5<<20 {
		return "", errors.New("照片不能超过 5 MB")
	}
	buf, err := io.ReadAll(io.LimitReader(f, (5<<20)+1))
	if err != nil {
		return "", err
	}
	if len(buf) > 5<<20 {
		return "", errors.New("照片不能超过 5 MB")
	}
	mime := http.DetectContentType(buf)
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[mime]
	if ext == "" {
		return "", errors.New("照片仅支持 JPEG、PNG 或 WebP")
	}
	name, err := randomHex(16)
	if err != nil {
		return "", err
	}
	name += ext
	if err := os.WriteFile(filepath.Join(s.dataDir, "uploads", name), buf, 0600); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Server) users(w http.ResponseWriter, r *http.Request, u *inventory.User, csrf string) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "表单无效", 400)
			return
		}
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		role := r.FormValue("role")
		if len(username) < 3 || len(username) > 80 || strings.ContainsAny(username, " \t\r\n") || len(password) < 8 || len(password) > 128 || (role != inventory.RoleViewer && role != inventory.RoleEditor && role != inventory.RoleAdmin) {
			s.userPage(w, u, csrf, "账号至少 3 位且无空格；密码至少 8 位；请选择有效权限", "")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err == nil {
			err = s.db.Create(&inventory.User{Username: username, PasswordHash: string(hash), Role: role}).Error
		}
		if err != nil {
			s.userPage(w, u, csrf, "账号已存在或保存失败", "")
			return
		}
		http.Redirect(w, r, "/users?notice=账号已创建", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "方法不允许", 405)
		return
	}
	s.userPage(w, u, csrf, "", r.URL.Query().Get("notice"))
}

func (s *Server) userPage(w http.ResponseWriter, u *inventory.User, csrf, errMsg, notice string) {
	var users []inventory.User
	s.db.Order("id").Find(&users)
	s.render(w, "users.html", page{Title: "账号管理", User: u, CSRF: csrf, Users: users, Error: errMsg, Notice: notice})
}

func (s *Server) userUpdate(w http.ResponseWriter, r *http.Request, actor *inventory.User, csrf string) {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", 405)
		return
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/users/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var target inventory.User
	if err := s.db.First(&target, id).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	role := r.FormValue("role")
	password := r.FormValue("password")
	if role != inventory.RoleViewer && role != inventory.RoleEditor && role != inventory.RoleAdmin {
		s.userPage(w, actor, csrf, "权限无效", "")
		return
	}
	if target.ID == actor.ID && role != inventory.RoleAdmin {
		s.userPage(w, actor, csrf, "不能取消自己的管理员权限", "")
		return
	}
	oldRole := target.Role
	if password != "" {
		if len(password) < 8 || len(password) > 128 {
			s.userPage(w, actor, csrf, "新密码须为 8–128 位", "")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "服务器错误", 500)
			return
		}
		target.PasswordHash = string(hash)
	}
	target.Role = role
	if err := s.db.Save(&target).Error; err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	if password != "" || role != oldRole {
		s.db.Where("user_id = ?", target.ID).Delete(&inventory.Session{})
	}
	http.Redirect(w, r, "/users?notice=账号已更新", http.StatusSeeOther)
}
