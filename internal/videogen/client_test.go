package videogen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProbeModelsUsesModelIDAsValue(t *testing.T) {
	client, _ := testClientWithModels(t, []Model{
		{ID: "0f1ff0a3-fb26-4163-9820-1779ad9f5f13", Name: "Seedance-2.0-D", Type: "video"},
		{ID: "0e37fa2d-72b3-483a-81b4-ad595cd147c7", Name: "Seedance-2.0-D-V", Type: "video"},
	})
	_, _, modelName, models, err := client.ProbeModels(context.Background())
	if err != nil {
		t.Fatalf("ProbeModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models len = %d", len(models))
	}
	if modelName != "Seedance-2.0-D-V" {
		t.Fatalf("preferred model name = %q", modelName)
	}
	if models[0].Value != "0e37fa2d-72b3-483a-81b4-ad595cd147c7" {
		t.Fatalf("model value should use id, got %q", models[0].Value)
	}
}

func TestResolveDefaultAliasPrefersSeedanceDVModelID(t *testing.T) {
	client, _ := testClientWithModels(t, []Model{
		{ID: "0f1ff0a3-fb26-4163-9820-1779ad9f5f13", Name: "Seedance-2.0-D", Type: "video"},
		{ID: "0e37fa2d-72b3-483a-81b4-ad595cd147c7", Name: "Seedance-2.0-D-V", Type: "video"},
	})
	got, err := client.resolveModelID(context.Background(), Config{
		BaseURL: client.baseURL,
		APIKey:  "test-key",
		Model:   "Seedance 2.0",
	}, Options{})
	if err != nil {
		t.Fatalf("resolveModelID error: %v", err)
	}
	if got != "0e37fa2d-72b3-483a-81b4-ad595cd147c7" {
		t.Fatalf("model id = %q", got)
	}
}

func TestRuntimeConfigDefaultsToSeedanceDVModelID(t *testing.T) {
	client := NewClient(Config{APIKey: "test-key"})
	if got := client.runtimeConfig().Model; got != "0e37fa2d-72b3-483a-81b4-ad595cd147c7" {
		t.Fatalf("default model = %q", got)
	}
}

func TestDefaultVideoGenTimeoutIsThirtyMinutesAndRequestTimeoutIsSixtySeconds(t *testing.T) {
	client := NewClient(Config{APIKey: "test-key"})
	if got := client.runtimeConfig().TimeoutSec; got != 1800 {
		t.Fatalf("default timeout sec = %d", got)
	}
	if client.httpClient == nil {
		t.Fatal("http client is nil")
	}
	if got := client.httpClient.Timeout; got != 60*time.Second {
		t.Fatalf("http client timeout = %s", got)
	}
}

func TestBalanceUsesAccountBalanceEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/account/balance" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept-Encoding") == "identity" {
			t.Fatalf("echoon balance should not force identity encoding")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Balance{
			Credits:         984816,
			RechargeBalance: 7,
			FreeQuotas: []FreeQuota{{
				ModelID:        "uuid",
				ModelName:      "Seedream 5.0",
				RemainingCount: 3,
			}},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	if got.Credits != 984816 || got.RechargeBalance != 7 {
		t.Fatalf("unexpected balance: %+v", got)
	}
	if len(got.FreeQuotas) != 1 || got.FreeQuotas[0].ModelName != "Seedream 5.0" || got.FreeQuotas[0].RemainingCount != 3 {
		t.Fatalf("unexpected free quotas: %+v", got.FreeQuotas)
	}
	if got.DurationMs < 0 {
		t.Fatalf("duration should be non-negative: %d", got.DurationMs)
	}
}

func TestAPIYIProbeModelsUsesBuiltInModels(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYISeedance, APIKey: "test-key"})
	_, count, modelName, models, err := client.ProbeModels(context.Background())
	if err != nil {
		t.Fatalf("ProbeModels error: %v", err)
	}
	if count != 2 || len(models) != 2 {
		t.Fatalf("model counts = %d/%d", count, len(models))
	}
	if modelName != apiyiDefaultFastModel {
		t.Fatalf("modelName = %q", modelName)
	}
	if models[0].Value != apiyiDefaultFastModel || models[1].Value != apiyiDefaultStandardModel {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestAPIYIBalanceUnsupported(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYISeedance, APIKey: "test-key"})
	got, err := client.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	if got.Supported || got.Message == "" {
		t.Fatalf("expected unsupported balance, got %+v", got)
	}
	if len(got.FreeQuotas) != 0 {
		t.Fatalf("free quotas should be empty: %+v", got.FreeQuotas)
	}
}

func TestAPIYIBalanceUnsupportedWithoutAPIKey(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYISeedance})
	got, err := client.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	if got.Supported || got.Message == "" {
		t.Fatalf("expected unsupported balance, got %+v", got)
	}
}

func TestAPIYIWanProbeModelsUsesBuiltInModels(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYIWan27, APIKey: "test-key"})
	_, count, modelName, models, err := client.ProbeModels(context.Background())
	if err != nil {
		t.Fatalf("ProbeModels error: %v", err)
	}
	if count != 3 || len(models) != 3 {
		t.Fatalf("model counts = %d/%d", count, len(models))
	}
	if modelName != apiyiWanDefaultModel {
		t.Fatalf("modelName = %q", modelName)
	}
	if models[0].Value != apiyiWanDefaultModel || models[1].Value != apiyiWanTextModel || models[2].Value != apiyiWanImageModel {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestAPIYIWanBalanceUnsupportedWithoutAPIKey(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYIWan27})
	got, err := client.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	if got.Supported || !strings.Contains(got.Message, "Wan2.7") {
		t.Fatalf("expected unsupported wan balance, got %+v", got)
	}
}

func TestAPIYIHappyHorseProbeModelsUsesBuiltInModels(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYIHappyHorse, APIKey: "test-key"})
	_, count, modelName, models, err := client.ProbeModels(context.Background())
	if err != nil {
		t.Fatalf("ProbeModels error: %v", err)
	}
	if count != 3 || len(models) != 3 {
		t.Fatalf("model counts = %d/%d", count, len(models))
	}
	if modelName != apiyiHappyHorseDefaultModel {
		t.Fatalf("modelName = %q", modelName)
	}
	if models[0].Value != apiyiHappyHorseDefaultModel || models[1].Value != apiyiHappyHorseTextModel || models[2].Value != apiyiHappyHorseImageModel {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestAPIYIHappyHorseBalanceUnsupportedWithoutAPIKey(t *testing.T) {
	client := NewClient(Config{ChannelType: ChannelAPIYIHappyHorse})
	got, err := client.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	if got.Supported || !strings.Contains(got.Message, "HappyHorse") {
		t.Fatalf("expected unsupported happyhorse balance, got %+v", got)
	}
}

func TestAPIYIGeneratePayloadTextAndReferenceImages(t *testing.T) {
	var createBody map[string]any
	var getCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Fatalf("apiyi requests should force identity encoding, got %q", r.Header.Get("Accept-Encoding"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiTaskPath:
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-test"}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiTaskPath+"/cgt-test":
			getCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"cgt-test",
				"model":"doubao-seedance-2-0-fast-260128",
				"status":"succeeded",
				"content":{"video_url":"https://example.com/out.mp4"},
				"usage":{"completion_tokens":108900}
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYISeedance, BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.Generate(context.Background(), Options{
		Prompt:      "生成商品视频",
		Images:      []ImageInput{{URL: "data:image/png;base64,abc", Name: "product_anchor"}},
		DurationSec: 5,
		AspectRatio: "9:16",
		Resolution:  "720p",
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if getCount != 1 {
		t.Fatalf("poll count = %d", getCount)
	}
	if got.Status != "completed" || got.ResultURL != "https://example.com/out.mp4" || got.CostDetail.Price != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if createBody["model"] != apiyiDefaultFastModel || createBody["ratio"] != "9:16" || createBody["resolution"] != "720p" {
		t.Fatalf("unexpected create body: %#v", createBody)
	}
	content, ok := createBody["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("unexpected content: %#v", createBody["content"])
	}
	text, _ := content[0].(map[string]any)
	img, _ := content[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "生成商品视频" {
		t.Fatalf("unexpected text content: %#v", text)
	}
	if img["type"] != "image_url" || img["role"] != "reference_image" {
		t.Fatalf("unexpected image content: %#v", img)
	}
	imageURL, _ := img["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,abc" {
		t.Fatalf("unexpected image_url content: %#v", imageURL)
	}
}

func TestAPIYIWanGeneratePayloadReferenceImage(t *testing.T) {
	var createBody map[string]any
	var sawAsyncHeader bool
	var getCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiWanTaskPath:
			sawAsyncHeader = r.Header.Get("X-DashScope-Async") == "enable"
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"wan-test","task_status":"PENDING"},"request_id":"req-test"}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiWanQueryPath+"/wan-test":
			getCount++
			if r.Header.Get("X-DashScope-Async") != "" {
				t.Fatalf("query should not include async header")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"task_id":"wan-test",
				"status":"completed",
				"progress":100,
				"result_url":"https://example.com/wan.mp4"
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYIWan27, BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.Generate(context.Background(), Options{
		Prompt:      "参考图片生成商品视频",
		Images:      []ImageInput{{URL: "https://example.com/product.png"}},
		DurationSec: 5,
		AspectRatio: "9:16",
		Resolution:  "720p",
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !sawAsyncHeader {
		t.Fatal("create request missing X-DashScope-Async header")
	}
	if getCount != 1 {
		t.Fatalf("poll count = %d", getCount)
	}
	if got.Status != "completed" || got.ResultURL != "https://example.com/wan.mp4" || got.CostDetail.Price != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if createBody["model"] != apiyiWanDefaultModel {
		t.Fatalf("unexpected model: %#v", createBody["model"])
	}
	input, _ := createBody["input"].(map[string]any)
	if input["prompt"] != "参考图片生成商品视频" {
		t.Fatalf("unexpected input: %#v", input)
	}
	media, ok := input["media"].([]any)
	if !ok || len(media) != 1 {
		t.Fatalf("unexpected media: %#v", input["media"])
	}
	firstMedia, _ := media[0].(map[string]any)
	if firstMedia["type"] != "reference_image" || firstMedia["url"] != "https://example.com/product.png" {
		t.Fatalf("unexpected media item: %#v", firstMedia)
	}
	params, _ := createBody["parameters"].(map[string]any)
	if params["resolution"] != "720P" || params["ratio"] != "9:16" || params["duration"] != float64(5) {
		t.Fatalf("unexpected parameters: %#v", params)
	}
	if params["prompt_extend"] != true || params["watermark"] != false {
		t.Fatalf("unexpected parameter defaults: %#v", params)
	}
}

func TestAPIYIWanGeneratePayloadTextFallback(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiWanTaskPath:
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"wan-text","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiWanQueryPath+"/wan-text":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"task_id":"wan-text",
				"status":"completed",
				"progress":100,
				"result_url":"https://example.com/text.mp4"
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYIWan27, BaseURL: srv.URL, APIKey: "test-key", Model: apiyiWanDefaultModel})
	if _, err := client.Generate(context.Background(), Options{Prompt: "纯文本生成视频"}); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if createBody["model"] != apiyiWanTextModel {
		t.Fatalf("expected text fallback model, got %#v", createBody["model"])
	}
	input, _ := createBody["input"].(map[string]any)
	if _, ok := input["media"]; ok {
		t.Fatalf("text fallback should not send media: %#v", input)
	}
}

func TestAPIYIWanGeneratePayloadReferenceVideo(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiWanTaskPath:
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"wan-video","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiWanQueryPath+"/wan-video":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"task_id":"wan-video",
				"status":"completed",
				"progress":100,
				"result_url":"https://example.com/ref-video.mp4"
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYIWan27, BaseURL: srv.URL, APIKey: "test-key"})
	if _, err := client.Generate(context.Background(), Options{
		Prompt:            "参考视频生成新视频",
		ReferenceVideoURL: "https://example.com/ref.mp4",
	}); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if createBody["model"] != apiyiWanDefaultModel {
		t.Fatalf("unexpected model: %#v", createBody["model"])
	}
	input, _ := createBody["input"].(map[string]any)
	media, ok := input["media"].([]any)
	if !ok || len(media) != 1 {
		t.Fatalf("unexpected media: %#v", input["media"])
	}
	firstMedia, _ := media[0].(map[string]any)
	if firstMedia["type"] != "reference_video" || firstMedia["url"] != "https://example.com/ref.mp4" {
		t.Fatalf("unexpected media item: %#v", firstMedia)
	}
}

func TestAPIYIHappyHorseGeneratePayloadReferenceImagesAllowsNine(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiWanTaskPath:
			if r.Header.Get("X-DashScope-Async") != "enable" {
				t.Fatalf("missing async header: %q", r.Header.Get("X-DashScope-Async"))
			}
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"hh-test","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiWanQueryPath+"/hh-test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"task_id":"hh-test",
				"status":"completed",
				"progress":100,
				"result_url":"https://example.com/hh.mp4"
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	images := make([]ImageInput, 0, 10)
	for i := 0; i < 10; i++ {
		images = append(images, ImageInput{URL: "https://example.com/ref" + strconv.Itoa(i) + ".png"})
	}
	client := NewClient(Config{ChannelType: ChannelAPIYIHappyHorse, BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.Generate(context.Background(), Options{
		Prompt: "保持参考图主体生成视频",
		Images: images,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if got.Status != "completed" || got.ResultURL != "https://example.com/hh.mp4" || got.CostDetail.Price != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if createBody["model"] != apiyiHappyHorseDefaultModel {
		t.Fatalf("unexpected model: %#v", createBody["model"])
	}
	input, _ := createBody["input"].(map[string]any)
	media, ok := input["media"].([]any)
	if !ok || len(media) != 9 {
		t.Fatalf("happyhorse should send up to 9 reference images, got %#v", input["media"])
	}
	firstMedia, _ := media[0].(map[string]any)
	if firstMedia["type"] != "reference_image" || firstMedia["url"] != "https://example.com/ref0.png" {
		t.Fatalf("unexpected first media: %#v", firstMedia)
	}
}

func TestAPIYIHappyHorseGeneratePayloadTextFallback(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiWanTaskPath:
			data, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(data, &createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"hh-text","task_status":"PENDING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiWanQueryPath+"/hh-text":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"task_id":"hh-text",
				"status":"completed",
				"progress":100,
				"result_url":"https://example.com/hh-text.mp4"
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYIHappyHorse, BaseURL: srv.URL, APIKey: "test-key", Model: apiyiHappyHorseDefaultModel})
	if _, err := client.Generate(context.Background(), Options{Prompt: "纯文本生成视频"}); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if createBody["model"] != apiyiHappyHorseTextModel {
		t.Fatalf("expected happyhorse text fallback model, got %#v", createBody["model"])
	}
	input, _ := createBody["input"].(map[string]any)
	if _, ok := input["media"]; ok {
		t.Fatalf("text fallback should not send media: %#v", input)
	}
}

func TestAPIYICreateUsesTaskTimeoutInsteadOfDefaultRequestTimeout(t *testing.T) {
	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiTaskPath:
			createCalled = true
			time.Sleep(120 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-slow"}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiTaskPath+"/cgt-slow":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"cgt-slow",
				"model":"doubao-seedance-2-0-fast-260128",
				"status":"succeeded",
				"content":{"video_url":"https://example.com/slow.mp4"}
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{
		ChannelType: ChannelAPIYISeedance,
		BaseURL:     srv.URL,
		APIKey:      "test-key",
		TimeoutSec:  2,
	})
	client.httpClient.Timeout = 50 * time.Millisecond

	got, err := client.Generate(context.Background(), Options{Prompt: "生成商品视频"})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !createCalled {
		t.Fatal("create was not called")
	}
	if got.Status != "completed" || got.ResultURL != "https://example.com/slow.mp4" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAPIYIStatusMapping(t *testing.T) {
	for upstream, want := range map[string]string{
		"queued":      "queued",
		"running":     "running",
		"in_progress": "running",
		"processing":  "running",
		"succeeded":   "completed",
		"completed":   "completed",
		"failed":      "failed",
		"expired":     "failed",
	} {
		if got := mapAPIYIStatus(upstream); got != want {
			t.Fatalf("status %q => %q, want %q", upstream, got, want)
		}
	}
}

func TestAPIYIWanStatusMapping(t *testing.T) {
	for upstream, want := range map[string]string{
		"submitted":   "queued",
		"pending":     "queued",
		"queued":      "queued",
		"in_progress": "running",
		"running":     "running",
		"completed":   "completed",
		"succeeded":   "completed",
		"failed":      "failed",
		"expired":     "failed",
	} {
		if got := mapAPIYIWanStatus(upstream); got != want {
			t.Fatalf("status %q => %q, want %q", upstream, got, want)
		}
	}
}

func TestAPIYIProgressParsesUpstreamPercent(t *testing.T) {
	task := apiyiTaskResp{Status: "running", Progress: "30%"}
	got := task.toResult("cgt-progress")
	if got.Status != "running" || got.Progress != 30 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.TaskID != "cgt-progress" {
		t.Fatalf("task id = %q", got.TaskID)
	}
}

func TestAPIYIProgressParsesWrappedDataPercent(t *testing.T) {
	var task apiyiTaskResp
	if err := json.Unmarshal([]byte(`{
		"id":"cgt-progress",
		"status":"in_progress",
		"data":{"id":"cgt-progress","status":"running","progress":"30%"}
	}`), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	got := task.toResult("")
	if got.TaskID != "cgt-progress" || got.Status != "running" || got.Progress != 30 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAPIYIRunningWithoutProgressUsesConservativeFallback(t *testing.T) {
	task := apiyiTaskResp{Status: "running"}
	got := task.toResult("cgt-no-progress")
	if got.Status != "running" || got.Progress != 10 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAPIYIWrappedTaskReadsDataContentVideoURL(t *testing.T) {
	var task apiyiTaskResp
	if err := json.Unmarshal([]byte(`{
		"id":"cgt-wrapper",
		"task_id":"cgt-wrapper",
		"status":"completed",
		"progress":"100%",
		"data":{
			"id":"cgt-wrapper",
			"model":"doubao-seedance-2-0-fast-260128",
			"status":"succeeded",
			"content":{"video_url":"https://example.com/wrapped.mp4"}
		}
	}`), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	got := task.toResult("")
	if got.TaskID != "cgt-wrapper" || got.Status != "completed" || got.Progress != 100 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.ResultURL != "https://example.com/wrapped.mp4" || got.ModelID != apiyiDefaultFastModel {
		t.Fatalf("unexpected result fields: %+v", got)
	}
}

func TestAPIYIPollUsesMappedCompletedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == apiyiTaskPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-wrapper"}`))
		case r.Method == http.MethodGet && r.URL.Path == apiyiTaskPath+"/cgt-wrapper":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id":"cgt-wrapper",
				"task_id":"cgt-wrapper",
				"status":"completed",
				"progress":"100%",
				"data":{
					"id":"cgt-wrapper",
					"status":"succeeded",
					"content":{"video_url":"https://example.com/wrapped.mp4"}
				}
			}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYISeedance, BaseURL: srv.URL, APIKey: "test-key"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, err := client.Generate(ctx, Options{Prompt: "生成商品视频"})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if got.Status != "completed" || got.Progress != 100 || got.ResultURL != "https://example.com/wrapped.mp4" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAPIYIGetTaskReadsContentVideoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiyiTaskPath+"/cgt-test" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"cgt-test",
			"model":"doubao-seedance-2-0-260128",
			"status":"succeeded",
			"content":{"video_url":"https://example.com/result.mp4"},
			"usage":{"completion_tokens":2000}
		}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYISeedance, BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.GetTask(context.Background(), "cgt-test")
	if err != nil {
		t.Fatalf("GetTask error: %v", err)
	}
	if got.Status != "completed" || got.ResultURL != "https://example.com/result.mp4" || got.CostType != "credits" {
		t.Fatalf("unexpected task: %+v", got)
	}
	if got.CostDetail.Price != 1 || !strings.Contains(string(got.CostDetail.Raw), "completion_tokens") {
		t.Fatalf("unexpected cost detail: %+v raw=%s", got.CostDetail, string(got.CostDetail.Raw))
	}
}

func TestAPIYIWanGetTaskReadsResultURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiyiWanQueryPath+"/wan-test" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"task_id":"wan-test",
			"status":"in_progress",
			"progress":30,
			"result_url":""
		}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{ChannelType: ChannelAPIYIWan27, BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.GetTask(context.Background(), "wan-test")
	if err != nil {
		t.Fatalf("GetTask error: %v", err)
	}
	if got.TaskID != "wan-test" || got.Status != "running" || got.Progress != 30 {
		t.Fatalf("unexpected task: %+v", got)
	}
	if got.CostDetail.Price != 1 || got.CostType != "credits" {
		t.Fatalf("unexpected cost detail: %+v", got)
	}
}

func TestTaskCostDetailPriceParsesNumberAndString(t *testing.T) {
	for name, raw := range map[string]string{
		"number": `{"price":1.25,"model_name":"Seedance"}`,
		"string": `{"price":"2.5","model_name":"Seedance"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var cost struct {
				ModelName string          `json:"model_name"`
				Price     json.RawMessage `json:"price"`
			}
			if err := json.Unmarshal([]byte(raw), &cost); err != nil {
				t.Fatal(err)
			}
			task := taskResp{}
			task.CostDetail.ModelName = cost.ModelName
			task.CostDetail.Price = cost.Price
			got := task.costDetail()
			if got.ModelName != "Seedance" || got.Price <= 0 {
				t.Fatalf("unexpected cost detail: %+v", got)
			}
		})
	}
}

func testClientWithModels(t *testing.T, models []Model) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models)
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, APIKey: "test-key"}), srv
}
