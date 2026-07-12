package videoworkflow

import (
	"image"
	"image/color"
	"testing"
)

func TestApplyImageTransform_CropFlipRotate(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	source.Set(0, 0, color.NRGBA{R: 255, A: 255})
	source.Set(1, 0, color.NRGBA{G: 255, A: 255})
	source.Set(0, 1, color.NRGBA{B: 255, A: 255})
	source.Set(1, 1, color.NRGBA{R: 255, G: 255, A: 255})
	result := applyImageTransform(source, ImageTransform{
		Crop: NormalizedCrop{Width: 0.5, Height: 1}, FlipHorizontal: true, Rotation: 90,
	})
	if result.Bounds().Dx() != 2 || result.Bounds().Dy() != 2 {
		t.Fatalf("dimensions=%v", result.Bounds())
	}
	// 裁剪左半部分后水平翻转，再顺时针 90°：左上应来自原图(1,1)黄色。
	got := color.NRGBAModel.Convert(result.At(0, 0)).(color.NRGBA)
	if got.R != 255 || got.G != 255 || got.B != 0 {
		t.Fatalf("unexpected top-left pixel: %#v", got)
	}
}

func TestValidateImageTransform(t *testing.T) {
	valid := []ImageTransform{{}, {Crop: NormalizedCrop{X: .1, Y: .2, Width: .8, Height: .7}, Rotation: 270, FlipVertical: true}}
	for _, transform := range valid {
		if err := validateImageTransform(transform); err != nil {
			t.Fatalf("valid transform rejected: %+v: %v", transform, err)
		}
	}
	invalid := []ImageTransform{
		{Rotation: 45},
		{Crop: NormalizedCrop{X: .5, Width: .6, Height: 1}},
		{Crop: NormalizedCrop{Width: .5}},
	}
	for _, transform := range invalid {
		if err := validateImageTransform(transform); err == nil {
			t.Fatalf("invalid transform accepted: %+v", transform)
		}
	}
}
