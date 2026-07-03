package settings

import "testing"

func TestVideoGenChannelSpecificConfig(t *testing.T) {
	svc := &Service{cache: map[string]string{
		VideoGenChannelType:       "apiyi_seedance2",
		VideoGenAPIKey:            "echoon-key",
		VideoGenBaseURL:           "http://app.echoon.top/api/v1",
		VideoGenModel:             "0e37fa2d-72b3-483a-81b4-ad595cd147c7",
		VideoGenAPIYIAPIKey:       "apiyi-key",
		VideoGenAPIYIBaseURL:      "https://api.apiyi.com",
		VideoGenAPIYIModel:        "doubao-seedance-2-0-fast-260128",
		VideoGenWan27APIKey:       "wan-key",
		VideoGenWan27BaseURL:      "https://api.apiyi.com",
		VideoGenWan27Model:        "wan2.7-r2v",
		VideoGenHappyHorseAPIKey:  "hh-key",
		VideoGenHappyHorseBaseURL: "https://api.apiyi.com",
		VideoGenHappyHorseModel:   "happyhorse-1.0-r2v",
		VideoGenTimeoutSec:        "1800",
		VideoGenDurationSec:       "5",
		VideoGenAspectRatio:       "16:9",
		VideoGenResolution:        "720p",
		VideoGenGenerateAudio:     "false",
	}}

	if got := svc.VideoGenAPIKey(); got != "apiyi-key" {
		t.Fatalf("apiyi api key = %q", got)
	}
	if got := svc.VideoGenBaseURL(); got != "https://api.apiyi.com" {
		t.Fatalf("apiyi base url = %q", got)
	}
	if got := svc.VideoGenModel(); got != "doubao-seedance-2-0-fast-260128" {
		t.Fatalf("apiyi model = %q", got)
	}

	svc.cache[VideoGenChannelType] = "echoon"
	if got := svc.VideoGenAPIKey(); got != "echoon-key" {
		t.Fatalf("echoon api key = %q", got)
	}
	if got := svc.VideoGenBaseURL(); got != "http://app.echoon.top/api/v1" {
		t.Fatalf("echoon base url = %q", got)
	}

	svc.cache[VideoGenChannelType] = "apiyi_wan27"
	if got := svc.VideoGenAPIKey(); got != "wan-key" {
		t.Fatalf("wan api key = %q", got)
	}
	if got := svc.VideoGenBaseURL(); got != "https://api.apiyi.com" {
		t.Fatalf("wan base url = %q", got)
	}
	if got := svc.VideoGenModel(); got != "wan2.7-r2v" {
		t.Fatalf("wan model = %q", got)
	}
	cfg := svc.VideoGenConfigForChannel("apiyi_wan27")
	if cfg.ChannelType != "apiyi_wan27" || cfg.APIKey != "wan-key" || cfg.Model != "wan2.7-r2v" {
		t.Fatalf("wan config = %+v", cfg)
	}

	svc.cache[VideoGenChannelType] = "apiyi_happyhorse"
	if got := svc.VideoGenAPIKey(); got != "hh-key" {
		t.Fatalf("happyhorse api key = %q", got)
	}
	if got := svc.VideoGenBaseURL(); got != "https://api.apiyi.com" {
		t.Fatalf("happyhorse base url = %q", got)
	}
	if got := svc.VideoGenModel(); got != "happyhorse-1.0-r2v" {
		t.Fatalf("happyhorse model = %q", got)
	}
	cfg = svc.VideoGenConfigForChannel("apiyi_happyhorse")
	if cfg.ChannelType != "apiyi_happyhorse" || cfg.APIKey != "hh-key" || cfg.Model != "happyhorse-1.0-r2v" {
		t.Fatalf("happyhorse config = %+v", cfg)
	}
}
