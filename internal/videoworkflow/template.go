package videoworkflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed templates/ancient_drama_v1.json
var ancientDramaV1JSON []byte

func BuiltinTemplateV1() (Template, error) {
	var template Template
	if err := json.Unmarshal(ancientDramaV1JSON, &template); err != nil {
		return Template{}, fmt.Errorf("decode embedded video workflow template: %w", err)
	}
	return template, nil
}

func MustBuiltinTemplateV1() Template {
	template, err := BuiltinTemplateV1()
	if err != nil {
		panic(err)
	}
	return template
}

// BuiltinTemplates 返回当前可创建的模板。保留 v1 解码入口仅用于读取历史快照；
// 新建工作流统一使用 Graph v2。
func BuiltinTemplates() ([]Template, error) {
	legacy, err := BuiltinTemplateV1()
	if err != nil {
		return nil, err
	}
	ancient, err := upgradeAncientTemplate(legacy)
	if err != nil {
		return nil, err
	}
	blank := blankVideoCanvasTemplate()
	return []Template{ancient, blank}, nil
}

func upgradeAncientTemplate(template Template) (Template, error) {
	graph, err := UpgradeGraphV1ToV2(template.Graph)
	if err != nil {
		return Template{}, err
	}
	graph.Settings.Resolution = Resolution1080p
	graph.Settings.VideoModel = "wan2.7-r2v"
	if err := applyAncientDramaV3Defaults(&graph); err != nil {
		return Template{}, err
	}
	template.Code = "ancient_drama_seedance"
	template.Version = 3
	template.Name = "古风多角色短剧 · Wan2.7"
	template.Description = "按角色定妆、三视图、四幕分镜、场景背景、Wan2.7 生成的 15 秒场景视频和时间线合成生成古风短剧。"
	template.Graph = graph
	return template, nil
}

func applyAncientDramaV3Defaults(graph *Graph) error {
	characterPrompts := map[string]string{
		"role_heroine": "二维国风动画角色设定；虚构成年人古风女主（22岁），统一面容、发型、服装和气质；非照片、非写实、非真人。",
		"role_hero":    "二维国风动画角色设定；虚构成年人古风男主（25岁），统一面容、发型、服装和气质；非照片、非写实、非真人。",
		"role_cousin":  "二维国风动画角色设定；虚构成年人古风表小姐（23岁），统一面容、发型、服装和气质；非照片、非写实、非真人。",
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		var prompt string
		switch {
		case node.ID == "brief":
			prompt = "创作四幕古风短剧创意简报，描述主题、人物关系、冲突和结局；视觉统一为二维国风动画，所有角色均为虚构成年人；非照片、非写实、非真人。"
		case characterPrompts[node.ID] != "":
			prompt = characterPrompts[node.ID]
		case node.Type == NodeBackground:
			prompt = "生成二维国风动画场景背景；若出现人物仅限虚构成年人；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。"
		case node.Type == NodeVideo:
			prompt = "生成9:16、15秒二维国风动画剧情视频；图1=场景背景，图2=女主，图3=男主，图4=表小姐；所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。"
		case node.Type == NodeTimeline:
			var config TimelineConfig
			if err := json.Unmarshal(node.Config, &config); err != nil {
				return fmt.Errorf("decode ancient drama v3 timeline: %w", err)
			}
			if len(config.Clips) != 4 {
				return fmt.Errorf("ancient drama v3 timeline has %d clips, want 4", len(config.Clips))
			}
			config.Clips[1].TrimInMS = 1_200
			config.Clips[1].TrimOutMS = 12_000
			encodedConfig, err := json.Marshal(config)
			if err != nil {
				return err
			}
			node.Config = encodedConfig
		}
		if prompt != "" {
			if err := setTemplateNodePrompt(node, prompt); err != nil {
				return fmt.Errorf("set ancient drama v3 prompt for %s: %w", node.ID, err)
			}
		}
	}
	return nil
}

func setTemplateNodePrompt(node *Node, prompt string) error {
	var config map[string]json.RawMessage
	if len(node.Config) > 0 {
		if err := json.Unmarshal(node.Config, &config); err != nil {
			return err
		}
	}
	if config == nil {
		config = make(map[string]json.RawMessage)
	}
	encodedPrompt, err := json.Marshal(prompt)
	if err != nil {
		return err
	}
	config["prompt"] = encodedPrompt
	node.Config, err = json.Marshal(config)
	return err
}

func UpgradeGraphV1ToV2(source Graph) (Graph, error) {
	graph, err := CloneGraph(source)
	if err != nil {
		return Graph{}, err
	}
	if graph.SchemaVersion == SchemaVersionV2 {
		return graph, nil
	}
	if graph.SchemaVersion != SchemaVersionV1 {
		return Graph{}, fmt.Errorf("unsupported graph schema version %d", graph.SchemaVersion)
	}
	graph.SchemaVersion = SchemaVersionV2
	graph.Settings.Resolution = Resolution720p
	graph.Settings.SceneDurationMS = graph.Settings.EffectiveSceneDurationMS()
	if graph.Settings.SceneDurationMS == 0 {
		graph.Settings.SceneDurationMS = SceneDurationMS
	}
	graph.Settings.SceneDurationSeconds = 0
	graph.Settings.CharacterApprovalPolicy = ApprovalManual
	graph.Settings.StoryboardApprovalPolicy = ApprovalManual
	for i := range graph.Nodes {
		if graph.Nodes[i].Type != NodeTimeline {
			continue
		}
		var config TimelineConfig
		if err := json.Unmarshal(graph.Nodes[i].Config, &config); err != nil {
			return Graph{}, fmt.Errorf("decode timeline config: %w", err)
		}
		config.Clips = normalizedTimelineClips(config)
		config.ClipNodeIDs = nil
		graph.Nodes[i].Config, err = json.Marshal(config)
		if err != nil {
			return Graph{}, err
		}
	}
	return graph, nil
}

func blankVideoCanvasTemplate() Template {
	settings := Settings{
		AspectRatio: AspectRatioPortrait, Resolution: Resolution720p, FPS: DefaultFPS,
		SceneDurationMS: SceneDurationMS, CharacterApprovalPolicy: ApprovalManual,
		StoryboardApprovalPolicy: ApprovalManual, TextModel: "default", ImageModel: "gpt-image-2", VideoModel: "wan2.7-r2v",
	}
	timelineConfig, _ := json.Marshal(TimelineConfig{Clips: []TimelineClip{{
		ID: "clip_1", SourceNodeID: "video_1", SourcePort: "video", TrimOutMS: SceneDurationMS,
	}}})
	graph := Graph{SchemaVersion: SchemaVersionV2, Settings: settings}
	graph.Nodes = []Node{
		{ID: "brief", Type: NodeStoryBrief, Position: Position{X: 40, Y: 160}, Config: json.RawMessage(`{"title":"创意简报","prompt":"描述主题、人物、场景和镜头"}`), Outputs: []Port{{ID: "text", Type: PortText}}},
		{ID: "scene_1", Type: NodeScene, SceneID: "scene_1", DurationSeconds: SceneDuration, Position: Position{X: 300, Y: 160}, Inputs: []Port{{ID: "script", Type: PortText, Required: true}}, Outputs: []Port{{ID: "scene", Type: PortScene}}},
		{ID: "background_1", Type: NodeBackground, SceneID: "scene_1", Position: Position{X: 540, Y: 160}, Inputs: []Port{{ID: "scene", Type: PortScene, Required: true}}, Outputs: []Port{{ID: "image", Type: PortImage}}},
		{ID: "video_1", Type: NodeVideo, SceneID: "scene_1", DurationSeconds: SceneDuration, Position: Position{X: 800, Y: 160}, Inputs: []Port{{ID: "scene", Type: PortScene, Required: true}, {ID: "background", Type: PortImage, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}}},
		{ID: "timeline", Type: NodeTimeline, Locked: true, Position: Position{X: 1060, Y: 160}, Config: timelineConfig, Inputs: []Port{{ID: "clip_1", Type: PortVideo, Required: true}}, Outputs: []Port{{ID: "videos", Type: PortVideoList}}},
		{ID: "compose", Type: NodeCompose, Locked: true, Position: Position{X: 1300, Y: 160}, Inputs: []Port{{ID: "videos", Type: PortVideoList, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}}},
	}
	graph.Edges = []Edge{
		{ID: "e_brief_scene", Source: "brief", SourcePort: "text", Target: "scene_1", TargetPort: "script"},
		{ID: "e_scene_background", Source: "scene_1", SourcePort: "scene", Target: "background_1", TargetPort: "scene"},
		{ID: "e_scene_video", Source: "scene_1", SourcePort: "scene", Target: "video_1", TargetPort: "scene"},
		{ID: "e_background_video", Source: "background_1", SourcePort: "image", Target: "video_1", TargetPort: "background"},
		{ID: "e_video_timeline", Source: "video_1", SourcePort: "video", Target: "timeline", TargetPort: "clip_1"},
		{ID: "e_timeline_compose", Source: "timeline", SourcePort: "videos", Target: "compose", TargetPort: "videos"},
	}
	graph.Groups = []Group{{ID: "scene_group_1", Type: "scene", SceneID: "scene_1", Enabled: true, DurationSeconds: SceneDuration, NodeIDs: []string{"scene_1", "background_1", "video_1"}, Position: Position{X: 280, Y: 110}, Size: Size{Width: 740, Height: 250}}}
	return Template{Code: "blank_video_canvas", Version: 1, Name: "空白视频画布", Description: "单场景图片、视频与成片工作流", Graph: graph, Enabled: true}
}

func CloneGraph(g Graph) (Graph, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return Graph{}, err
	}
	var cloned Graph
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return Graph{}, err
	}
	return cloned, nil
}
