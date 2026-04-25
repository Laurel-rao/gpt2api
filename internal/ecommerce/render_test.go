package ecommerce

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTemplate(t *testing.T) {
	got := renderTemplate("平台={{.Platform.Name}} 商品={{.Requirement}}", renderData{
		Requirement: "保温杯",
		Platform:    Platform{Name: "淘宝"},
	})
	if got != "平台=淘宝 商品=保温杯" {
		t.Fatalf("unexpected render: %q", got)
	}
}

func TestParseOutputFromJSONFence(t *testing.T) {
	raw := "```json\n{\"product_title\":\"无线耳机\",\"description\":\"低延迟\",\"marketing_copy\":[\"清晰通话\"],\"detail_sections\":[{\"title\":\"卖点\",\"body\":\"续航长\"}]}\n```"
	out := parseOutput(raw, "无线耳机")
	if out.ProductTitle != "无线耳机" {
		t.Fatalf("product title = %q", out.ProductTitle)
	}
	if out.ShopTitle == "" || out.PriceCopy == "" {
		t.Fatalf("expected normalized fields: %+v", out)
	}
	if out.PlatformFields["title"] != "无线耳机" {
		t.Fatalf("platform title missing: %+v", out.PlatformFields)
	}
}

func TestParseOutputFallback(t *testing.T) {
	out := parseOutput("not json", "智能台灯\n护眼")
	if out.ProductTitle != "智能台灯" {
		t.Fatalf("fallback title = %q", out.ProductTitle)
	}
	if len(out.DetailSections) == 0 {
		t.Fatal("expected fallback detail sections")
	}
}

func TestBuildHTML(t *testing.T) {
	out := Output{
		ProductTitle:   "儿童书包",
		Description:    "轻量护脊",
		PriceCopy:      "开学优惠",
		MarketingCopy:  []string{"大容量"},
		DetailSections: []DetailSection{{Title: "材质", Body: "耐磨面料"}},
	}
	assets := []Asset{{AssetType: AssetMain, URL: "/p/img/a/0"}}
	html := buildHTML(out, assets)
	for _, want := range []string{"儿童书包", "/p/img/a/0", "耐磨面料"} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q: %s", want, html)
		}
	}
	if !json.Valid([]byte(mustJSON(out))) {
		t.Fatal("output should stay JSON serializable")
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
