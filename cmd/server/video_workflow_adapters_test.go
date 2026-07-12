package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/432539/gpt2api/internal/videogen"
	"github.com/432539/gpt2api/internal/videoworkflow"
)

func TestNormalizeVideoWorkflowJSON(t *testing.T) {
	tests := map[string]string{
		"plain":  `{"scenes":[]}`,
		"fenced": "```json\n{\"scenes\":[{\"index\":1}]}\n```",
		"prose":  `结果如下： {"scenes":[1,2,3,4]} 请查收`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got := normalizeVideoWorkflowJSON(input)
			if !json.Valid(got) {
				t.Fatalf("invalid JSON: %q", got)
			}
		})
	}
}

func TestVideoWorkflowUpstreamModel(t *testing.T) {
	snapshot := videogen.TaskConfigSnapshot{Model: "doubao-seedance-2-0-fast-260128"}
	tests := map[string]struct {
		requested string
		want      string
	}{
		"empty":          {requested: "", want: snapshot.Model},
		"default":        {requested: " DEFAULT ", want: snapshot.Model},
		"semantic alias": {requested: " Seedance-2.0 ", want: snapshot.Model},
		"explicit model": {requested: "doubao-seedance-2-0-260128", want: "doubao-seedance-2-0-260128"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := videoWorkflowUpstreamModel(tt.requested, snapshot); got != tt.want {
				t.Fatalf("model=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestValidateVideoWorkflowProviderSnapshot(t *testing.T) {
	for _, model := range []string{"wan2.7-r2v", " WAN2.7-I2V ", "wan2.7-t2v"} {
		if err := validateVideoWorkflowProviderSnapshot(model, videogen.TaskConfigSnapshot{ChannelType: " APIYI_WAN27 "}); err != nil {
			t.Fatalf("valid Wan2.7 snapshot rejected for model %q: %v", model, err)
		}
	}
	if err := validateVideoWorkflowProviderSnapshot("doubao-seedance-2-0-260128", videogen.TaskConfigSnapshot{ChannelType: videogen.ChannelEchoon}); err != nil {
		t.Fatalf("non-Wan model should not require Wan channel: %v", err)
	}
	for _, channelType := range []string{"", videogen.ChannelAPIYISeedance, videogen.ChannelEchoon} {
		err := validateVideoWorkflowProviderSnapshot("wan2.7-r2v", videogen.TaskConfigSnapshot{ChannelType: channelType})
		if err == nil || !strings.Contains(err.Error(), videogen.ChannelAPIYIWan27) {
			t.Fatalf("channel %q mismatch error=%v", channelType, err)
		}
	}
}

func TestVideoWorkflowWanModelChannelMismatchFailsBeforeProviderRequest(t *testing.T) {
	for _, channelType := range []string{videogen.ChannelAPIYISeedance, videogen.ChannelEchoon} {
		for _, providerTaskID := range []string{"", "existing-task"} {
			name := channelType + "/generate"
			if providerTaskID != "" {
				name = channelType + "/resume"
			}
			t.Run(name, func(t *testing.T) {
				var requestCount atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					requestCount.Add(1)
					http.Error(w, "provider should not be called", http.StatusInternalServerError)
				}))
				t.Cleanup(server.Close)

				generator := videoWorkflowVideoGenerator{client: videogen.NewClient(videogen.Config{
					ChannelType: channelType,
					BaseURL:     server.URL,
					APIKey:      "test-key",
					Model:       "provider-default",
					TimeoutSec:  1,
				})}
				_, err := generator.GenerateVideo(context.Background(), videoworkflow.VideoGenerationRequest{
					Model:          "wan2.7-r2v",
					Prompt:         "生成测试视频",
					ProviderTaskID: providerTaskID,
				})
				if err == nil || !strings.Contains(err.Error(), videogen.ChannelAPIYIWan27) {
					t.Fatalf("expected Wan channel mismatch, got %v", err)
				}
				if got := requestCount.Load(); got != 0 {
					t.Fatalf("provider received %d requests after channel mismatch", got)
				}
			})
		}
	}
}
