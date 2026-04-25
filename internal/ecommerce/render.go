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
	Requirement string
	Platform    Platform
	Prompt      PromptTemplate
	Style       StyleTemplate
	Output      Output
	AssetType   string
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
		ShopTitle:     title + "官方优选",
		ProductTitle:  title,
		Description:   "围绕核心卖点、真实使用场景与购买理由生成的电商详情内容。",
		PriceCopy:     "限时优惠，立即入手",
		MarketingCopy: []string{"清晰展示核心卖点", "适合多平台商品详情页", "突出购买理由与信任感"},
		DetailSections: []DetailSection{
			{Title: "核心卖点", Body: "提炼商品优势，让用户快速理解值得购买的原因。"},
			{Title: "使用场景", Body: "结合日常场景展示商品价值，降低决策成本。"},
			{Title: "购买理由", Body: "用简洁文案强化品质、体验与服务承诺。"},
		},
		PlatformFields: map[string]string{"title": title, "description": requirement},
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
