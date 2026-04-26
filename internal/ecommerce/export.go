package ecommerce

import (
	"bytes"
	"context"
	"fmt"
	stdimage "image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/432539/gpt2api/internal/middleware"
	"github.com/432539/gpt2api/pkg/resp"
)

const (
	exportPosterWidth       = 1242
	exportPosterPadding     = 72
	exportPosterGap         = 34
	exportPosterMaxImageH   = 1600
	exportPosterMaxImageIn  = 32 * 1024 * 1024
	exportPosterHTTPTimeout = 90 * time.Second
)

// ExportPoster GET /api/me/ecommerce/tasks/:id/export
func (h *Handler) ExportPoster(c *gin.Context) {
	uid := middleware.UserID(c)
	if uid == 0 {
		resp.Unauthorized(c, "not logged in")
		return
	}
	row, err := h.dao.GetTask(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeErr(c, err)
		return
	}
	if row.UserID != uid {
		resp.NotFound(c, "任务不存在")
		return
	}
	assets, err := h.dao.ListAssets(c.Request.Context(), row.TaskID)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	refreshAssetProxyURLs(assets)
	data, err := buildPosterPNG(c.Request.Context(), c.Request, assets)
	if err != nil {
		resp.Internal(c, err.Error())
		return
	}
	name := fmt.Sprintf("%s-ecommerce-poster.png", row.TaskID)
	c.Header("Content-Type", "image/png")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", data)
}

func buildPosterPNG(ctx context.Context, req *http.Request, assets []Asset) ([]byte, error) {
	type loadedImage struct {
		typ string
		img stdimage.Image
	}
	loaded := make([]loadedImage, 0, len(assetTypes))
	for _, typ := range assetTypes {
		for _, asset := range assets {
			if asset.AssetType != typ || asset.Status != StatusSuccess || strings.TrimSpace(asset.URL) == "" {
				continue
			}
			img, err := fetchPosterImage(ctx, absoluteAssetURL(req, asset.URL))
			if err == nil {
				loaded = append(loaded, loadedImage{typ: typ, img: img})
			}
			break
		}
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("没有可导出的图片")
	}

	contentW := exportPosterWidth - exportPosterPadding*2
	height := exportPosterPadding
	sizes := make([]stdimage.Rectangle, 0, len(loaded))
	for _, item := range loaded {
		b := item.img.Bounds()
		w := max(1, b.Dx())
		h := max(1, b.Dy())
		drawH := h * contentW / w
		if drawH > exportPosterMaxImageH {
			drawH = exportPosterMaxImageH
		}
		rect := stdimage.Rect(0, 0, contentW, drawH)
		sizes = append(sizes, rect)
		height += drawH + exportPosterGap
	}
	height += exportPosterPadding

	canvas := stdimage.NewRGBA(stdimage.Rect(0, 0, exportPosterWidth, height))
	xdraw.Draw(canvas, canvas.Bounds(), &stdimage.Uniform{C: color.RGBA{R: 245, G: 245, B: 245, A: 255}}, stdimage.Point{}, xdraw.Src)
	y := exportPosterPadding
	for i, item := range loaded {
		dst := sizes[i].Add(stdimage.Pt(exportPosterPadding, y))
		xdraw.CatmullRom.Scale(canvas, dst, item.img, item.img.Bounds(), xdraw.Over, nil)
		y += sizes[i].Dy() + exportPosterGap
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, fmt.Errorf("导出图片编码失败: %w", err)
	}
	return buf.Bytes(), nil
}

func fetchPosterImage(ctx context.Context, rawURL string) (stdimage.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, exportPosterHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("图片下载失败 HTTP %d", res.StatusCode)
	}
	img, _, err := stdimage.Decode(io.LimitReader(res.Body, exportPosterMaxImageIn))
	if err != nil {
		return nil, fmt.Errorf("图片解码失败: %w", err)
	}
	return img, nil
}

func absoluteAssetURL(req *http.Request, raw string) string {
	u, err := url.Parse(raw)
	if err == nil {
		q := u.Query()
		q.Del("thumb_kb")
		u.RawQuery = q.Encode()
		if u.IsAbs() {
			return u.String()
		}
		raw = u.String()
	}
	scheme := req.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if req.TLS != nil {
			scheme = "https"
		}
	}
	host := req.Host
	if forwarded := req.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return scheme + "://" + host + raw
}
