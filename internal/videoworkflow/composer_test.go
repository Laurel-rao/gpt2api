package videoworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveOutputSpec(t *testing.T) {
	tests := map[string]OutputSpec{
		"9:16": {AspectRatio: "9:16", Width: 720, Height: 1280},
		"16:9": {AspectRatio: "16:9", Width: 1280, Height: 720},
		"1:1":  {AspectRatio: "1:1", Width: 720, Height: 720},
	}
	for ratio, want := range tests {
		got, err := ResolveOutputSpec(ratio)
		if err != nil || got != want {
			t.Fatalf("ResolveOutputSpec(%q) = %#v, %v; want %#v", ratio, got, err, want)
		}
	}
	if _, err := ResolveOutputSpec("4:3"); err == nil {
		t.Fatal("unsupported ratio should fail")
	}
}

func TestResolveOutputSpec_AllSixOutputs(t *testing.T) {
	want := map[string][2]int{
		"9:16/720p": {720, 1280}, "9:16/1080p": {1080, 1920},
		"16:9/720p": {1280, 720}, "16:9/1080p": {1920, 1080},
		"1:1/720p": {720, 720}, "1:1/1080p": {1080, 1080},
	}
	for key, dimensions := range want {
		parts := strings.Split(key, "/")
		spec, err := ResolveOutputSpecFor(parts[0], Resolution(parts[1]))
		if err != nil || spec.Width != dimensions[0] || spec.Height != dimensions[1] {
			t.Fatalf("%s = %+v, %v", key, spec, err)
		}
	}
	if _, err := ResolveOutputSpecFor("9:16", "4k"); err == nil {
		t.Fatal("unsupported resolution was accepted")
	}
}

func TestBuildComposeFilter_UsesTimelineTrim(t *testing.T) {
	probes := []mediaProbe{{}}
	probes[0].Streams = append(probes[0].Streams, struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
		Duration   string `json:"duration"`
	}{CodecType: "video"})
	filter := buildComposeFilterClips(probes, []ComposeClip{{TrimInMS: 1200, TrimOutMS: 9800}}, OutputSpec{Width: 720, Height: 1280})
	for _, fragment := range []string{"trim=start=1.200:end=9.800", "anullsrc=r=48000:cl=stereo:d=8.600"} {
		if !strings.Contains(filter, fragment) {
			t.Fatalf("filter missing %q: %s", fragment, filter)
		}
	}
}

func TestBuildComposeFilterHandlesMissingAudio(t *testing.T) {
	probes := []mediaProbe{{}, {}}
	probes[0].Streams = append(probes[0].Streams, struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
		Duration   string `json:"duration"`
	}{CodecType: "video"})
	probes[1].Streams = append(probes[1].Streams,
		struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
			Duration   string `json:"duration"`
		}{CodecType: "video"},
		struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
			Duration   string `json:"duration"`
		}{CodecType: "audio"},
	)
	filter := buildComposeFilter(probes, OutputSpec{Width: 720, Height: 1280})
	for _, want := range []string{"anullsrc=r=48000", "[1:a:0]aresample=48000", "concat=n=2:v=1:a=1"} {
		if !contains(filter, want) {
			t.Fatalf("filter missing %q: %s", want, filter)
		}
	}
}

func TestCompose_NormalizesAndConcats(t *testing.T) {
	composer := NewComposer()
	if !composer.Available() {
		if os.Getenv("VIDEO_WORKFLOW_REQUIRE_FFMPEG") == "1" {
			t.Fatal(ErrFFmpegUnavailable)
		}
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	red := filepath.Join(dir, "red.mp4")
	green := filepath.Join(dir, "green.mp4")
	blue := filepath.Join(dir, "blue.mp4")
	makeFixture(t, red, "red", "160x284", 24, false)
	makeFixture(t, green, "green", "284x160", 60, true)
	makeFixture(t, blue, "blue", "160x160", 30, true)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out := filepath.Join(dir, "out.mp4")
	result, err := composer.Compose(ctx, []string{red, green, blue}, out, OutputSpec{AspectRatio: "9:16", Width: 720, Height: 1280})
	if err != nil {
		t.Fatal(err)
	}
	if result.Duration < 44.95 || result.Duration > 45.05 {
		t.Fatalf("duration %.3f, want 45.000 (+/-0.05)", result.Duration)
	}
	if result.Width != 720 || result.Height != 1280 || result.FPS != 30 || result.VideoCodec != "h264" || result.AudioCodec != "aac" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestCompose_AllSixOutputProfiles(t *testing.T) {
	composer := NewComposer()
	if !composer.Available() {
		if os.Getenv("VIDEO_WORKFLOW_REQUIRE_FFMPEG") == "1" {
			t.Fatal(ErrFFmpegUnavailable)
		}
		t.Skip("ffmpeg unavailable")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "source.mp4")
	makeFixture(t, input, "navy", "180x320", 24, false)

	for _, ratio := range []string{"9:16", "16:9", "1:1"} {
		for _, resolution := range []Resolution{Resolution720p, Resolution1080p} {
			name := ratio + "/" + string(resolution)
			t.Run(name, func(t *testing.T) {
				spec, err := ResolveOutputSpecFor(ratio, resolution)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				output := filepath.Join(dir, strings.ReplaceAll(name, ":", "-")+".mp4")
				result, err := composer.ComposeClips(ctx, []ComposeClip{{
					Path: input, TrimInMS: 0, TrimOutMS: 1000,
				}}, output, spec)
				if err != nil {
					t.Fatal(err)
				}
				if result.Duration < 0.95 || result.Duration > 1.05 ||
					result.Width != spec.Width || result.Height != spec.Height ||
					result.FPS != 30 || result.VideoCodec != "h264" ||
					result.AudioCodec != "aac" || result.SampleRate != 48000 || result.Channels != 2 {
					t.Fatalf("unexpected result: %#v", result)
				}
			})
		}
	}
}

func TestCompose_FourClipsTrimmedTo55Point8Seconds(t *testing.T) {
	composer := NewComposer()
	if !composer.Available() {
		if os.Getenv("VIDEO_WORKFLOW_REQUIRE_FFMPEG") == "1" {
			t.Fatal(ErrFFmpegUnavailable)
		}
		t.Skip("ffmpeg unavailable")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "source.mp4")
	makeFixture(t, input, "maroon", "180x320", 30, false)
	clips := []ComposeClip{
		{Path: input, TrimOutMS: 15000},
		{Path: input, TrimInMS: 1200, TrimOutMS: 12000},
		{Path: input, TrimOutMS: 15000},
		{Path: input, TrimOutMS: 15000},
	}
	spec, err := ResolveOutputSpecFor("9:16", Resolution720p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := composer.ComposeClips(ctx, clips, filepath.Join(dir, "trimmed-55.8.mp4"), spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Duration < 55.75 || result.Duration > 55.85 {
		t.Fatalf("duration %.3f, want 55.800 (+/-0.05)", result.Duration)
	}
}

func TestCompose_MissingFFmpeg(t *testing.T) {
	c := &Composer{FFmpegPath: filepath.Join(t.TempDir(), "missing-ffmpeg"), FFprobePath: "ffprobe"}
	_, err := c.Compose(context.Background(), []string{"unused.mp4"}, filepath.Join(t.TempDir(), "out.mp4"), OutputSpec{Width: 720, Height: 1280})
	if !errors.Is(err, ErrFFmpegUnavailable) {
		t.Fatalf("got %v, want ErrFFmpegUnavailable", err)
	}
}

func makeFixture(t *testing.T, path, color, size string, fps int, audio bool) {
	t.Helper()
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=%s:r=%d:d=1", color, size, fps)}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1", "-shortest", "-c:a", "aac")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, out)
	}
}

func contains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
