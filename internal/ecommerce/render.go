package ecommerce

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"text/template"
)

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

type renderData struct {
	Requirement          string
	CreativeRequirement  string
	Platform             Platform
	Prompt               PromptTemplate
	Style                StyleTemplate
	Output               Output
	AssetType            string
	UnifiedInfo          string
	ImageTextPlan        string
	CompactUnifiedInfo   string
	CompactImageTextPlan string
	VisualDirection      string
	CompactLanguageRule  string
	RetryExtra           string
	LanguageCode         string
	LanguageName         string
	LanguageRule         string
}

func renderTemplate(src string, data renderData) string {
	tpl, err := template.New("prompt").Option("missingkey=zero").Parse(src)
	if err != nil {
		return src
	}
	var b bytes.Buffer
	if err := tpl.Execute(&b, data); err != nil {
		return src
	}
	return b.String()
}

func newRenderData(requirement string, platform Platform, prompt PromptTemplate, style StyleTemplate, out Output) renderData {
	code := platformLanguageCode(platform)
	return renderData{
		Requirement:         requirement,
		CreativeRequirement: extractCreativeRequirement(requirement),
		Platform:            platform,
		Prompt:              prompt,
		Style:               style,
		Output:              out,
		VisualDirection:     combineVisualDirection(prompt.ImagePrompt, style.StylePrompt),
		LanguageCode:        code,
		LanguageName:        platformLanguageName(code),
		LanguageRule:        platformLanguageRule(code),
		CompactLanguageRule: compactLanguageRule(code),
	}
}

func platformLanguageCode(platform Platform) string {
	if s := strings.TrimSpace(platform.Language); s != "" {
		return normalizeLanguageCode(s)
	}
	var cfg struct {
		Locale string `json:"locale"`
	}
	_ = json.Unmarshal(platform.FieldSchema.RawMessage(), &cfg)
	return normalizeLanguageCode(cfg.Locale)
}

func normalizeLanguageCode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "en", "en-us", "english":
		return "en-US"
	case "zh", "zh-cn", "cn", "chinese", "中文":
		return "zh-CN"
	default:
		if strings.TrimSpace(s) == "" {
			return "zh-CN"
		}
		return strings.TrimSpace(s)
	}
}

func platformLanguageName(code string) string {
	if strings.HasPrefix(strings.ToLower(code), "en") {
		return "English"
	}
	return "简体中文"
}

func platformLanguageRule(code string) string {
	if strings.HasPrefix(strings.ToLower(code), "en") {
		return "All user-facing copy, platform fields, detail page text and image text plans must be written in English. Preserve user-provided brand names, model numbers, dimensions, prices and SKUs exactly; do not output Chinese unless it is a brand/model/spec explicitly provided by the user."
	}
	return "所有面向用户的文案、平台字段、详情页文字和图片文字计划必须使用简体中文。用户提供的品牌名、型号、尺寸、价格和 SKU 必须逐字保留。"
}

func compactLanguageRule(code string) string {
	if strings.HasPrefix(strings.ToLower(code), "en") {
		return "Visible text must stay in English and keep provided brand/model/price/spec wording exactly."
	}
	return "可见文字必须使用简体中文，并逐字保留用户给出的品牌、型号、价格和规格。"
}

func combineVisualDirection(parts ...string) string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ReplaceAll(part, "\n", " "))
		if part == "" {
			continue
		}
		items = append(items, part)
	}
	return strings.Join(items, " ")
}

func extractCreativeRequirement(requirement string) string {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return ""
	}
	keywords := []string{
		"代言", "代言人", "人物", "模特", "雷军", "苹果风", "简约风", "科技风",
		"场景", "首页", "店铺", "品牌氛围", "活动主题", "海报", "发布会", "氛围",
		"endorsement", "spokesperson", "model", "lifestyle", "scene", "homepage",
		"brand mood", "campaign", "launch", "minimal", "editorial", "deconstructed",
	}
	lower := strings.ToLower(requirement)
	for _, keyword := range keywords {
		if strings.Contains(requirement, keyword) || strings.Contains(lower, strings.ToLower(keyword)) {
			return requirement
		}
	}
	return ""
}

func parseOutput(raw, requirement string) Output {
	clean := strings.TrimSpace(raw)
	if m := jsonFenceRe.FindStringSubmatch(clean); len(m) == 2 {
		clean = strings.TrimSpace(m[1])
	}
	var out Output
	if err := json.Unmarshal([]byte(clean), &out); err == nil && out.ProductTitle != "" {
		normalizeOutput(&out, requirement)
		return out
	}
	out = fallbackOutput(requirement)
	normalizeOutput(&out, requirement)
	return out
}

func fallbackOutput(requirement string) Output {
	title := firstLine(requirement)
	if title == "" {
		title = "精选商品"
	}
	return Output{
		ShopTitle:    title + "官方优选",
		ProductTitle: title,
		Description:  "围绕核心卖点、真实使用场景与购买理由生成的电商详情内容。",
		PriceCopy:    "限时优惠，立即入手",
		ProductInfo: ProductInfo{
			CanonicalTitle: title,
			ShortTitle:     title,
			CoreValue:      "清晰呈现商品价值",
		},
		PriceInfo: PriceInfo{
			Currency:      "CNY",
			PromotionText: "限时优惠",
			CTA:           "立即入手",
		},
		MarketingCopy: []string{"清晰展示核心卖点", "适合多平台商品详情页", "突出购买理由与信任感"},
		DetailSections: []DetailSection{
			{Title: "核心卖点", Body: "提炼商品优势，让用户快速理解值得购买的原因。"},
			{Title: "使用场景", Body: "结合日常场景展示商品价值，降低决策成本。"},
			{Title: "购买理由", Body: "用简洁文案强化品质、体验与服务承诺。"},
		},
		PlatformFields: map[string]string{"title": title, "description": requirement},
		ImageSpecs:     defaultImageSpecs(),
	}
}

func normalizeOutput(out *Output, requirement string) {
	if out.ProductTitle == "" {
		out.ProductTitle = firstLine(requirement)
	}
	if out.ShopTitle == "" {
		out.ShopTitle = out.ProductTitle + "官方优选"
	}
	if out.Description == "" {
		out.Description = requirement
	}
	if out.PriceCopy == "" {
		out.PriceCopy = "限时优惠，立即入手"
	}
	normalizeProductInfo(out)
	normalizePriceInfo(out)
	if len(out.MarketingCopy) == 0 {
		out.MarketingCopy = []string{"高转化商品卖点", "清晰详情页结构", "适配主流电商平台"}
	}
	if len(out.DetailSections) == 0 {
		out.DetailSections = fallbackOutput(requirement).DetailSections
	}
	if out.PlatformFields == nil {
		out.PlatformFields = map[string]string{}
	}
	out.PlatformFields["title"] = out.ProductTitle
	out.PlatformFields["description"] = out.Description
	out.PlatformFields["price_text"] = out.PriceInfo.PriceText
	out.PlatformFields["promotion_text"] = out.PriceInfo.PromotionText
	if out.ImageSpecs == nil {
		out.ImageSpecs = map[string]ImageSpec{}
	}
	defaults := defaultImageSpecs()
	for k, spec := range defaults {
		out.ImageSpecs[k] = normalizeImageSpec(out.ImageSpecs[k], spec)
	}
	if allImageSpecsSame(out.ImageSpecs) {
		for k, spec := range defaults {
			out.ImageSpecs[k] = spec
		}
	}
	normalizeImageTextPlans(out)
}

func normalizeProductInfo(out *Output) {
	if out.ProductInfo.CanonicalTitle == "" {
		out.ProductInfo.CanonicalTitle = out.ProductTitle
	}
	if out.ProductInfo.ShortTitle == "" {
		out.ProductInfo.ShortTitle = out.ProductTitle
	}
	if out.ProductInfo.CoreValue == "" {
		out.ProductInfo.CoreValue = out.Description
	}
}

func normalizePriceInfo(out *Output) {
	if out.PriceInfo.Currency == "" {
		out.PriceInfo.Currency = "CNY"
	}
	if out.PriceInfo.PriceText == "" {
		out.PriceInfo.PriceText = out.PriceCopy
	}
	if out.PriceInfo.PromotionText == "" && out.PriceInfo.PriceText != out.PriceCopy {
		out.PriceInfo.PromotionText = out.PriceCopy
	}
	if out.PriceInfo.CTA == "" {
		out.PriceInfo.CTA = "立即购买"
	}
}

func normalizeImageTextPlans(out *Output) {
	if out.ImageTextPlans == nil {
		out.ImageTextPlans = map[string]ImageTextPlan{}
	}
	defaultPlan := ImageTextPlan{
		Title:         out.ProductInfo.ShortTitle,
		Subtitle:      out.ProductInfo.CoreValue,
		PriceText:     out.PriceInfo.PriceText,
		PromotionText: out.PriceInfo.PromotionText,
		CTA:           out.PriceInfo.CTA,
		SellingPoints: append([]string(nil), out.ProductInfo.SellingPoints...),
		Specs:         append([]string(nil), out.ProductInfo.KeySpecs...),
	}
	if len(defaultPlan.SellingPoints) == 0 {
		defaultPlan.SellingPoints = append([]string(nil), out.MarketingCopy...)
	}
	for _, assetType := range assetTypes {
		plan := out.ImageTextPlans[assetType]
		plan.Title = defaultPlan.Title
		if plan.Subtitle == "" {
			plan.Subtitle = defaultPlan.Subtitle
		}
		plan.PriceText = defaultPlan.PriceText
		plan.PromotionText = defaultPlan.PromotionText
		plan.CTA = defaultPlan.CTA
		if len(defaultPlan.SellingPoints) > 0 {
			plan.SellingPoints = append([]string(nil), defaultPlan.SellingPoints...)
		}
		if len(defaultPlan.Specs) > 0 {
			plan.Specs = append([]string(nil), defaultPlan.Specs...)
		}
		if assetType == AssetWhite {
			plan.PriceText = ""
			plan.PromotionText = ""
			plan.CTA = ""
			plan.Badges = nil
			plan.SellingPoints = nil
			plan.Specs = nil
			plan.Notes = []string{"白底图不放任何文字、价格、促销标签或图标"}
		}
		out.ImageTextPlans[assetType] = plan
	}
}

func defaultImageSpecs() map[string]ImageSpec {
	return map[string]ImageSpec{
		AssetTitle:  {Size: "1792x1024", AspectRatio: "7:4", Clarity: "high"},
		AssetMain:   {Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"},
		AssetWhite:  {Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"},
		AssetDetail: {Size: "1024x1792", AspectRatio: "4:7", Clarity: "high"},
		AssetPrice:  {Size: "1024x1792", AspectRatio: "4:7", Clarity: "high"},
	}
}

func normalizeImageSpec(spec, fallback ImageSpec) ImageSpec {
	if spec.Size == "" {
		spec.Size = fallback.Size
	}
	spec.Size = normalizeImageSize(spec.Size, fallback.Size)
	if spec.AspectRatio == "" {
		spec.AspectRatio = aspectRatioForSize(spec.Size)
	}
	if spec.AspectRatio == "" {
		spec.AspectRatio = fallback.AspectRatio
	}
	if spec.Clarity == "" {
		spec.Clarity = fallback.Clarity
	}
	return spec
}

func normalizeImageSize(size, fallback string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	size = strings.ReplaceAll(size, "*", "x")
	size = strings.ReplaceAll(size, "×", "x")
	switch size {
	case "1024x1024", "1792x1024", "1024x1792":
		return size
	case "1024x1536", "1024x1365", "1024x1280":
		return "1024x1792"
	case "1536x1024", "1365x1024", "1280x1024":
		return "1792x1024"
	default:
		return fallback
	}
}

func aspectRatioForSize(size string) string {
	switch size {
	case "1024x1024":
		return "1:1"
	case "1792x1024":
		return "7:4"
	case "1024x1792":
		return "4:7"
	default:
		return ""
	}
}

func allImageSpecsSame(specs map[string]ImageSpec) bool {
	first := ""
	for _, assetType := range assetTypes {
		size := specs[assetType].Size
		if size == "" {
			continue
		}
		if first == "" {
			first = size
			continue
		}
		if size != first {
			return false
		}
	}
	return first != ""
}

func formatUnifiedInfo(out Output) string {
	return formatUnifiedInfoForLanguage(out, "zh-CN")
}

func formatUnifiedInfoForLanguage(out Output, languageCode string) string {
	if strings.HasPrefix(strings.ToLower(languageCode), "en") {
		lines := []string{
			"Canonical product title: " + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
			"Short title: " + firstNonEmpty(out.ProductInfo.ShortTitle, out.ProductTitle),
			"Category: " + out.ProductInfo.Category,
			"Core value: " + out.ProductInfo.CoreValue,
			"Price text: " + out.PriceInfo.PriceText,
			"Original price: " + out.PriceInfo.OriginalPrice,
			"Promotion text: " + out.PriceInfo.PromotionText,
			"Call to action: " + out.PriceInfo.CTA,
			"Key specs: " + strings.Join(out.ProductInfo.KeySpecs, "; "),
			"Selling points: " + strings.Join(out.ProductInfo.SellingPoints, "; "),
		}
		return strings.Join(nonEmptyLines(lines), "\n")
	}
	lines := []string{
		"统一商品标题：" + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
		"统一短标题：" + firstNonEmpty(out.ProductInfo.ShortTitle, out.ProductTitle),
		"统一商品品类：" + out.ProductInfo.Category,
		"统一核心价值：" + out.ProductInfo.CoreValue,
		"统一价格文字：" + out.PriceInfo.PriceText,
		"统一原价文字：" + out.PriceInfo.OriginalPrice,
		"统一促销文字：" + out.PriceInfo.PromotionText,
		"统一行动号召：" + out.PriceInfo.CTA,
		"统一关键规格：" + strings.Join(out.ProductInfo.KeySpecs, "；"),
		"统一卖点：" + strings.Join(out.ProductInfo.SellingPoints, "；"),
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func limitStrings(items []string, n int) []string {
	if len(items) == 0 {
		return nil
	}
	if n <= 0 || len(items) <= n {
		return append([]string(nil), items...)
	}
	return append([]string(nil), items[:n]...)
}

func formatCompactUnifiedInfoForLanguage(out Output, languageCode string) string {
	if strings.HasPrefix(strings.ToLower(languageCode), "en") {
		lines := []string{
			"Title: " + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
			"Core value: " + out.ProductInfo.CoreValue,
			"Price text: " + out.PriceInfo.PriceText,
			"Promotion text: " + out.PriceInfo.PromotionText,
			"CTA: " + out.PriceInfo.CTA,
			"Key specs: " + strings.Join(limitStrings(out.ProductInfo.KeySpecs, 4), "; "),
			"Selling points: " + strings.Join(limitStrings(out.ProductInfo.SellingPoints, 3), "; "),
		}
		return strings.Join(nonEmptyLines(lines), "\n")
	}
	lines := []string{
		"标题：" + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
		"核心价值：" + out.ProductInfo.CoreValue,
		"价格文字：" + out.PriceInfo.PriceText,
		"促销文字：" + out.PriceInfo.PromotionText,
		"行动号召：" + out.PriceInfo.CTA,
		"关键规格：" + strings.Join(limitStrings(out.ProductInfo.KeySpecs, 4), "；"),
		"卖点：" + strings.Join(limitStrings(out.ProductInfo.SellingPoints, 3), "；"),
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func formatCompactUnifiedInfoForAssetLanguage(out Output, assetType, languageCode string) string {
	if strings.HasPrefix(strings.ToLower(languageCode), "en") {
		lines := []string{
			"Title: " + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
			"Category: " + out.ProductInfo.Category,
			"Core value: " + out.ProductInfo.CoreValue,
		}
		switch assetType {
		case AssetPrice:
			lines = append(lines,
				"Price text: "+out.PriceInfo.PriceText,
				"Promotion text: "+out.PriceInfo.PromotionText,
				"CTA: "+out.PriceInfo.CTA,
			)
		case AssetDetail:
			lines = append(lines,
				"Key specs: "+strings.Join(limitStrings(out.ProductInfo.KeySpecs, 3), "; "),
				"Selling points: "+strings.Join(limitStrings(out.ProductInfo.SellingPoints, 3), "; "),
			)
		case AssetMain, AssetTitle:
			lines = append(lines,
				"Key specs: "+strings.Join(limitStrings(out.ProductInfo.KeySpecs, 2), "; "),
			)
		}
		return strings.Join(nonEmptyLines(lines), "\n")
	}
	lines := []string{
		"标题：" + firstNonEmpty(out.ProductInfo.CanonicalTitle, out.ProductTitle),
		"品类：" + out.ProductInfo.Category,
		"核心价值：" + out.ProductInfo.CoreValue,
	}
	switch assetType {
	case AssetPrice:
		lines = append(lines,
			"价格文字："+out.PriceInfo.PriceText,
			"促销文字："+out.PriceInfo.PromotionText,
			"行动号召："+out.PriceInfo.CTA,
		)
	case AssetDetail:
		lines = append(lines,
			"关键规格："+strings.Join(limitStrings(out.ProductInfo.KeySpecs, 3), "；"),
			"卖点："+strings.Join(limitStrings(out.ProductInfo.SellingPoints, 3), "；"),
		)
	case AssetMain, AssetTitle:
		lines = append(lines,
			"关键规格："+strings.Join(limitStrings(out.ProductInfo.KeySpecs, 2), "；"),
		)
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func formatImageTextPlan(plan ImageTextPlan) string {
	return formatImageTextPlanForLanguage(plan, "zh-CN")
}

func formatImageTextPlanForLanguage(plan ImageTextPlan, languageCode string) string {
	if strings.HasPrefix(strings.ToLower(languageCode), "en") {
		lines := []string{
			"Title: " + plan.Title,
			"Subtitle: " + plan.Subtitle,
			"Price: " + plan.PriceText,
			"Promotion: " + plan.PromotionText,
			"Call to action: " + plan.CTA,
			"Badges: " + strings.Join(plan.Badges, "; "),
			"Selling points: " + strings.Join(plan.SellingPoints, "; "),
			"Specs: " + strings.Join(plan.Specs, "; "),
			"Notes: " + strings.Join(plan.Notes, "; "),
		}
		return strings.Join(nonEmptyLines(lines), "\n")
	}
	lines := []string{
		"标题：" + plan.Title,
		"副标题：" + plan.Subtitle,
		"价格：" + plan.PriceText,
		"促销：" + plan.PromotionText,
		"行动号召：" + plan.CTA,
		"标签：" + strings.Join(plan.Badges, "；"),
		"卖点：" + strings.Join(plan.SellingPoints, "；"),
		"规格：" + strings.Join(plan.Specs, "；"),
		"备注：" + strings.Join(plan.Notes, "；"),
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func formatCompactImageTextPlanForLanguage(plan ImageTextPlan, assetType, languageCode string) string {
	var lines []string
	switch {
	case strings.HasPrefix(strings.ToLower(languageCode), "en"):
		switch assetType {
		case AssetTitle, AssetMain:
			lines = []string{
				"Title: " + plan.Title,
				"Subtitle: " + plan.Subtitle,
				"Badges: " + strings.Join(limitStrings(plan.Badges, 2), "; "),
				"CTA: " + plan.CTA,
			}
		case AssetPrice:
			lines = []string{
				"Title: " + plan.Title,
				"Price: " + plan.PriceText,
				"Promotion: " + plan.PromotionText,
				"CTA: " + plan.CTA,
				"Badges: " + strings.Join(limitStrings(plan.Badges, 2), "; "),
			}
		default:
			lines = []string{
				"Title: " + plan.Title,
				"Subtitle: " + plan.Subtitle,
				"Selling points: " + strings.Join(limitStrings(plan.SellingPoints, 3), "; "),
				"Specs: " + strings.Join(limitStrings(plan.Specs, 3), "; "),
			}
		}
	default:
		switch assetType {
		case AssetTitle, AssetMain:
			lines = []string{
				"标题：" + plan.Title,
				"副标题：" + plan.Subtitle,
				"标签：" + strings.Join(limitStrings(plan.Badges, 2), "；"),
				"行动号召：" + plan.CTA,
			}
		case AssetPrice:
			lines = []string{
				"标题：" + plan.Title,
				"价格：" + plan.PriceText,
				"促销：" + plan.PromotionText,
				"行动号召：" + plan.CTA,
				"标签：" + strings.Join(limitStrings(plan.Badges, 2), "；"),
			}
		default:
			lines = []string{
				"标题：" + plan.Title,
				"副标题：" + plan.Subtitle,
				"卖点：" + strings.Join(limitStrings(plan.SellingPoints, 3), "；"),
				"规格：" + strings.Join(limitStrings(plan.Specs, 3), "；"),
			}
		}
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func nonEmptyLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.LastIndex(line, "："); idx >= 0 && strings.TrimSpace(line[idx+len("："):]) == "" {
			continue
		}
		if idx := strings.LastIndex(line, ":"); idx >= 0 && strings.TrimSpace(line[idx+1:]) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func buildHTML(out Output, assets []Asset) string {
	assetMap := map[string]string{}
	for _, a := range assets {
		if a.URL != "" {
			assetMap[a.AssetType] = a.URL
		}
	}
	var b strings.Builder
	b.WriteString(`<article class="ecommerce-detail-preview">`)
	if u := assetMap[AssetMain]; u != "" {
		b.WriteString(`<img class="hero-image" src="` + html.EscapeString(u) + `" alt="商品主图">`)
	}
	b.WriteString(`<section class="detail-head"><h1>`)
	b.WriteString(html.EscapeString(out.ProductTitle))
	b.WriteString(`</h1><p>`)
	b.WriteString(html.EscapeString(out.Description))
	b.WriteString(`</p><strong>`)
	b.WriteString(html.EscapeString(out.PriceCopy))
	b.WriteString(`</strong></section>`)
	if u := assetMap[AssetPrice]; u != "" {
		b.WriteString(`<img class="wide-image" src="` + html.EscapeString(u) + `" alt="价格图">`)
	}
	b.WriteString(`<section class="copy-grid">`)
	for _, s := range out.MarketingCopy {
		b.WriteString(`<span>`)
		b.WriteString(html.EscapeString(s))
		b.WriteString(`</span>`)
	}
	b.WriteString(`</section>`)
	if u := assetMap[AssetDetail]; u != "" {
		b.WriteString(`<img class="wide-image" src="` + html.EscapeString(u) + `" alt="详情图">`)
	}
	for _, sec := range out.DetailSections {
		b.WriteString(`<section class="detail-section"><h2>`)
		b.WriteString(html.EscapeString(sec.Title))
		b.WriteString(`</h2><p>`)
		b.WriteString(html.EscapeString(sec.Body))
		b.WriteString(`</p></section>`)
	}
	b.WriteString(`</article>`)
	return b.String()
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len([]rune(line)) > 36 {
				return string([]rune(line)[:36])
			}
			return line
		}
	}
	return ""
}
