package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"eidosinventory/internal/inventory"
)

func postOption(t *testing.T, s *Server, cookie *http.Cookie, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestOptionAdministrationAndAssetCodes(t *testing.T) {
	s := testServer(t)
	viewer, _ := loginTest(t, s, "viewer", "viewer1234")
	editor, editorCSRF := loginTest(t, s, "editor", "editor1234")
	for _, cookie := range []*http.Cookie{viewer, editor} {
		r := httptest.NewRequest(http.MethodGet, "/options", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("non-admin options page: %d", w.Code)
		}
	}
	admin, csrf := loginTest(t, s, "admin", "admin1234")
	denied := postOption(t, s, editor, "/options", url.Values{"csrf": {editorCSRF}, "field": {"group"}, "name": {"不应新增"}, "code": {"T"}})
	if denied.Code != http.StatusNotFound {
		t.Fatalf("editor changed options: %d", denied.Code)
	}
	denied = postOption(t, s, admin, "/options", url.Values{"csrf": {"invalid"}, "field": {"group"}, "name": {"不应新增"}, "code": {"T"}})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF changed options: %d", denied.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/options", nil)
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "可选值管理") || !strings.Contains(w.Body.String(), "鞋帮") {
		t.Fatalf("admin options page: %d %s", w.Code, w.Body.String())
	}

	var shoe inventory.FieldOption
	if err := s.db.Where("field = ? AND code = ?", "category", "S").First(&shoe).Error; err != nil {
		t.Fatal(err)
	}
	w = postOption(t, s, admin, "/options/"+strconv.Itoa(int(shoe.ID)), url.Values{"csrf": {csrf}, "name": {"鞋履"}, "active": {"1"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("rename shoe: %d %s", w.Code, w.Body.String())
	}
	var asset inventory.Asset
	if err := s.db.First(&asset, 1).Error; err != nil || asset.Category != "鞋履" || asset.Tag != "SKM41101-28-01" {
		t.Fatalf("shoe rename damaged asset: %v %+v", err, asset)
	}
	if _, err := New(s.db, s.dataDir); err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&shoe, shoe.ID).Error; err != nil || shoe.Name != "鞋履" {
		t.Fatalf("shoe rename lost on restart: %v %s", err, shoe.Name)
	}
	r = httptest.NewRequest(http.MethodGet, "/assets/1", nil)
	r.AddCookie(admin)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "鞋类属性") || !strings.Contains(w.Body.String(), "正常") {
		t.Fatalf("shoe detail after rename: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/assets/1/edit", nil)
	r.AddCookie(admin)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "value=\"鞋履\" selected") {
		t.Fatal("renamed option missing from edit form")
	}

	w = postOption(t, s, admin, "/options/"+strconv.Itoa(int(shoe.ID)), url.Values{"csrf": {csrf}, "name": {"鞋履"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "不能停用") {
		t.Fatalf("used option was disabled: %d %s", w.Code, w.Body.String())
	}

	w = postOption(t, s, admin, "/options", url.Values{"csrf": {csrf}, "field": {"group"}, "name": {"青少年"}, "code": {"T"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("add option: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/assets/new", nil)
	r.AddCookie(admin)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "value=\"青少年\"") {
		t.Fatal("new option missing from form")
	}
	asset = inventory.Asset{Category: "帽子", Name: "测试帽", Group: "青少年", Audience: "通用", Color: "红色", Size: "52", Status: "在库", DesignNo: 1}
	_, codes, err := s.options()
	if err != nil {
		t.Fatal(err)
	}
	if err := inventory.ValidateAssetWithOptions(&asset, codes); err != nil {
		t.Fatal(err)
	}
	if err := s.createAsset(&asset); err != nil || asset.Tag != "HTU001-52-01" {
		t.Fatalf("new option code: %v %s", err, asset.Tag)
	}

	var group inventory.FieldOption
	if err := s.db.Where("field = ? AND code = ?", "group", "T").First(&group).Error; err != nil {
		t.Fatal(err)
	}
	w = postOption(t, s, admin, "/options/"+strconv.Itoa(int(group.ID)), url.Values{"csrf": {csrf}, "name": {"青少年"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "不能停用") {
		t.Fatalf("used group disabled: %d", w.Code)
	}
}

func TestOptionValidationAndUnusedDeactivation(t *testing.T) {
	s := testServer(t)
	admin, csrf := loginTest(t, s, "admin", "admin1234")
	w := postOption(t, s, admin, "/options", url.Values{"csrf": {csrf}, "field": {"category"}, "name": {"测试类"}, "code": {"S"}})
	if w.Code != http.StatusOK {
		t.Fatalf("duplicate shoe code accepted: %d", w.Code)
	}
	w = postOption(t, s, admin, "/options", url.Values{"csrf": {csrf}, "field": {"closure"}, "name": {"拉链"}, "code": {"5"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("add closure: %d", w.Code)
	}
	var option inventory.FieldOption
	if err := s.db.Where("field = ? AND name = ?", "closure", "拉链").First(&option).Error; err != nil {
		t.Fatal(err)
	}
	w = postOption(t, s, admin, "/options/"+strconv.Itoa(int(option.ID)), url.Values{"csrf": {csrf}, "name": {"拉链"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("disable unused: %d", w.Code)
	}
	_, codes, err := s.options()
	if err != nil {
		t.Fatal(err)
	}
	if codes.Closure["拉链"] != "" {
		t.Fatal("disabled option still selectable")
	}
}

func TestColorSizeAndLocationOptions(t *testing.T) {
	s := testServer(t)
	admin, csrf := loginTest(t, s, "admin", "admin1234")
	editor, editorCSRF := loginTest(t, s, "editor", "editor1234")
	getForm := func() string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/assets/new", nil)
		r.AddCookie(editor)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("asset form: %d", w.Code)
		}
		return w.Body.String()
	}
	formHTML := getForm()
	for _, field := range []string{"color", "size", "location"} {
		if !strings.Contains(formHTML, `<select class="form-select" id="`+field+`" name="`+field+`"`) {
			t.Errorf("%s must use a selection control", field)
		}
	}
	for _, name := range []string{"白色", "蓝色", "均码", "S", "XL", "28", "52", "120", "A架-02", "E架-01"} {
		if !strings.Contains(formHTML, `value="`+name+`"`) {
			t.Errorf("common option %s missing from form", name)
		}
	}
	for _, option := range []struct{ field, name string }{{"color", "银色"}, {"size", "47"}, {"location", "摄影区"}} {
		w := postOption(t, s, admin, "/options", url.Values{"csrf": {csrf}, "field": {option.field}, "name": {option.name}})
		if w.Code != http.StatusSeeOther {
			t.Fatalf("add %s without a label code: %d %s", option.field, w.Code, w.Body.String())
		}
	}
	w := postOption(t, s, admin, "/options", url.Values{"csrf": {csrf}, "field": {"size"}, "name": {"不合法规格"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "无效") {
		t.Fatal("size options must obey the label numbering rules")
	}
	form := url.Values{"csrf": {editorCSRF}, "category": {"帽子"}, "name": {"可选值测试帽"}, "group": {"成人"}, "audience": {"通用"}, "color": {"银色"}, "size": {"47"}, "location": {"摄影区"}, "status": {"在库"}, "design_no": {"1"}}
	postAsset := func(want int) *httptest.ResponseRecorder {
		t.Helper()
		r := multipartRequest(t, "/assets/new", form)
		r.AddCookie(editor)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("create asset: %d want %d: %s", w.Code, want, w.Body.String())
		}
		return w
	}
	for _, field := range []string{"color", "size", "location"} {
		old := form.Get(field)
		form.Set(field, "未配置的值")
		w = postAsset(http.StatusOK)
		if !strings.Contains(w.Body.String(), "选项无效") {
			t.Errorf("unconfigured %s was accepted", field)
		}
		form.Set(field, old)
	}
	postAsset(http.StatusSeeOther)
	var asset inventory.Asset
	if err := s.db.Where("name = ?", form.Get("name")).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if asset.Tag != "HAU001-47-01" || asset.Color != "银色" || asset.Location != "摄影区" {
		t.Fatalf("selected values were not saved with the correct label: %+v", asset)
	}
	for _, change := range []struct{ field, oldName, newName string }{{"color", "银色", "银灰色"}, {"location", "摄影区", "摄影间"}, {"size", "47", "49"}} {
		var option inventory.FieldOption
		if err := s.db.Where("field = ? AND name = ?", change.field, change.oldName).First(&option).Error; err != nil {
			t.Fatal(err)
		}
		path := "/options/" + strconv.Itoa(int(option.ID))
		w = postOption(t, s, admin, path, url.Values{"csrf": {csrf}, "name": {change.oldName}})
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "不能停用") {
			t.Fatalf("used %s option must remain active", change.field)
		}
		w = postOption(t, s, admin, path, url.Values{"csrf": {csrf}, "name": {change.newName}, "active": {"1"}})
		if change.field == "size" {
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "影响标签编号") {
				t.Fatal("used size must not be renamed to another label specification")
			}
		} else if w.Code != http.StatusSeeOther {
			t.Fatalf("rename %s: %d %s", change.field, w.Code, w.Body.String())
		}
	}
	if err := s.db.First(&asset, asset.ID).Error; err != nil || asset.Color != "银灰色" || asset.Location != "摄影间" || asset.Size != "47" || asset.Tag != "HAU001-47-01" {
		t.Fatalf("option maintenance must preserve label identity and update asset values: %v %+v", err, asset)
	}
	var unused inventory.FieldOption
	if err := s.db.Where("field = ? AND name = ?", "color", "紫色").First(&unused).Error; err != nil {
		t.Fatal(err)
	}
	w = postOption(t, s, admin, "/options/"+strconv.Itoa(int(unused.ID)), url.Values{"csrf": {csrf}, "name": {unused.Name}})
	if w.Code != http.StatusSeeOther {
		t.Fatal("unused color must be deactivatable")
	}
	if _, err := New(s.db, s.dataDir); err != nil {
		t.Fatal(err)
	}
	formHTML = getForm()
	if strings.Contains(formHTML, `value="紫色"`) || strings.Contains(formHTML, `value="银色"`) || !strings.Contains(formHTML, `value="银灰色"`) {
		t.Fatal("restart must preserve option renames and deactivation")
	}
	_, codes, err := s.options()
	if err != nil {
		t.Fatal(err)
	}
	asset.Color = "紫色"
	if err := inventory.ValidateAssetWithOptions(&asset, codes); err == nil {
		t.Fatal("disabled colors must be rejected when saving")
	}
}

func TestLegacyColorSizeAndLocationBecomeOptions(t *testing.T) {
	s := testServer(t)
	location := strings.Repeat("长", 100)
	if err := s.db.Model(&inventory.Asset{}).Where("id = ?", 9).Updates(map[string]any{"color": "深蓝色", "size": "75", "size_code": "75", "tag": "PAU001-75-01", "location": location}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := New(s.db, s.dataDir); err != nil {
			t.Fatal(err)
		}
	}
	for _, option := range []struct{ field, name string }{{"color", "深蓝色"}, {"size", "75"}, {"location", location}} {
		var count int64
		if err := s.db.Model(&inventory.FieldOption{}).Where("field = ? AND name = ? AND active = ?", option.field, option.name, true).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("legacy %s must become exactly one active option: %v %d", option.field, err, count)
		}
	}
	editor, csrf := loginTest(t, s, "editor", "editor1234")
	r := httptest.NewRequest(http.MethodGet, "/assets/9/edit", nil)
	r.AddCookie(editor)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	for _, value := range []string{"深蓝色", "75", location} {
		if !strings.Contains(w.Body.String(), `value="`+value+`" selected`) {
			t.Errorf("legacy value must remain selected: %s", value)
		}
	}
	form := url.Values{"csrf": {csrf}, "category": {"道具"}, "name": {"木质手提箱"}, "group": {"成人"}, "audience": {"通用"}, "color": {"深蓝色"}, "size": {"75"}, "location": {location}, "status": {"在库"}, "design_no": {"1"}, "item_no": {"1"}}
	r = multipartRequest(t, "/assets/9/edit", form)
	r.AddCookie(editor)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("legacy selections must still be editable: %d %s", w.Code, w.Body.String())
	}
	var asset inventory.Asset
	if err := s.db.First(&asset, 9).Error; err != nil || asset.Tag != "PAU001-75-01" || asset.Location != location {
		t.Fatalf("editing legacy selections changed label or location: %v %+v", err, asset)
	}
}
