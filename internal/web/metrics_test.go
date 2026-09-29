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

func TestEditableAssetMetrics(t *testing.T) {
	s := testServer(t)
	editor, csrf := loginTest(t, s, "editor", "editor1234")
	viewer, viewerCSRF := loginTest(t, s, "viewer", "viewer1234")
	form := url.Values{"csrf": {csrf}, "category": {"帽子"}, "name": {"手动库存测试"}, "group": {"成人"}, "audience": {"通用"}, "color": {"红色"}, "size": {"52"}, "status": {"在库"}, "design_no": {"1"}, "same_stock": {"12"}, "validation": {"待校验"}}
	post := func(path string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := multipartRequest(t, path, form)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("post %s: %d want %d: %s", path, w.Code, want, w.Body.String())
		}
		return w
	}
	get := func(path string, cookie *http.Cookie) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("get %s: %d", path, w.Code)
		}
		return w.Body.String()
	}
	post("/assets/new", editor, http.StatusSeeOther)
	var asset inventory.Asset
	if err := s.db.Where("name = ?", form.Get("name")).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	path := "/assets/" + strconv.Itoa(int(asset.ID))
	if asset.SameStockOverride == nil || *asset.SameStockOverride != 12 || asset.ValidationOverride != "待校验" {
		t.Fatal("manual inventory and validation must be persisted on asset creation")
	}
	var count int64
	if err := s.db.Model(&inventory.Asset{}).Count(&count).Error; err != nil || count != 10 {
		t.Fatalf("manual stock must not create physical asset records: %v %d", err, count)
	}
	if _, err := New(s.db, s.dataDir); err != nil {
		t.Fatal(err)
	}
	detail := get(path, viewer)
	ledger := get("/?view=full", viewer)
	if !strings.Contains(detail, "12 件") || !strings.Contains(detail, "待校验") || !strings.Contains(ledger, "<td>12</td><td>待校验</td>") {
		t.Fatal("detail and full ledger must use persisted manual values after restart")
	}
	edit := get(path+"/edit", editor)
	if !strings.Contains(edit, `name="same_stock" min="0" step="1" value="12"`) || !strings.Contains(edit, `value="待校验" selected`) || !strings.Contains(edit, "自动：1 件") {
		t.Fatal("edit form must show manual values and retain the actual automatic stock")
	}
	form.Set("csrf", viewerCSRF)
	post(path+"/edit", viewer, http.StatusNotFound)
	form.Set("csrf", csrf)
	for _, invalid := range []struct{ field, value string }{
		{"same_stock", "-1"}, {"same_stock", "1.5"}, {"same_stock", "abc"}, {"same_stock", "9223372036854775808"}, {"validation", "任意状态"},
	} {
		original := form.Get(invalid.field)
		form.Set(invalid.field, invalid.value)
		post(path+"/edit", editor, http.StatusOK)
		if err := s.db.First(&asset, asset.ID).Error; err != nil || asset.SameStockOverride == nil || *asset.SameStockOverride != 12 || asset.ValidationOverride != "待校验" {
			t.Fatalf("invalid %s must not change saved values: %v %+v", invalid.field, err, asset)
		}
		form.Set(invalid.field, original)
	}
	form.Set("same_stock", "0")
	form.Set("validation", "异常")
	post(path+"/edit", editor, http.StatusSeeOther)
	if err := s.db.First(&asset, asset.ID).Error; err != nil || asset.SameStockOverride == nil || *asset.SameStockOverride != 0 || asset.ValidationOverride != "异常" {
		t.Fatalf("zero must be saved as a manual count: %v %+v", err, asset)
	}
	edit = get(path+"/edit", editor)
	if !strings.Contains(edit, `name="same_stock" min="0" step="1" value="0"`) || !strings.Contains(get(path, viewer), "0 件") {
		t.Fatal("a manual zero must not fall back to the automatic stock")
	}
	form.Set("validation", "正常")
	post(path+"/edit", editor, http.StatusSeeOther)
	if !strings.Contains(get(path+"/edit", editor), `value="正常" selected`) {
		t.Fatal("normal must be selectable as a manual validation status")
	}
	form.Set("same_stock", "")
	form.Set("validation", "")
	post(path+"/edit", editor, http.StatusSeeOther)
	var automatic inventory.Asset
	if err := s.db.First(&automatic, asset.ID).Error; err != nil || automatic.SameStockOverride != nil || automatic.ValidationOverride != "" {
		t.Fatalf("clearing manual values must restore automatic calculation: %v %+v", err, automatic)
	}
	detail = get(path, viewer)
	if !strings.Contains(detail, "1 件") || !strings.Contains(detail, `class="text-success">正常`) {
		t.Fatal("restored automatic values must use actual stock and system validation")
	}
	second := inventory.Asset{Category: "帽子", Name: "同规格测试", Group: "成人", Audience: "通用", Color: "红色", Size: "52", Status: "在库", DesignNo: 1}
	if err := inventory.ValidateAsset(&second); err != nil {
		t.Fatal(err)
	}
	if err := s.createAsset(&second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(get(path, viewer), "2 件") {
		t.Fatal("automatic stock must still track subsequent physical records")
	}
}

func TestManualMetricsDoNotBypassShoeStockLimit(t *testing.T) {
	s := testServer(t)
	editor, csrf := loginTest(t, s, "editor", "editor1234")
	form := url.Values{"csrf": {csrf}, "category": {"鞋"}, "name": {"手动在库鞋"}, "group": {"KIDS 120"}, "audience": {"男"}, "shoe_type": {"运动/板鞋"}, "shoe_height": {"低帮"}, "closure": {"魔术贴"}, "color": {"白色"}, "size": {"28"}, "status": {"在库"}, "design_no": {"1"}, "same_stock": {"0"}, "validation": {"正常"}}
	for i := range 4 {
		r := multipartRequest(t, "/assets/new", form)
		r.AddCookie(editor)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if i < 3 {
			if w.Code != http.StatusSeeOther {
				t.Fatalf("allowed shoe creation: %d %s", w.Code, w.Body.String())
			}
		} else if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "最多 4 双") {
			t.Fatal("manual zero and normal status must not bypass the physical four-pair limit")
		}
	}
}
