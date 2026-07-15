package videoworkflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SceneDurationSeconds = 15
	OutputFPS            = 30
	OutputAudioRate      = 48000
)

var ErrFFmpegUnavailable = errors.New("ffmpeg/ffprobe unavailable")

type OutputSpec struct {
	AspectRatio string
	Resolution  Resolution
	Width       int
	Height      int
}

func ResolveOutputSpec(aspectRatio string) (OutputSpec, error) {
	spec, err := ResolveOutputSpecFor(aspectRatio, Resolution720p)
	spec.Resolution = "" // 保持 v1 调用方的结构兼容
	return spec, err
}

func ResolveOutputSpecFor(aspectRatio string, resolution Resolution) (OutputSpec, error) {
	if resolution == "" {
		resolution = Resolution720p
	}
	if resolution != Resolution720p && resolution != Resolution1080p {
		return OutputSpec{}, fmt.Errorf("unsupported resolution: %s", resolution)
	}
	long, short := 1280, 720
	if resolution == Resolution1080p {
		long, short = 1920, 1080
	}
	switch strings.TrimSpace(aspectRatio) {
	case "9:16":
		return OutputSpec{AspectRatio: "9:16", Resolution: resolution, Width: short, Height: long}, nil
	case "16:9":
		return OutputSpec{AspectRatio: "16:9", Resolution: resolution, Width: long, Height: short}, nil
	case "1:1":
		return OutputSpec{AspectRatio: "1:1", Resolution: resolution, Width: short, Height: short}, nil
	default:
		return OutputSpec{}, fmt.Errorf("unsupported aspect ratio: %s", aspectRatio)
	}
}

type Composer struct {
	FFmpegPath  string
	FFprobePath string
}

type mediaProbe struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
		Duration   string `json:"duration"`
	} `json:"streams"`
}

type ComposeResult struct {
	Path       string
	Duration   float64
	Width      int
	Height     int
	FPS        float64
	VideoCodec string
	AudioCodec string
	SampleRate int
	Channels   int
}

type ComposeClip struct {
	Path      string
	TrimInMS  int
	TrimOutMS int
}

func NewComposer() *Composer {
	return &Composer{FFmpegPath: "ffmpeg", FFprobePath: "ffprobe"}
}

func (c *Composer) Available() bool {
	_, err1 := exec.LookPath(defaultString(c.FFmpegPath, "ffmpeg"))
	_, err2 := exec.LookPath(defaultString(c.FFprobePath, "ffprobe"))
	return err1 == nil && err2 == nil
}

func (c *Composer) Compose(ctx context.Context, inputs []string, outputPath string, spec OutputSpec) (*ComposeResult, error) {
	clips := make([]ComposeClip, 0, len(inputs))
	for _, input := range inputs {
		clips = append(clips, ComposeClip{Path: input, TrimOutMS: SceneDurationMS})
	}
	return c.ComposeClips(ctx, clips, outputPath, spec)
}

func (c *Composer) ComposeClips(ctx context.Context, clips []ComposeClip, outputPath string, spec OutputSpec) (*ComposeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if len(clips) < 1 || len(clips) > 4 {
		return nil, fmt.Errorf("compose requires 1-4 input videos")
	}
	inputs := make([]string, len(clips))
	totalDurationMS := 0
	for i := range clips {
		if clips[i].TrimOutMS == 0 {
			clips[i].TrimOutMS = SceneDurationMS
		}
		if err := validateComposeClip(clips[i]); err != nil {
			return nil, fmt.Errorf("clip %d: %w", i+1, err)
		}
		inputs[i] = clips[i].Path
		totalDurationMS += clips[i].TrimOutMS - clips[i].TrimInMS
	}
	if len(inputs) < 1 || len(inputs) > 4 {
		return nil, fmt.Errorf("compose requires 1-4 input videos")
	}
	if spec.Width <= 0 || spec.Height <= 0 {
		return nil, errors.New("invalid output dimensions")
	}
	if !c.Available() {
		return nil, ErrFFmpegUnavailable
	}
	probes := make([]mediaProbe, len(inputs))
	for i, input := range inputs {
		if strings.TrimSpace(input) == "" {
			return nil, fmt.Errorf("input %d path is empty", i+1)
		}
		if _, err := os.Stat(input); err != nil {
			return nil, fmt.Errorf("input %d: %w", i+1, err)
		}
		probe, err := c.probe(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("probe input %d: %w", i+1, err)
		}
		if !hasStream(probe, "video") {
			return nil, fmt.Errorf("input %d has no video stream", i+1)
		}
		probes[i] = probe
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, err
	}
	tempPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".tmp-" + uuid.NewString() + ".mp4"
	defer os.Remove(tempPath)

	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	for _, clip := range clips {
		start := float64(clip.TrimInMS) / 1000
		duration := float64(clip.TrimOutMS-clip.TrimInMS) / 1000
		// -ss/-t 放在 -i 前，只解码入出点窗口，避免整片 scale/tpad 撑爆容器内存。
		args = append(args,
			"-ss", formatComposeSeconds(start),
			"-t", formatComposeSeconds(duration),
			"-protocol_whitelist", "file,pipe",
			"-i", clip.Path,
		)
	}
	filter := buildComposeFilterClips(probes, clips, spec)
	args = append(args,
		"-filter_complex", filter,
		"-filter_complex_threads", "2",
		"-map", "[vout]", "-map", "[aout]",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
		"-threads", "2",
		"-pix_fmt", "yuv420p", "-r", strconv.Itoa(OutputFPS),
		"-c:a", "aac", "-b:a", "192k", "-ar", strconv.Itoa(OutputAudioRate), "-ac", "2",
		"-movflags", "+faststart", tempPath,
	)
	cmd := exec.CommandContext(ctx, defaultString(c.FFmpegPath, "ffmpeg"), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 4000 {
			msg = msg[len(msg)-4000:]
		}
		return nil, fmt.Errorf("%s", formatComposeExecError(err, msg))
	}
	result, err := c.validateOutputDuration(ctx, tempPath, float64(totalDurationMS)/1000, spec)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return nil, fmt.Errorf("publish composed video: %w", err)
	}
	result.Path = outputPath
	return result, nil
}

// ExtractPreviewFrame 从视频抽取首帧 JPEG，供素材库与节点卡封面使用。
func (c *Composer) ExtractPreviewFrame(ctx context.Context, videoPath, outputPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if strings.TrimSpace(videoPath) == "" || strings.TrimSpace(outputPath) == "" {
		return errors.New("video and output paths are required")
	}
	if !c.Available() {
		return ErrFFmpegUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return err
	}
	tempPath := outputPath + ".tmp-" + uuid.NewString() + ".jpg"
	defer os.Remove(tempPath)
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", "0", "-i", videoPath,
		"-frames:v", "1", "-q:v", "3",
		tempPath,
	}
	cmd := exec.CommandContext(ctx, defaultString(c.FFmpegPath, "ffmpeg"), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 4000 {
			msg = msg[len(msg)-4000:]
		}
		return fmt.Errorf("ffmpeg preview frame: %w: %s", err, msg)
	}
	info, err := os.Stat(tempPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("ffmpeg preview frame produced empty output")
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("publish preview frame: %w", err)
	}
	return nil
}

func buildComposeFilter(probes []mediaProbe, spec OutputSpec) string {
	clips := make([]ComposeClip, len(probes))
	for i := range clips {
		clips[i].TrimOutMS = SceneDurationMS
	}
	return buildComposeFilterClips(probes, clips, spec)
}

func buildComposeFilterClips(probes []mediaProbe, clips []ComposeClip, spec OutputSpec) string {
	parts := make([]string, 0, len(probes)*2+1)
	concatInputs := strings.Builder{}
	for i, probe := range probes {
		duration := float64(clips[i].TrimOutMS-clips[i].TrimInMS) / 1000
		// 输入侧已按入出点裁切；此处只标准化分辨率/帧率，并在源片段偏短时克隆尾帧补足时长。
		parts = append(parts, fmt.Sprintf(
			"[%d:v:0]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,setsar=1,fps=%d,tpad=stop_mode=clone:stop_duration=%.3f,trim=duration=%.3f,setpts=PTS-STARTPTS[v%d]",
			i, spec.Width, spec.Height, spec.Width, spec.Height, OutputFPS, duration, duration, i,
		))
		if hasStream(probe, "audio") {
			parts = append(parts, fmt.Sprintf(
				"[%d:a:0]aresample=%d,aformat=sample_fmts=fltp:channel_layouts=stereo,apad=whole_dur=%.3f,atrim=duration=%.3f,asetpts=PTS-STARTPTS[a%d]",
				i, OutputAudioRate, duration, duration, i,
			))
		} else {
			parts = append(parts, fmt.Sprintf("anullsrc=r=%d:cl=stereo:d=%.3f,asetpts=PTS-STARTPTS[a%d]", OutputAudioRate, duration, i))
		}
		fmt.Fprintf(&concatInputs, "[v%d][a%d]", i, i)
	}
	parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[vout][aout]", concatInputs.String(), len(probes)))
	return strings.Join(parts, ";")
}

func formatComposeSeconds(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func formatComposeExecError(err error, stderr string) string {
	errText := strings.TrimSpace(err.Error())
	if strings.Contains(errText, "signal: killed") {
		if stderr == "" {
			return "ffmpeg compose: signal: killed（进程被系统终止，通常是容器内存不足）"
		}
		return fmt.Sprintf("ffmpeg compose: signal: killed（进程被系统终止，通常是容器内存不足）: %s", stderr)
	}
	if stderr == "" {
		return fmt.Sprintf("ffmpeg compose: %s", errText)
	}
	return fmt.Sprintf("ffmpeg compose: %s: %s", errText, stderr)
}

func (c *Composer) validateOutput(ctx context.Context, path string, sceneCount int, spec OutputSpec) (*ComposeResult, error) {
	return c.validateOutputDuration(ctx, path, float64(sceneCount*SceneDurationSeconds), spec)
}

func (c *Composer) validateOutputDuration(ctx context.Context, path string, wantDuration float64, spec OutputSpec) (*ComposeResult, error) {
	probe, err := c.probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe composed output: %w", err)
	}
	duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	if math.Abs(duration-wantDuration) > 0.05 {
		return nil, fmt.Errorf("composed duration %.3fs, want %.3fs (+/-0.05s)", duration, wantDuration)
	}
	result := &ComposeResult{Duration: duration}
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			result.VideoCodec = stream.CodecName
			result.Width = stream.Width
			result.Height = stream.Height
			result.FPS = parseRate(stream.RFrameRate)
		case "audio":
			result.AudioCodec = stream.CodecName
			result.SampleRate, _ = strconv.Atoi(stream.SampleRate)
			result.Channels = stream.Channels
		}
	}
	if result.Width != spec.Width || result.Height != spec.Height || math.Abs(result.FPS-OutputFPS) > 0.01 {
		return nil, fmt.Errorf("unexpected output video %dx%d %.3ffps", result.Width, result.Height, result.FPS)
	}
	if result.VideoCodec != "h264" || result.AudioCodec != "aac" || result.SampleRate != OutputAudioRate || result.Channels != 2 {
		return nil, fmt.Errorf("unexpected codecs video=%s audio=%s rate=%d channels=%d", result.VideoCodec, result.AudioCodec, result.SampleRate, result.Channels)
	}
	return result, nil
}

func validateComposeClip(clip ComposeClip) error {
	if strings.TrimSpace(clip.Path) == "" {
		return errors.New("path is empty")
	}
	return validateTimelineTrim(TimelineClip{ID: filepath.Base(clip.Path), TrimInMS: clip.TrimInMS, TrimOutMS: clip.TrimOutMS})
}

func (c *Composer) probe(ctx context.Context, path string) (mediaProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, defaultString(c.FFprobePath, "ffprobe"),
		"-v", "error", "-protocol_whitelist", "file,pipe", "-show_streams", "-show_format", "-of", "json", path)
	out, err := cmd.Output()
	if err != nil {
		return mediaProbe{}, err
	}
	var probe mediaProbe
	if err := json.Unmarshal(out, &probe); err != nil {
		return mediaProbe{}, err
	}
	return probe, nil
}

func hasStream(probe mediaProbe, streamType string) bool {
	for _, stream := range probe.Streams {
		if stream.CodecType == streamType {
			return true
		}
	}
	return false
}

func parseRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		n, _ := strconv.ParseFloat(parts[0], 64)
		d, _ := strconv.ParseFloat(parts[1], 64)
		if d != 0 {
			return n / d
		}
	}
	v, _ := strconv.ParseFloat(value, 64)
	return v
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
