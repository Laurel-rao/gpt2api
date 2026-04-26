package ecommerce

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	imgpkg "github.com/432539/gpt2api/internal/image"
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
	assets := []Asset{{AssetType: AssetMain, URL: "/p/img/a/0"}, {AssetType: AssetWhite, URL: "/p/img/white/0"}}
	html := buildHTML(out, assets)
	for _, want := range []string{"儿童书包", "/p/img/a/0", "耐磨面料"} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "/p/img/white/0") || strings.Contains(html, "白底图") {
		t.Fatalf("detail page html should not include white asset: %s", html)
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

func TestNormalizeOutputResetsUniformImageSpecs(t *testing.T) {
	out := Output{
		ProductTitle: "可收纳钢琴",
		Description:  "便携收纳",
		ImageSpecs: map[string]ImageSpec{
			AssetTitle:  {Size: "1024x1024"},
			AssetMain:   {Size: "1024x1024"},
			AssetWhite:  {Size: "1024x1024"},
			AssetDetail: {Size: "1024x1024"},
			AssetPrice:  {Size: "1024x1024"},
		},
	}
	normalizeOutput(&out, "可收纳钢琴")
	if out.ImageSpecs[AssetTitle].Size != "1792x1024" {
		t.Fatalf("title spec = %+v", out.ImageSpecs[AssetTitle])
	}
	if out.ImageSpecs[AssetDetail].Size != "1024x1792" || out.ImageSpecs[AssetPrice].Size != "1024x1792" {
		t.Fatalf("vertical specs = detail:%+v price:%+v", out.ImageSpecs[AssetDetail], out.ImageSpecs[AssetPrice])
	}
	if out.ImageSpecs[AssetMain].Size != "1024x1024" || out.ImageSpecs[AssetWhite].Size != "1024x1024" {
		t.Fatalf("square specs = main:%+v white:%+v", out.ImageSpecs[AssetMain], out.ImageSpecs[AssetWhite])
	}
}

func TestNormalizeImageSpecMapsUnsupportedVerticalSize(t *testing.T) {
	got := normalizeImageSpec(ImageSpec{Size: "1024*1536"}, ImageSpec{Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"})
	if got.Size != "1024x1792" || got.AspectRatio != "4:7" {
		t.Fatalf("spec = %+v", got)
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

func TestBuildEnglishDetailPromptRequiresSceneCanvas(t *testing.T) {
	out := Output{
		ProductTitle: "Ma Jia Sofa",
		Description:  "High-End Comfort Experience",
		ProductInfo: ProductInfo{
			CanonicalTitle: "Ma Jia Sofa",
			ShortTitle:     "Ma Jia Sofa",
			SellingPoints:  []string{"Luxurious and comfortable design"},
		},
	}
	normalizeOutput(&out, "Ma Jia Sofa")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildImagePrompt(
		Platform{Name: "Amazon", Language: "en-US"},
		PromptTemplate{ImagePrompt: "Amazon product image"},
		StyleTemplate{StylePrompt: "fresh lifestyle scene"},
		out,
		"Ma Jia Sofa",
		AssetDetail,
	)
	for _, want := range []string{"full-bleed home/outdoor scene", "colored design canvas", "integrate the product into the scene"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("detail prompt missing %q: %s", want, prompt)
		}
	}
	for _, unwanted := range []string{"Price text:", "Promotion text:", "CTA:"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("detail prompt should not include %q: %s", unwanted, prompt)
		}
	}
}

func TestBuildRetryPromptUsesRawExtraWithoutPrefix(t *testing.T) {
	out := Output{
		ProductTitle: "JBK Bluetooth Speaker",
		Description:  "Powerful sound with ultra-long standby time.",
		ProductInfo: ProductInfo{
			CanonicalTitle: "JBK Bluetooth Speaker",
			ShortTitle:     "JBK Bluetooth Speaker",
			KeySpecs:       []string{"Ultra-long standby"},
			SellingPoints:  []string{"Portable and sleek"},
		},
	}
	normalizeOutput(&out, "JBK 蓝牙音响，超长待机一个月")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildRetryImagePrompt(
		Platform{Name: "Shopify", Language: "en-US"},
		PromptTemplate{ImagePrompt: "seed content"},
		StyleTemplate{StylePrompt: "soft pastel"},
		out,
		"JBK 蓝牙音响，超长待机一个月",
		AssetDetail,
		"Apple minimal style, exploded core-components diagram.",
	)
	if !strings.Contains(prompt, "Apple minimal style, exploded core-components diagram.") {
		t.Fatalf("retry prompt missing raw extra: %s", prompt)
	}
	if strings.Contains(prompt, "重试追加描述词（高优先级）") {
		t.Fatalf("retry prompt should not inject retry prefix: %s", prompt)
	}
}

func TestRetryReferencesForNonWhiteUsesOriginalReferences(t *testing.T) {
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	fallback := []imgpkg.ReferenceImage{{Data: []byte("original"), FileName: "original.png"}}
	got := r.retryReferences(context.Background(), "task-1", Asset{
		AssetType:   AssetDetail,
		Status:      StatusSuccess,
		ImageTaskID: "generated-detail",
	}, fallback)
	if len(got) != 1 || got[0].FileName != "original.png" || string(got[0].Data) != "original" {
		t.Fatalf("non-white retry should use original references, got %+v", got)
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
