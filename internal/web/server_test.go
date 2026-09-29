package web

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eidosinventory/internal/inventory"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	s, err := New(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func loginTest(t *testing.T, s *Server, username, password string) (*http.Cookie, string) {
	t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login %s: %d %s", username, w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("session cookie missing")
	}
	var sess inventory.Session
	if err := s.db.Where("token_hash = ?", hashToken(cookie.Value)).First(&sess).Error; err != nil {
		t.Fatal(err)
	}
	return cookie, sess.CSRF
}

func TestAssetPhotoDownload(t *testing.T) {
	s := testServer(t)
	viewer, _ := loginTest(t, s, "viewer", "viewer1234")
	get := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := get("/assets/1", viewer)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "download=1") {
		t.Fatal("asset without a photo must not offer a download")
	}
	const filename = "test-photo.png"
	photo := []byte("\x89PNG\r\n\x1a\nphoto content")
	if err := os.MkdirAll(filepath.Join(s.dataDir, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "uploads", filename), photo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&inventory.Asset{}).Where("id = ?", 1).Update("photo", filename).Error; err != nil {
		t.Fatal(err)
	}
	w = get("/assets/1", viewer)
	const downloadPath = "/uploads/" + filename + "?download=1"
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `href="`+downloadPath+`" download>下载</a>`) {
		t.Fatal("viewer must see the photo download action in asset details")
	}
	w = get(downloadPath, viewer)
	disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil || disposition != "attachment" || params["filename"] != filename {
		t.Fatalf("photo must be forced to download with its filename: %q (%v)", w.Header().Get("Content-Disposition"), err)
	}
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), photo) || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("download must return the original image bytes and media type")
	}
	w = get("/uploads/"+filename, viewer)
	if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "" || !bytes.Equal(w.Body.Bytes(), photo) {
		t.Fatal("normal photo requests must still support inline display")
	}
	w = get(downloadPath, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" || w.Header().Get("Content-Disposition") != "" {
		t.Fatal("photo download must require login")
	}
}

func TestRolesAndCreate(t *testing.T) {
	s := testServer(t)
	viewer, _ := loginTest(t, s, "viewer", "viewer1234")
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(viewer)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "href=\"/assets/1/edit\"") {
		t.Fatalf("viewer list edit entry: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/assets/new", nil)
	r.AddCookie(viewer)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("viewer accessed edit: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/users", nil)
	r.AddCookie(viewer)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("viewer accessed users: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/assets/1", nil)
	r.AddCookie(viewer)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "SKM41101-28-01") || !strings.Contains(w.Body.String(), "款式编号") || !strings.Contains(w.Body.String(), "鞋型") || !strings.Contains(w.Body.String(), "校验") || !strings.Contains(w.Body.String(), "创建时间") {
		t.Fatalf("viewer cannot read details: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "编辑资产") {
		t.Fatal("viewer saw edit action")
	}
	r = httptest.NewRequest("GET", "/assets/1/edit", nil)
	r.AddCookie(viewer)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("viewer accessed edit: %d", w.Code)
	}
	editor, csrf := loginTest(t, s, "editor", "editor1234")
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "href=\"/assets/1/edit\"") {
		t.Fatalf("editor list edit entry: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/assets/1", nil)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "href=\"/assets/1/edit\"") {
		t.Fatalf("editor detail action: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/assets/1/edit", nil)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "action=\"/assets/1/edit\"") {
		t.Fatalf("editor edit page: %d", w.Code)
	}
	for _, name := range []string{"category", "group", "audience", "shoe_type", "shoe_height", "closure"} {
		if !strings.Contains(w.Body.String(), "name=\""+name+"\"") {
			t.Fatalf("edit form missing %s dropdown", name)
		}
	}
	update := url.Values{"csrf": {csrf}, "name": {"更新后的板鞋"}, "category": {"鞋"}, "group": {"KIDS 120"}, "audience": {"男"}, "shoe_type": {"靴子"}, "shoe_height": {"低帮"}, "closure": {"魔术贴"}, "color": {"白色"}, "size": {"28"}, "design_no": {"1"}, "item_no": {"1"}, "keywords": {"拍摄;运动"}, "location": {"A架-02"}, "status": {"在库"}}
	r = multipartRequest(t, "/assets/1/edit", update)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/assets/1?") {
		t.Fatalf("editor update redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	var updated inventory.Asset
	if err := s.db.First(&updated, 1).Error; err != nil || updated.Name != "更新后的板鞋" {
		t.Fatalf("editor update failed: %v, %s", err, updated.Name)
	}
	form := url.Values{"csrf": {csrf}, "category": {"帽子"}, "name": {"测试帽"}, "group": {"KIDS 120"}, "audience": {"通用"}, "color": {"蓝色"}, "size": {"52"}, "status": {"在库"}, "design_no": {"1"}}
	r = multipartRequest(t, "/assets/new", form)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("asset create: %d %s", w.Code, w.Body.String())
	}
	var a inventory.Asset
	if err := s.db.Where("name = ?", "测试帽").First(&a).Error; err != nil {
		t.Fatal(err)
	}
	if a.Tag != "HKU001-52-02" {
		t.Fatalf("tag = %s", a.Tag)
	}
	form.Set("csrf", "wrong")
	r = multipartRequest(t, "/assets/new", form)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF accepted: %d", w.Code)
	}
	admin, _ := loginTest(t, s, "admin", "admin1234")
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(admin)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "href=\"/assets/1/edit\"") {
		t.Fatalf("admin list edit entry: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/users", nil)
	r.AddCookie(admin)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "创建账号") {
		t.Fatalf("admin user page: %d %s", w.Code, w.Body.String())
	}
}

func TestEditCodeOptionsReserveOldTag(t *testing.T) {
	s := testServer(t)
	editor, csrf := loginTest(t, s, "editor", "editor1234")
	form := url.Values{"csrf": {csrf}, "name": {"魔术贴板鞋"}, "category": {"鞋"}, "group": {"KIDS 120"}, "audience": {"男"}, "shoe_type": {"运动/板鞋"}, "shoe_height": {"低帮"}, "closure": {"魔术贴"}, "color": {"白色"}, "size": {"28"}, "design_no": {"1"}, "item_no": {"1"}, "keywords": {"拍摄;运动"}, "location": {"A架-02"}, "status": {"在库"}}
	post := func() {
		t.Helper()
		r := multipartRequest(t, "/assets/1/edit", form)
		r.AddCookie(editor)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("edit code options: %d %s", w.Code, w.Body.String())
		}
	}
	post()
	var a inventory.Asset
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "SKM11101-28-04" {
		t.Fatalf("new tag: %v %s", err, a.Tag)
	}
	r := httptest.NewRequest("GET", "/assets/1", nil)
	r.AddCookie(editor)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "历史标签编号") || !strings.Contains(w.Body.String(), "SKM41101-28-01") {
		t.Fatal("old tag not shown in detail")
	}
	form.Set("shoe_type", "靴子")
	form.Set("item_no", "4")
	post()
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "SKM41101-28-02" {
		t.Fatalf("old tag reused: %v %s", err, a.Tag)
	}
	form.Set("category", "帽子")
	form.Set("group", "BABY 90")
	form.Set("audience", "通用")
	form.Set("item_no", "2")
	form.Del("shoe_type")
	form.Del("shoe_height")
	form.Del("closure")
	post()
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "HBU001-28-01" || a.ShoeType != "" {
		t.Fatalf("category change: %v %s %s", err, a.Tag, a.ShoeType)
	}
}

func TestExistingAssetTagsAreReservedOnStartup(t *testing.T) {
	s := testServer(t)
	if err := s.db.Where("1 = 1").Delete(&inventory.TagReservation{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(s.db, s.dataDir); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := s.db.Model(&inventory.TagReservation{}).Count(&count).Error; err != nil || count != 9 {
		t.Fatalf("tag backfill: %v %d", err, count)
	}
}

func TestShoeStockLimit(t *testing.T) {
	s := testServer(t)
	for i := 0; i < 3; i++ {
		a := inventory.Asset{Category: "鞋", Name: "测试板鞋", Group: "KIDS 120", Audience: "男", Color: "白色", Size: "28", Status: "在库", ShoeType: "运动/板鞋", ShoeHeight: "低帮", Closure: "魔术贴", DesignNo: 1}
		if err := inventory.ValidateAsset(&a); err != nil {
			t.Fatal(err)
		}
		if err := s.createAsset(&a); err != nil {
			t.Fatal(err)
		}
		if a.ItemNo != i+4 {
			t.Fatalf("item no = %d, want %d", a.ItemNo, i+4)
		}
	}
	a := inventory.Asset{Category: "鞋", Name: "第五双", Group: "KIDS 120", Audience: "男", Color: "白色", Size: "28", Status: "在库", ShoeType: "运动/板鞋", ShoeHeight: "低帮", Closure: "魔术贴", DesignNo: 1}
	if err := inventory.ValidateAsset(&a); err != nil {
		t.Fatal(err)
	}
	if err := s.createAsset(&a); err == nil {
		t.Fatal("fifth same-color shoe accepted")
	}
}

func TestFullLedgerAndEditableNumberFields(t *testing.T) {
	s := testServer(t)
	editor, csrf := loginTest(t, s, "editor", "editor1234")
	r := httptest.NewRequest(http.MethodGet, "/?view=full", nil)
	r.AddCookie(editor)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("full ledger: %d %s", w.Code, w.Body.String())
	}
	for _, header := range []string{"照片", "标签编号·自动", "类别", "名称", "组别", "适用", "颜色", "尺码/规格", "搜索关键词", "存放位置", "状态", "鞋型", "鞋帮", "穿脱结构", "设计序号", "款式编号·自动", "规格码·自动", "单件序号", "同款同色同尺码在库", "校验"} {
		if !strings.Contains(w.Body.String(), ">"+header+"</span></th>") {
			t.Errorf("full ledger missing %s", header)
		}
	}
	r = httptest.NewRequest(http.MethodGet, "/assets/1/edit", nil)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	for _, name := range []string{"size", "design_no", "item_no", "photo"} {
		if !strings.Contains(w.Body.String(), "name=\""+name+"\"") {
			t.Errorf("edit form missing %s", name)
		}
	}
	for _, label := range []string{"标签编号·自动", "款式编号·自动", "规格码·自动", "同款同色同尺码在库", "校验"} {
		if !strings.Contains(w.Body.String(), label) {
			t.Errorf("edit form missing %s", label)
		}
	}
	form := url.Values{"csrf": {csrf}, "name": {"魔术贴板鞋"}, "category": {"鞋"}, "group": {"KIDS 120"}, "audience": {"男"}, "shoe_type": {"靴子"}, "shoe_height": {"低帮"}, "closure": {"魔术贴"}, "color": {"白色"}, "size": {"30"}, "design_no": {"1"}, "item_no": {"1"}, "keywords": {"拍摄"}, "location": {"A架-02"}, "status": {"在库"}}
	post := func(want int) *httptest.ResponseRecorder {
		t.Helper()
		r := multipartRequest(t, "/assets/1/edit", form)
		r.AddCookie(editor)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("edit: got %d want %d: %s", w.Code, want, w.Body.String())
		}
		return w
	}
	post(http.StatusSeeOther)
	var a inventory.Asset
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "SKM41101-30-01" || a.SizeCode != "30" {
		t.Fatalf("size edit: %v %+v", err, a)
	}
	form.Set("design_no", "2")
	post(http.StatusSeeOther)
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "SKM41102-30-01" || a.StyleCode != "41102" {
		t.Fatalf("design edit: %v %+v", err, a)
	}
	form.Set("item_no", "7")
	post(http.StatusSeeOther)
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "SKM41102-30-07" {
		t.Fatalf("item edit: %v %+v", err, a)
	}
	form.Set("item_no", "1")
	w = post(http.StatusOK)
	if !strings.Contains(w.Body.String(), "已使用") {
		t.Fatal("reused tag was not rejected")
	}
	if err := s.db.First(&a, 1).Error; err != nil || a.Tag != "SKM41102-30-07" {
		t.Fatalf("failed edit changed tag: %v %+v", err, a)
	}
}

func multipartRequest(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for key, values := range form {
		for _, v := range values {
			if err := mw.WriteField(key, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}
