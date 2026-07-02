package ecommerce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func (h *Handler) resolveTaskLibraryAssets(ctx context.Context, uid uint64, productAssetID, modelAssetID string) (*LibraryAsset, *LibraryAsset, error) {
	var productAsset *LibraryAsset
	var modelAsset *LibraryAsset
	if strings.TrimSpace(productAssetID) != "" {
		a, err := h.dao.GetVisibleLibraryAsset(ctx, strings.TrimSpace(productAssetID), uid)
		if err != nil {
			return nil, nil, fmt.Errorf("商品资产不存在或不可用")
		}
		if a.Kind != LibraryKindProduct {
			return nil, nil, fmt.Errorf("请选择商品资产")
		}
		productAsset = a
	}
	if strings.TrimSpace(modelAssetID) != "" {
		a, err := h.dao.GetVisibleLibraryAsset(ctx, strings.TrimSpace(modelAssetID), uid)
		if err != nil {
			return nil, nil, fmt.Errorf("模特资产不存在或不可用")
		}
		if a.Kind != LibraryKindModel {
			return nil, nil, fmt.Errorf("请选择模特资产")
		}
		modelAsset = a
	}
	return productAsset, modelAsset, nil
}

func (h *Handler) mergeTaskReferenceImages(manual []string, productAsset, modelAsset *LibraryAsset) []string {
	out := make([]string, 0, maxReferenceImages)
	seen := map[string]struct{}{}
	add := func(u string) {
		if len(out) >= maxReferenceImages {
			return
		}
		u = h.absoluteAssetURL(strings.TrimSpace(u))
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	for _, u := range libraryReferenceImages(productAsset) {
		add(u)
	}
	for _, u := range libraryReferenceImages(modelAsset) {
		add(u)
	}
	for _, u := range manual {
		add(u)
	}
	return out
}

func (h *Handler) absoluteAssetURL(raw string) string {
	if raw == "" || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if h.runner == nil || h.runner.appBaseURL == "" || !strings.HasPrefix(raw, "/") {
		return raw
	}
	base, err := url.Parse(h.runner.appBaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func buildRequirementWithLibraryAssets(requirement string, productAsset, modelAsset *LibraryAsset) string {
	blocks := []string{strings.TrimSpace(requirement)}
	if productAsset != nil {
		blocks = append(blocks, "【商品资产资料】\n"+libraryAssetPromptBlock(*productAsset))
	}
	if modelAsset != nil {
		blocks = append(blocks, "【模特资产资料】\n"+libraryAssetPromptBlock(*modelAsset))
	}
	return strings.Join(nonEmptyLines(blocks), "\n\n")
}

func libraryAssetPromptBlock(a LibraryAsset) string {
	lines := []string{
		"资产名称：" + a.Name,
		"资产编码：" + a.Code,
		"资产类型：" + a.Kind,
	}
	tags := compactJSONForPrompt(a.TagsJSON.RawMessage())
	if tags != "" {
		lines = append(lines, "标签："+tags)
	}
	detail := compactJSONForPrompt(a.DetailJSON.RawMessage())
	if detail != "" {
		lines = append(lines, "生产资料："+detail)
	}
	if a.CoverURL != "" {
		lines = append(lines, "主参考图："+a.CoverURL)
	}
	return strings.Join(nonEmptyLines(lines), "\n")
}

func compactJSONForPrompt(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncate(string(raw), 1200)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return truncate(string(raw), 1200)
	}
	return truncate(string(b), 2000)
}

func assetIDOrEmpty(a *LibraryAsset) string {
	if a == nil {
		return ""
	}
	return a.AssetID
}
