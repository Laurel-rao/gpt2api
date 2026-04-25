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
	Requirement   string
	Platform      Platform
	Prompt        PromptTemplate
	Style         StyleTemplate
	Output        Output
	AssetType     string
	UnifiedInfo   string
	ImageTextPlan string
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
	for k, spec := range defaultImageSpecs() {
		out.ImageSpecs[k] = normalizeImageSpec(out.ImageSpecs[k], spec)
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
		AssetTitle:  {Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"},
		AssetMain:   {Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"},
		AssetWhite:  {Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"},
		AssetDetail: {Size: "1024x1536", AspectRatio: "2:3", Clarity: "high"},
		AssetPrice:  {Size: "1024x1024", AspectRatio: "1:1", Clarity: "high"},
	}
}

func normalizeImageSpec(spec, fallback ImageSpec) ImageSpec {
	if spec.Size == "" {
		spec.Size = fallback.Size
	}
	if spec.AspectRatio == "" {
		spec.AspectRatio = fallback.AspectRatio
	}
	if spec.Clarity == "" {
		spec.Clarity = fallback.Clarity
	}
	return spec
}

func formatUnifiedInfo(out Output) string {
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

func formatImageTextPlan(plan ImageTextPlan) string {
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
	if u := assetMap[AssetWhite]; u != "" {
		b.WriteString(`<img class="wide-image" src="` + html.EscapeString(u) + `" alt="白底图">`)
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
