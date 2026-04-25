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

func TestNormalizeOutputBuildsConsistentImagePlans(t *testing.T) {
	out := Output{
		ProductTitle: "800可收纳钢琴",
		Description:  "可折叠便携电子琴",
		PriceCopy:    "限时到手价 2399",
		ProductInfo: ProductInfo{
			CanonicalTitle: "800可收纳钢琴",
			ShortTitle:     "800可收纳钢琴",
			KeySpecs:       []string{"可折叠", "便携收纳"},
			SellingPoints:  []string{"节省空间"},
		},
		PriceInfo: PriceInfo{
			Currency:  "CNY",
			SalePrice: "2399",
			PriceText: "到手价 2399",
			CTA:       "立即购买",
		},
		ImageTextPlans: map[string]ImageTextPlan{
			AssetTitle: {Title: "其他标题", PriceText: "1299"},
			AssetPrice: {Title: "其他标题", PriceText: "1299"},
		},
	}
	normalizeOutput(&out, "800 的可收纳钢琴，价格 2399")
	if out.ImageTextPlans[AssetPrice].PriceText != "到手价 2399" {
		t.Fatalf("price plan mismatch: %+v", out.ImageTextPlans[AssetPrice])
	}
	if out.ImageTextPlans[AssetTitle].Title != "800可收纳钢琴" || out.ImageTextPlans[AssetTitle].PriceText != "到手价 2399" {
		t.Fatalf("title image plan should be unified: %+v", out.ImageTextPlans[AssetTitle])
	}
	if out.ImageTextPlans[AssetWhite].PriceText != "" || len(out.ImageTextPlans[AssetWhite].SellingPoints) != 0 {
		t.Fatalf("white image should not contain text plan: %+v", out.ImageTextPlans[AssetWhite])
	}
}

func TestBuildImagePromptUsesUnifiedPrice(t *testing.T) {
	out := Output{
		ProductTitle: "800可收纳钢琴",
		Description:  "可折叠便携电子琴",
		PriceCopy:    "限时到手价 2399",
		ProductInfo: ProductInfo{
			CanonicalTitle: "800可收纳钢琴",
			ShortTitle:     "800可收纳钢琴",
		},
		PriceInfo: PriceInfo{
			PriceText: "到手价 2399",
			CTA:       "立即购买",
		},
	}
	normalizeOutput(&out, "800 的可收纳钢琴，价格 2399")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildImagePrompt(
		Platform{Name: "抖音电商"},
		PromptTemplate{ImagePrompt: "促销图"},
		StyleTemplate{StylePrompt: "红白风格"},
		out,
		"800 的可收纳钢琴，价格 2399",
		AssetPrice,
	)
	for _, want := range []string{"统一商品信息", "本图允许出现的文字", "到手价 2399", "不得新增、替换、改写任何数字价格"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestRunnerImageConcurrencyDefault(t *testing.T) {
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 0)
	if r.imageConcurrency != 1 {
		t.Fatalf("image concurrency default = %d", r.imageConcurrency)
	}
	if cap(r.imageSem) != 1 {
		t.Fatalf("image semaphore cap = %d", cap(r.imageSem))
	}
}

func TestRunnerImageConcurrencyCustom(t *testing.T) {
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 3)
	if r.imageConcurrency != 3 {
		t.Fatalf("image concurrency custom = %d", r.imageConcurrency)
	}
	if cap(r.imageSem) != 3 {
		t.Fatalf("image semaphore cap = %d", cap(r.imageSem))
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
