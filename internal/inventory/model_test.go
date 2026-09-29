package inventory

import "testing"

func TestCodeParts(t *testing.T) {
	cases := []struct {
		a                   Asset
		prefix, style, size string
	}{
		{Asset{Category: "鞋", Group: "KIDS 120", Audience: "男", ShoeType: "运动/板鞋", ShoeHeight: "低帮", Closure: "魔术贴", DesignNo: 1, Size: "28"}, "SKM", "11101", "28"},
		{Asset{Category: "帽子", Group: "KIDS 120", Audience: "通用", DesignNo: 1, Size: "52"}, "HKU", "001", "52"},
		{Asset{Category: "围巾", Group: "BABY 90", Audience: "通用", DesignNo: 1, Size: "均码"}, "WBU", "001", "00"},
		{Asset{Category: "服装", Group: "KIDS 120", Audience: "女", DesignNo: 1, Size: "120"}, "CKF", "001", "120"},
	}
	for _, c := range cases {
		p, s, z, err := CodeParts(&c.a)
		if err != nil || p != c.prefix || s != c.style || z != c.size {
			t.Errorf("%+v -> %s %s %s, %v", c.a, p, s, z, err)
		}
	}
}

func TestInvalidCodeParts(t *testing.T) {
	a := Asset{Category: "鞋", Group: "KIDS 120", Audience: "男", DesignNo: 1, Size: "28"}
	if _, _, _, err := CodeParts(&a); err == nil {
		t.Fatal("shoe without structure accepted")
	}
	a = Asset{Category: "帽子", Group: "成人", Audience: "通用", DesignNo: 100, Size: "均码"}
	if _, _, _, err := CodeParts(&a); err == nil {
		t.Fatal("design number 100 accepted")
	}
}
