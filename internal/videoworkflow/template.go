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
	graph.Settings.VideoModel = "doubao-seedance-2-0-fast-260128"
	graph.Settings.ImageModel = "gpt-image-2"
	if err := applyAncientDramaV3Defaults(&graph); err != nil {
		return Template{}, err
	}
	NormalizeBackgroundEnvironmentPorts(&graph)
	template.Code = "ancient_drama_seedance"
	template.Version = 3
	template.Name = "古风多角色短剧 · 本地 Seedance"
	template.Description = "按角色定妆、三视图、四幕分镜、场景背景、本机 Seedance/Motion 生成的 15 秒场景视频和时间线合成生成古风短剧。"
	template.Graph = graph
	return template, nil
}

func applyAncientDramaV3Defaults(graph *Graph) error {
	const characterSheetSuffix = "输出一张角色设定参考拼贴：上排全身正面、侧面、背面三视图；中排脸部大特写（正面与3/4侧）刻画五官、妆容、眼神与发型发丝细节；下排服装与物料细节图（面料纹理、刺绣纹样、扣襟缘饰、配饰、鞋履、腰带/披帛等材质特写）；浅底设定稿、布局清晰；统一面容、发型、服装和气质；非照片、非写实、非真人。"
	characterPrompts := map[string]string{
		"role_heroine": "二维国风动画角色设定；虚构成年人古风女主（22岁），鹅蛋脸杏眼，乌黑高挽发髻插白玉簪，穿月白交领襦裙配浅青披帛，气质清冷克制。" + characterSheetSuffix,
		"role_hero":    "二维国风动画角色设定；虚构成年人古风男主（25岁），剑眉星目，束发玉冠，青色圆领袍外罩玄色披风，腰佩木剑，气质沉稳。" + characterSheetSuffix,
		"role_cousin":  "二维国风动画角色设定；虚构成年人古风表小姐（23岁），桃花妆圆脸，双环髻缀红花，朱红罗裙绣金纹，笑意明亮。" + characterSheetSuffix,
	}
	characterViews := map[string]string{
		"role_heroine": "全身三视图 + 脸部特写 + 服装物料细节",
		"role_hero":    "全身三视图 + 脸部特写 + 服装物料细节",
		"role_cousin":  "全身三视图 + 脸部特写 + 服装物料细节",
	}
	backgroundPrompts := map[string]string{
		"background_1": "二维国风动画场景背景；江南雨夜青石板巷，纸伞与灯笼倒影，远处木门半掩；无人或仅远景剪影；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
		"background_2": "二维国风动画场景背景；府邸后花园夜色，石桥、垂柳、凉亭烛火，池面映星；无人空镜；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
		"background_3": "二维国风动画场景背景；朱漆宴厅华灯高悬，红烛长案、屏风字画，热闹却空无一人；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
		"background_4": "二维国风动画场景背景；河岸放河灯，水面漂满莲花灯，远山淡墨月色；空镜无特写人物；虚构成年人世界观；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。",
	}
	videoPrompts := map[string]string{
		"video_1": "生成9:16、15秒二维国风动画剧情视频；图1=场景背景，图2=女主，图3=男主，图4=表小姐；雨夜巷口女主撑伞归来，与男主对视停步；所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。",
		"video_2": "生成9:16、15秒二维国风动画剧情视频；图1=场景背景，图2=女主，图3=男主，图4=表小姐；花园凉亭三人叙旧，表小姐打趣，气氛先松后紧；所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。",
		"video_3": "生成9:16、15秒二维国风动画剧情视频；图1=场景背景，图2=女主，图3=男主，图4=表小姐；宴厅揭开旧婚约冲突，女主抽身离席，男主追出；所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。",
		"video_4": "生成9:16、15秒二维国风动画剧情视频；图1=场景背景，图2=女主，图3=男主，图4=表小姐；河灯夜两人放下误会并肩放灯，表小姐远处祝福；所有角色均为虚构成年人；非照片、非写实、非真人；保持角色造型、背景美术和动作连续一致。",
	}
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		var prompt string
		switch {
		case node.ID == "brief":
			prompt = "创作四幕古风短剧《雨巷旧约》创意简报：女主离乡三年雨夜归来，重逢青梅竹马男主与热情表小姐；冲突是当年未解的婚约与误会，结局河灯夜和解。视觉统一为二维国风动画，所有角色均为虚构成年人；非照片、非写实、非真人。四幕分别为：雨巷重逢、花园叙旧、宴厅冲突、河灯和解。"
		case node.ID == "script" || node.Type == NodeScript:
			prompt = "输出严格 JSON 四场剧本，每场固定 15 秒。故事《雨巷旧约》：幕1雨巷重逢（女主归来、男主停步）、幕2花园叙旧（三人闲谈暗藏试探）、幕3宴厅冲突（旧约被当众提起）、幕4河灯和解（放下误会）。每场含地点、时间、台词，以及 start/end/camera/action/expression/lighting/audio 时间段；角色均为虚构成年人；视觉为二维国风动画。"
		case characterPrompts[node.ID] != "":
			prompt = characterPrompts[node.ID]
			if view := characterViews[node.ID]; view != "" {
				if err := setTemplateNodeStringField(node, "view", view); err != nil {
					return fmt.Errorf("set ancient drama v3 view for %s: %w", node.ID, err)
				}
			}
		case backgroundPrompts[node.ID] != "":
			prompt = backgroundPrompts[node.ID]
		case node.Type == NodeBackground:
			prompt = "生成二维国风动画场景背景；无人空镜或仅远景不可辨识剪影；禁止近中景人物与肢体动作；道具仅可静置；非照片、非写实、非真人；9:16单镜头、非拼贴，保持统一古风美术与清晰空间层次。"
		case videoPrompts[node.ID] != "":
			prompt = videoPrompts[node.ID]
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
	return setTemplateNodeStringField(node, "prompt", prompt)
}

func setTemplateNodeStringField(node *Node, key, value string) error {
	var config map[string]json.RawMessage
	if len(node.Config) > 0 {
		if err := json.Unmarshal(node.Config, &config); err != nil {
			return err
		}
	}
	if config == nil {
		config = make(map[string]json.RawMessage)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	config[key] = encoded
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
		graph.Nodes[i].PositionMode = PositionAuto
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
		StoryboardApprovalPolicy: ApprovalManual, TextModel: "default", ImageModel: "gpt-image-2", VideoModel: "doubao-seedance-2-0-fast-260128",
	}
	timelineConfig, _ := json.Marshal(TimelineConfig{Clips: []TimelineClip{{
		ID: "clip_1", SourceNodeID: "video_1", SourcePort: "video", TrimOutMS: SceneDurationMS,
	}}})
	graph := Graph{SchemaVersion: SchemaVersionV2, Settings: settings}
	graph.Nodes = []Node{
		{ID: "brief", Type: NodeStoryBrief, Position: Position{X: 40, Y: 160}, Config: json.RawMessage(`{"title":"创意简报","prompt":"描述主题、人物、场景和镜头"}`), Outputs: []Port{{ID: "text", Type: PortText}}},
		{ID: "scene_1", Type: NodeScene, SceneID: "scene_1", DurationSeconds: SceneDuration, Position: Position{X: 300, Y: 160}, Inputs: []Port{{ID: "script", Type: PortText, Required: true}}, Outputs: []Port{{ID: "scene", Type: PortScene}}},
		{ID: "background_1", Type: NodeBackground, SceneID: "scene_1", Position: Position{X: 540, Y: 160}, Inputs: []Port{{ID: "environment", Label: "环境（地点/灯光/静物）", Type: PortScene, Required: true}}, Outputs: []Port{{ID: "image", Type: PortImage}}},
		{ID: "video_1", Type: NodeVideo, SceneID: "scene_1", DurationSeconds: SceneDuration, Position: Position{X: 800, Y: 160}, Inputs: []Port{{ID: "scene", Type: PortScene, Required: true}, {ID: "background", Type: PortImage, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}}},
		{ID: "timeline", Type: NodeTimeline, Locked: true, Position: Position{X: 1060, Y: 160}, Config: timelineConfig, Inputs: []Port{{ID: "clip_1", Type: PortVideo, Required: true}}, Outputs: []Port{{ID: "videos", Type: PortVideoList}}},
		{ID: "compose", Type: NodeCompose, Locked: true, Position: Position{X: 1300, Y: 160}, Inputs: []Port{{ID: "videos", Type: PortVideoList, Required: true}}, Outputs: []Port{{ID: "video", Type: PortVideo}}},
	}
	for i := range graph.Nodes {
		graph.Nodes[i].PositionMode = PositionAuto
	}
	graph.Edges = []Edge{
		{ID: "e_brief_scene", Source: "brief", SourcePort: "text", Target: "scene_1", TargetPort: "script"},
		{ID: "e_scene_background", Source: "scene_1", SourcePort: "scene", Target: "background_1", TargetPort: "environment"},
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
