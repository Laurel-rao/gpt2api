package ecommerce

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	imgpkg "github.com/432539/gpt2api/internal/image"
)

func TestTaskViewFromRowListModeUsesLocalDataOnly(t *testing.T) {
	row := &TaskRow{
		Task: Task{
			ID:         1,
			TaskID:     "ecm_list",
			UserID:     2,
			Language:   "zh-CN",
			Status:     StatusRunning,
			Progress:   80,
			OutputJSON: RawJSON(`{"product_title":"新标题","description":"新描述","price_copy":"新价格"}`),
			OutputHTML: "<article>stored</article>",
			CreatedAt:  time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
		},
		PlatformName: "通用电商",
		PromptName:   "转化优先",
		StyleName:    "清爽白底",
	}
	assets := []Asset{
		{
			ID:          7,
			TaskID:      row.TaskID,
			AssetType:   AssetMain,
			ImageTaskID: "img_task",
			FileID:      "file_1",
			Status:      StatusSuccess,
			Progress:    100,
		},
		{
			ID:          8,
			TaskID:      row.TaskID,
			AssetType:   AssetVideo,
			ImageTaskID: "cgt-upstream-task",
			Status:      StatusRunning,
			Progress:    50,
		},
	}

	got, err := (&Handler{}).taskViewFromRow(context.Background(), row, assets, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got["output_html"] != row.OutputHTML {
		t.Fatalf("list mode should keep stored output_html, got %q", got["output_html"])
	}
	if got["status"] != StatusRunning || got["progress"] != 80 {
		t.Fatalf("list mode should not auto-complete task, got status=%v progress=%v", got["status"], got["progress"])
	}
	viewAssets, ok := got["assets"].([]Asset)
	if !ok {
		t.Fatalf("assets type = %T", got["assets"])
	}
	if len(viewAssets) != 2 {
		t.Fatalf("assets len = %d", len(viewAssets))
	}
	if viewAssets[0].URL != imgpkg.BuildProxyURL("img_task", 0, 0) {
		t.Fatalf("main asset proxy url = %q", viewAssets[0].URL)
	}
	if viewAssets[1].Status != StatusRunning {
		t.Fatalf("video asset should stay unsynced in list mode: %+v", viewAssets[1])
	}
	if assets[0].URL != "" {
		raw, _ := json.Marshal(assets[0])
		t.Fatalf("input assets should not be mutated: %s", raw)
	}
}
