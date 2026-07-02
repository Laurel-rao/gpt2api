package ecommerce

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	imgpkg "github.com/432539/gpt2api/internal/image"
	"github.com/432539/gpt2api/internal/videogen"
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

func TestLatestAssetsByTypeUsesNewestAsset(t *testing.T) {
	assets := []Asset{
		{ID: 1, AssetType: AssetVideo, Status: StatusFailed, Error: "first failed"},
		{ID: 2, AssetType: AssetMain, Status: StatusSuccess, URL: "/old-main.png"},
		{ID: 3, AssetType: AssetVideo, Status: StatusSuccess, URL: "/video.mp4"},
		{ID: 4, AssetType: AssetMain, Status: StatusSuccess, URL: "/new-main.png"},
	}
	got := latestAssetsByType(assets)
	byType := map[string]Asset{}
	for _, asset := range got {
		byType[asset.AssetType] = asset
	}
	if byType[AssetVideo].Status != StatusSuccess || byType[AssetVideo].Error != "" {
		t.Fatalf("video should use newest success asset: %+v", byType[AssetVideo])
	}
	if byType[AssetMain].URL != "/new-main.png" {
		t.Fatalf("main image should use newest asset: %+v", byType[AssetMain])
	}
	if len(got) != 2 {
		t.Fatalf("expected one asset per type, got %+v", got)
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

func TestBuildChineseMainPromptUsesFourLayers(t *testing.T) {
	out := Output{
		ProductTitle: "折叠电钢琴",
		Description:  "便携可收纳",
		ProductInfo: ProductInfo{
			CanonicalTitle: "折叠电钢琴",
			ShortTitle:     "折叠电钢琴",
			SellingPoints:  []string{"折叠收纳"},
		},
	}
	normalizeOutput(&out, "折叠电钢琴，便携可收纳")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildImagePrompt(
		Platform{Name: "抖音电商"},
		PromptTemplate{ImagePrompt: "科技感电商主图"},
		StyleTemplate{StylePrompt: "干净高级"},
		out,
		"折叠电钢琴，便携可收纳",
		AssetMain,
	)
	for _, want := range []string{
		"【1. 主体保真层】",
		"【2. 当前图片任务层】",
		"【3. 风格提示词层】",
		"【4. 负面提示词层】",
		"严格对照参考图主体",
		"资产差异规则",
		"主体约占画面 45%-60%",
		"不能生成白底大商品特写",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildWhitePromptPreservesProductBrandLogo(t *testing.T) {
	out := Output{
		ProductTitle: "SU7 牛奶礼盒",
		Description:  "礼盒装牛奶",
		ProductInfo: ProductInfo{
			CanonicalTitle: "SU7 牛奶礼盒",
			ShortTitle:     "SU7 牛奶",
			KeySpecs:       []string{"品牌：SU7", "礼盒包装"},
		},
	}
	normalizeOutput(&out, "SU7 牛奶礼盒，保留包装品牌 logo")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildImagePrompt(
		Platform{Name: "Shopify", Language: "en-US"},
		PromptTemplate{ImagePrompt: "clean ecommerce product image"},
		StyleTemplate{StylePrompt: "premium retail"},
		out,
		"SU7 牛奶礼盒，保留包装品牌 logo",
		AssetWhite,
	)
	for _, want := range []string{
		"Brand logos, trademarks, package text, labels",
		"must be preserved",
		"already printed on the product/package must be preserved",
		"already present on the reference product/package must be preserved",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("brand preservation prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildTrumpMoodPromptDoesNotClaimRealEndorsement(t *testing.T) {
	out := Output{
		ProductTitle: "折叠电钢琴",
		Description:  "便携可收纳",
		ProductInfo: ProductInfo{
			CanonicalTitle: "折叠电钢琴",
			ShortTitle:     "折叠电钢琴",
		},
	}
	normalizeOutput(&out, "折叠电钢琴，要求特朗普代言")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildImagePrompt(
		Platform{Name: "抖音电商"},
		PromptTemplate{ImagePrompt: "竞选海报风格"},
		StyleTemplate{StylePrompt: "红白蓝强视觉"},
		out,
		"折叠电钢琴，要求特朗普代言",
		AssetMain,
	)
	for _, want := range []string{
		"人物/代言氛围处理",
		"高优先级执行",
		"特朗普式代言广告",
		"第 2 层的人物/代言视觉必须执行并突出",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("trump mood prompt missing %q: %s", want, prompt)
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
	for _, want := range []string{
		"full-bleed home/outdoor scene",
		"colored design canvas",
		"integrate the product into the scene",
		"must use different compositions",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("detail prompt missing %q: %s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		"Reuse the title image campaign master",
		"All non-white-background assets must share the same visual master",
		"without changing background, dominant colors or product angle",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("detail prompt should not include old same-master rule %q: %s", unwanted, prompt)
		}
	}
	for _, unwanted := range []string{"Price text:", "Promotion text:", "CTA:"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("detail prompt should not include %q: %s", unwanted, prompt)
		}
	}
}

func TestBuildChinesePricePromptKeepsPromotionRole(t *testing.T) {
	out := Output{
		ProductTitle: "折叠电钢琴",
		Description:  "便携可收纳",
		ProductInfo: ProductInfo{
			CanonicalTitle: "折叠电钢琴",
			ShortTitle:     "折叠电钢琴",
		},
		PriceInfo: PriceInfo{
			PriceText: "到手价 2399",
			CTA:       "立即购买",
		},
	}
	normalizeOutput(&out, "折叠电钢琴，到手价 2399")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildImagePrompt(
		Platform{Name: "抖音电商"},
		PromptTemplate{ImagePrompt: "促销图"},
		StyleTemplate{StylePrompt: "红白强转化"},
		out,
		"折叠电钢琴，到手价 2399",
		AssetPrice,
	)
	for _, want := range []string{
		"竖版促销图",
		"价格与行动号召优先",
		"促销层级必须强于商品展示",
		"不得生成成与主图相同的单商品海报",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("price prompt missing %q: %s", want, prompt)
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

func TestBuildVideoPromptUsesTemplateFields(t *testing.T) {
	out := Output{
		ProductTitle: "折叠电钢琴",
		Description:  "便携可收纳",
		ProductInfo: ProductInfo{
			CanonicalTitle: "折叠电钢琴",
			CoreValue:      "小户型也能练琴",
			KeySpecs:       []string{"可折叠", "88 键"},
			SellingPoints:  []string{"收纳方便", "音色真实"},
		},
		PriceInfo: PriceInfo{
			PriceText: "到手价 2399",
		},
		MarketingCopy: []string{"开学季推荐", "限时优惠"},
	}
	normalizeOutput(&out, "折叠电钢琴，到手价 2399")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildVideoPrompt(
		Platform{Name: "抖音电商"},
		PromptTemplate{
			ImagePrompt: "科技感主图",
			VideoPrompt: "视频模板：{{.Platform.Name}}|{{.ProductTitle}}|{{.ProductCoreValue}}|{{.SellingPointsText}}|{{.KeySpecsText}}|{{.MarketingCopyText}}|{{.PriceText}}|{{.VisualDirection}}",
		},
		StyleTemplate{StylePrompt: "高级柔光"},
		out,
		"折叠电钢琴，到手价 2399",
	)
	for _, want := range []string{"视频模板：抖音电商", "折叠电钢琴", "小户型也能练琴", "收纳方便 / 音色真实", "可折叠 / 88 键", "开学季推荐 / 限时优惠", "到手价 2399", "科技感主图 高级柔光"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("video prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildRetryVideoPromptAppendsExtraWhenTemplateOmitsIt(t *testing.T) {
	out := Output{ProductTitle: "无线耳机", Description: "降噪长续航"}
	normalizeOutput(&out, "无线耳机，降噪长续航")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildRetryVideoPrompt(
		Platform{Name: "淘宝"},
		PromptTemplate{VideoPrompt: "只渲染商品：{{.ProductTitle}}"},
		StyleTemplate{},
		out,
		"无线耳机，降噪长续航",
		"增加佩戴场景",
	)
	if !strings.Contains(prompt, "只渲染商品：无线耳机") {
		t.Fatalf("video prompt missing template output: %s", prompt)
	}
	if !strings.Contains(prompt, "本次重试额外要求：增加佩戴场景") {
		t.Fatalf("video prompt should append retry extra: %s", prompt)
	}
}

func TestBuildVideoPromptFallsBackWhenTemplateEmpty(t *testing.T) {
	out := Output{ProductTitle: "无线耳机", Description: "降噪长续航"}
	normalizeOutput(&out, "无线耳机，降噪长续航")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildVideoPrompt(
		Platform{Name: "淘宝"},
		PromptTemplate{VideoPrompt: ""},
		StyleTemplate{},
		out,
		"无线耳机，降噪长续航",
	)
	for _, want := range []string{
		"生成一支 5 秒精致电商商品短视频。",
		"分秒脚本：每一秒都必须同时交代镜头、灯光、背景、声音和细节描述。",
		"0-1 秒：镜头：商品主视觉开场",
		"灯光：柔和主光加边缘轮廓光",
		"背景：干净电商场景或品牌色块",
		"声音：轻柔开场音效或低音氛围铺底",
		"细节描述：突出商品轮廓、材质、颜色和真实比例",
		"4-5 秒：镜头：形成电商海报式结束定帧",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default video prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildEnglishVideoPromptFallsBackWithStoryboard(t *testing.T) {
	out := Output{ProductTitle: "Wireless Earbuds", Description: "Noise cancellation and long battery life"}
	normalizeOutput(&out, "Wireless Earbuds, noise cancellation and long battery life")
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 1)
	prompt := r.buildVideoPrompt(
		Platform{Name: "Shopify", Language: "en-US"},
		PromptTemplate{VideoPrompt: ""},
		StyleTemplate{},
		out,
		"Wireless Earbuds, noise cancellation and long battery life",
	)
	for _, want := range []string{
		"Create a polished 5-second ecommerce product short video.",
		"Second-by-second script: each second must specify camera, lighting, background, sound and detail description.",
		"0-1s: Camera: hero product reveal",
		"Lighting: soft key light with rim light",
		"Background: clean ecommerce set or brand color blocks",
		"Sound: gentle opening hit or low ambient bed",
		"Details: emphasize product silhouette, material, color and true scale",
		"4-5s: Camera: ecommerce poster-like final frame",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("english default video prompt missing %q: %s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "Visible text language: Visible text must stay in English") {
		t.Fatalf("empty video template should use default prompt: %s", prompt)
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

func TestReferencesForNonWhitePreferWhiteAnchor(t *testing.T) {
	original := []imgpkg.ReferenceImage{{Data: []byte("original"), FileName: "original.png"}}
	white := []imgpkg.ReferenceImage{{Data: []byte("white"), FileName: "white.png"}}
	got := referencesForAsset(AssetMain, original, white)
	if len(got) != 1 || got[0].FileName != "white.png" || string(got[0].Data) != "white" {
		t.Fatalf("non-white assets should use white anchor references, got %+v", got)
	}
	got = referencesForAsset(AssetWhite, original, white)
	if len(got) != 1 || got[0].FileName != "original.png" || string(got[0].Data) != "original" {
		t.Fatalf("white asset should use original references, got %+v", got)
	}
}

func TestVideoReferenceImagesUseProductAnchorDataURL(t *testing.T) {
	images, err := videoReferenceImages([]imgpkg.ReferenceImage{{
		Data:     []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
		FileName: "white.png",
	}})
	if err != nil {
		t.Fatalf("videoReferenceImages error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("images len = %d", len(images))
	}
	if images[0].Name != videoProductAnchorName {
		t.Fatalf("image name = %q", images[0].Name)
	}
	if !strings.HasPrefix(images[0].URL, "data:image/png;base64,") {
		t.Fatalf("image url should be png data URL, got %q", images[0].URL)
	}
	prompt := withVideoReferencePrompt("生成商品短视频")
	if !strings.Contains(prompt, "@"+videoProductAnchorName) {
		t.Fatalf("prompt should reference product anchor: %s", prompt)
	}
}

func TestComputeVideoBillingCostUsesPlatformCreditsRatio(t *testing.T) {
	got := computeVideoBillingCost(1.25, 10)
	if got != 125000 {
		t.Fatalf("video billing cost = %d", got)
	}
	if computeVideoBillingCost(0, 10) != 0 {
		t.Fatal("zero platform cost should not bill")
	}
}

func TestVideoBillingCompletedCostRules(t *testing.T) {
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, 0)
	r.SetBilling(nil, staticVideoRatio{ratio: 10})
	if cost, _ := r.completedVideoBillingCost(&videogen.Result{
		CostType:   "credits",
		CostDetail: videogen.CostDetail{Price: 1.25},
	}); cost != 125000 {
		t.Fatalf("credits cost = %d", cost)
	}
	if cost, err := r.completedVideoBillingCost(&videogen.Result{
		CostType:   "credits",
		CostDetail: videogen.CostDetail{Price: 0},
	}); err == nil || cost != 0 {
		t.Fatalf("credits without price should fail, cost=%d err=%v", cost, err)
	}
	if cost, err := r.completedVideoBillingCost(&videogen.Result{
		CostType:   "free_quota",
		CostDetail: videogen.CostDetail{Price: 0},
	}); err != nil || cost != 0 {
		t.Fatalf("free quota should not bill, cost=%d err=%v", cost, err)
	}
}

type staticVideoRatio struct{ ratio float64 }

func (s staticVideoRatio) VideoGenBillingRatio() float64 { return s.ratio }

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

func TestNormalizeLanguageSupportsAdditionalLocales(t *testing.T) {
	cases := map[string]string{
		"ja":      "ja-JP",
		"日本语":     "ja-JP",
		"韩语":      "ko-KR",
		"spanish": "es-ES",
		"泰语":      "th-TH",
	}
	for in, want := range cases {
		if got := normalizeLanguageCode(in); got != want {
			t.Fatalf("normalizeLanguageCode(%q)=%q want %q", in, got, want)
		}
	}
}

func TestAdditionalLanguageRulesAndLabels(t *testing.T) {
	cases := []struct {
		code string
		name string
		rule string
		key  string
	}{
		{"ja-JP", "日本语", "Japanese", "Title:"},
		{"ko-KR", "韩语", "Korean", "Title:"},
		{"es-ES", "Español", "Spanish", "Title:"},
		{"th-TH", "ไทย", "Thai", "Title:"},
	}
	plan := ImageTextPlan{Title: "Product", Subtitle: "Sub", PriceText: "199", CTA: "Buy"}
	for _, tc := range cases {
		if got := platformLanguageName(tc.code); got != tc.name {
			t.Fatalf("name(%s)=%q want %q", tc.code, got, tc.name)
		}
		if !strings.Contains(platformLanguageRule(tc.code), tc.rule) {
			t.Fatalf("rule(%s) missing %q: %s", tc.code, tc.rule, platformLanguageRule(tc.code))
		}
		if !strings.Contains(formatImageTextPlanForLanguage(plan, tc.code), tc.key) {
			t.Fatalf("image text labels for %s should use latin prompt keys", tc.code)
		}
	}
}
