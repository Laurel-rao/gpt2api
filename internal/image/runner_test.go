package image

import (
	"strings"
	"testing"
)

func TestAppendImageSizeInstruction(t *testing.T) {
	got := appendImageSizeInstruction("生成商品图", "1024*1536")
	if !strings.Contains(got, "1024x1792") || !strings.Contains(got, "4:7") {
		t.Fatalf("size instruction missing: %s", got)
	}
}

func TestAppendImageSizeInstructionIgnoresUnsupportedSize(t *testing.T) {
	got := appendImageSizeInstruction("生成商品图", "800x800")
	if got != "生成商品图" {
		t.Fatalf("unexpected prompt: %s", got)
	}
}
