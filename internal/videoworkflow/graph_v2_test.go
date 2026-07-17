package videoworkflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuiltinTemplatesGraphV2(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 3 {
		t.Fatalf("template count=%d, want 3", len(templates))
	}
	byCode := make(map[string]Template)
	for _, template := range templates {
		byCode[template.Code] = template
		if template.Graph.SchemaVersion != SchemaVersionV2 {
			t.Fatalf("template %s schema=%d", template.Code, template.Graph.SchemaVersion)
		}
		if errs := ValidateGraph(template.Graph, true); len(errs) != 0 {
			t.Fatalf("template %s invalid: %v", template.Code, errs)
		}
	}
	ancient := byCode["ancient_drama_seedance"]
	if len(ancient.Graph.Nodes) != 19 || ancient.Version != 4 || ancient.Graph.Settings.Resolution != Resolution1080p ||
		ancient.Graph.Settings.VideoModel != "doubao-seedance-2-0-fast-260128" ||
		ancient.Graph.Settings.ImageModel != "gpt-image-2" ||
		!strings.Contains(ancient.Name, "Seedance 材料包") || !strings.Contains(ancient.Description, "故事圣经") ||
		!strings.Contains(ancient.Description, "分镜镜头") {
		t.Fatalf("unexpected ancient template: nodes=%d name=%q settings=%+v", len(ancient.Graph.Nodes), ancient.Name, ancient.Graph.Settings)
	}
	globalChairman := byCode["global_chairman_drama_seedance"]
	if len(globalChairman.Graph.Nodes) != 24 || globalChairman.Version != 2 ||
		globalChairman.Graph.Settings.CharacterApprovalPolicy != ApprovalAutoFirst ||
		globalChairman.Graph.Settings.StoryboardApprovalPolicy != ApprovalAuto ||
		globalChairman.Graph.Settings.Resolution != Resolution1080p ||
		!strings.Contains(globalChairman.Name, "资产拆解版") ||
		!strings.Contains(globalChairman.Description, "大纲资产") ||
		!strings.Contains(globalChairman.Description, "角色生成资产") ||
		!strings.Contains(globalChairman.Description, "剧情背景资产") {
		t.Fatalf("unexpected global chairman template: nodes=%d name=%q settings=%+v", len(globalChairman.Graph.Nodes), globalChairman.Name, globalChairman.Graph.Settings)
	}
	blank := byCode["blank_video_canvas"]
	if len(blank.Graph.Nodes) != 6 || blank.Graph.Settings.Resolution != Resolution720p ||
		blank.Graph.Settings.VideoModel != "doubao-seedance-2-0-fast-260128" {
		t.Fatalf("unexpected blank template: nodes=%d settings=%+v", len(blank.Graph.Nodes), blank.Graph.Settings)
	}
}

func TestAncientDramaV4PreproductionAssetsAndSeedanceReferences(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var ancient *Template
	for i := range templates {
		if templates[i].Code == "ancient_drama_seedance" {
			ancient = &templates[i]
			break
		}
	}
	if ancient == nil || ancient.Version != 4 || ancient.Graph.Settings.VideoModel != "doubao-seedance-2-0-fast-260128" {
		t.Fatalf("ancient v4 template=%+v", ancient)
	}

	timeline := ancient.Graph.Nodes[findNodeIndex(ancient.Graph, "timeline")]
	var timelineConfig TimelineConfig
	if err := json.Unmarshal(timeline.Config, &timelineConfig); err != nil {
		t.Fatal(err)
	}
	if len(timelineConfig.Clips) != 4 {
		t.Fatalf("timeline clips=%d", len(timelineConfig.Clips))
	}
	var totalMS int
	for i, clip := range timelineConfig.Clips {
		totalMS += clip.TrimOutMS - clip.TrimInMS
		if i == 1 {
			if clip.TrimInMS != 1_200 || clip.TrimOutMS != 12_000 {
				t.Fatalf("second clip=%+v", clip)
			}
		} else if clip.TrimInMS != 0 || clip.TrimOutMS != SceneDurationMS {
			t.Fatalf("clip %d=%+v", i+1, clip)
		}
	}
	if totalMS != 55_800 {
		t.Fatalf("timeline total=%dms", totalMS)
	}

	requiredNodes := map[string]string{"brief": "brief", "script": "script", "role_heroine": "role", "role_hero": "role", "role_cousin": "role"}
	for i := 1; i <= 4; i++ {
		requiredNodes[fmt.Sprintf("background_%d", i)] = "background"
		requiredNodes[fmt.Sprintf("video_%d", i)] = "video"
	}
	for _, node := range ancient.Graph.Nodes {
		kind, required := requiredNodes[node.ID]
		if !required {
			continue
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal(node.Config, &config); err != nil {
			t.Fatalf("node %s config: %v", node.ID, err)
		}
		var prompt string
		if raw := config["prompt"]; len(raw) != 0 {
			if err := json.Unmarshal(raw, &prompt); err != nil {
				t.Fatalf("node %s prompt: %v", node.ID, err)
			}
		}
		if kind != "script" {
			for _, keyword := range []string{"二维国风动画", "虚构成年人", "非照片", "非写实", "非真人"} {
				if !strings.Contains(prompt, keyword) {
					t.Fatalf("node %s prompt lacks %q: %s", node.ID, keyword, prompt)
				}
			}
		}
		switch kind {
		case "brief":
			for _, key := range []string{"series_bible", "character_bible", "scene_bible", "episode_route", "storyboard_contract", "seedance_contract"} {
				if len(config[key]) == 0 || string(config[key]) == "null" {
					t.Fatalf("brief config lacks %s: %s", key, node.Config)
				}
			}
			for _, keyword := range []string{"前期材料包", "故事圣经", "人物小传", "场景设定表", "分集剧情路线", "分镜镜头规范", "Seedance 提示词契约"} {
				if !strings.Contains(prompt, keyword) {
					t.Fatalf("brief prompt lacks %q: %s", keyword, prompt)
				}
			}
		case "script":
			for _, keyword := range []string{"严格 JSON", "project", "characters", "locations", "episodes", "scenes", "shots", "seedance_prompt_contract", "0-3秒", "13-15秒"} {
				if !strings.Contains(prompt, keyword) {
					t.Fatalf("script prompt lacks %q: %s", keyword, prompt)
				}
			}
		case "role":
			for _, keyword := range []string{"读取上游人物小传资产", "角色设定参考图", "不是剧情镜头", "全身正面", "侧面", "背面三视图", "脸部大特写", "服装与物料细节"} {
				if !strings.Contains(prompt, keyword) {
					t.Fatalf("character %s prompt lacks %q: %s", node.ID, keyword, prompt)
				}
			}
		case "background":
			for _, keyword := range []string{"读取上游场景设定表", "地点、时间、光影、空间布局、道具和氛围", "9:16单镜头", "非拼贴"} {
				if !strings.Contains(prompt, keyword) {
					t.Fatalf("background %s prompt lacks %q: %s", node.ID, keyword, prompt)
				}
			}
		case "video":
			for _, keyword := range []string{"@图片1为场景背景参考", "@图片2为女主角色参考", "@图片3为男主角色参考", "@图片4为表小姐角色参考", "0-3秒", "4-8秒", "9-12秒", "13-15秒", "台词用", "音效", "禁止：字幕、LOGO、水印"} {
				if !strings.Contains(prompt, keyword) {
					t.Fatalf("video %s prompt lacks %q: %s", node.ID, keyword, prompt)
				}
			}
			if strings.Contains(prompt, "图1=场景背景") {
				t.Fatalf("video %s still uses legacy image mapping: %s", node.ID, prompt)
			}
		}
		delete(requiredNodes, node.ID)
	}
	if len(requiredNodes) != 0 {
		t.Fatalf("unchecked prompt nodes=%v", requiredNodes)
	}
}

func TestGlobalChairmanDramaTemplateThirtySecondWorkflow(t *testing.T) {
	templates, err := BuiltinTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var drama *Template
	for i := range templates {
		if templates[i].Code == "global_chairman_drama_seedance" {
			drama = &templates[i]
			break
		}
	}
	if drama == nil {
		t.Fatal("global chairman template not found")
	}
	if errs := ValidateGraph(drama.Graph, true); len(errs) != 0 {
		t.Fatalf("global chairman template invalid: %v", errs)
	}

	timeline := drama.Graph.Nodes[findNodeIndex(drama.Graph, "timeline")]
	var timelineConfig TimelineConfig
	if err := json.Unmarshal(timeline.Config, &timelineConfig); err != nil {
		t.Fatal(err)
	}
	if len(timelineConfig.Clips) != 4 {
		t.Fatalf("timeline clips=%d", len(timelineConfig.Clips))
	}
	var totalMS int
	for _, clip := range timelineConfig.Clips {
		if clip.TrimInMS != 0 || clip.TrimOutMS != 7_500 {
			t.Fatalf("unexpected clip trim: %+v", clip)
		}
		totalMS += clip.TrimOutMS - clip.TrimInMS
	}
	if totalMS != 30_000 {
		t.Fatalf("timeline total=%dms", totalMS)
	}

	assertInputOrder := func(nodeID string, want []string) {
		t.Helper()
		node := drama.Graph.Nodes[findNodeIndex(drama.Graph, nodeID)]
		if len(node.Inputs) < len(want) {
			t.Fatalf("%s inputs=%+v", nodeID, node.Inputs)
		}
		for i, id := range want {
			if node.Inputs[i].ID != id {
				t.Fatalf("%s input[%d]=%s want %s; inputs=%+v", nodeID, i, node.Inputs[i].ID, id, node.Inputs)
			}
		}
	}
	assertInputOrder("script", []string{"brief", "outline", "character_plan", "world", "director", "hero", "heroine", "cousin", "rival"})
	assertInputOrder("role_hero", []string{"brief", "character_plan"})
	assertInputOrder("background_1", []string{"environment", "world"})
	assertInputOrder("video_1", []string{"scene", "director", "background", "hero", "heroine", "cousin", "rival"})

	configOf := func(nodeID string) map[string]json.RawMessage {
		t.Helper()
		node := drama.Graph.Nodes[findNodeIndex(drama.Graph, nodeID)]
		var config map[string]json.RawMessage
		if err := json.Unmarshal(node.Config, &config); err != nil {
			t.Fatalf("%s config: %v", nodeID, err)
		}
		return config
	}
	promptOf := func(nodeID string) string {
		t.Helper()
		config := configOf(nodeID)
		var prompt string
		if raw := config["prompt"]; len(raw) != 0 {
			if err := json.Unmarshal(raw, &prompt); err != nil {
				t.Fatalf("%s prompt: %v", nodeID, err)
			}
		}
		return prompt
	}

	briefConfig := configOf("brief")
	for _, key := range []string{
		"source_materials", "seedance2_workflow", "creative_direction_candidates", "world_bible",
		"core_competency_matrix", "reference_terms_contract", "creative_audit_contract", "compatibility_check_contract", "seedance_contract",
	} {
		if len(briefConfig[key]) == 0 || string(briefConfig[key]) == "null" {
			t.Fatalf("brief config lacks %s: %s", key, drama.Graph.Nodes[findNodeIndex(drama.Graph, "brief")].Config)
		}
	}
	for _, keyword := range []string{"素材/题材诊断", "创意发散", "运镜匹配", "搭配验证", "创意审核", "不要套模板", "识人术", "纵横术"} {
		prompt := promptOf("brief")
		if !strings.Contains(prompt, keyword) {
			t.Fatalf("brief prompt lacks %q: %s", keyword, prompt)
		}
	}

	for nodeID, keywords := range map[string][]string{
		"asset_outline":    {"大纲资产", "30秒节拍", "四段7.5秒剧情", "识人术", "纵横术"},
		"asset_characters": {"角色生成资产", "谢无咎", "萧明凰", "林晚棠", "亚伦·霍克", "禁止写实真人脸"},
		"asset_world":      {"世界观/剧情背景资产", "纽港", "寰球联合会", "场景1", "背景图只做图生视频首帧/场景参考"},
		"asset_director":   {"Seedance2导演审核资产", "creative_directions", "reference_terms_used", "creative_audit", "不输出平庸 prompt"},
	} {
		prompt := promptOf(nodeID)
		for _, keyword := range keywords {
			if !strings.Contains(prompt, keyword) {
				t.Fatalf("%s prompt lacks %q: %s", nodeID, keyword, prompt)
			}
		}
	}

	scriptPrompt := promptOf("script")
	for _, keyword := range []string{
		"视频创意总监", "不是模板填充器", "严格 JSON", "source_diagnosis", "creative_directions",
		"selected_direction", "creative_audit", "compatibility_check", "reference_terms_used", "seedance_prompt",
		"推镜头", "希区柯克变焦", "交叉蒙太奇", "电影感", "3D 国漫 CG", "冷色调",
		"pass", "米国", "寰球联合会", "禁止文字/字幕/LOGO/水印", "禁止写实真人脸",
	} {
		if !strings.Contains(scriptPrompt, keyword) {
			t.Fatalf("script prompt lacks %q: %s", keyword, scriptPrompt)
		}
	}

	for _, nodeID := range []string{"role_hero", "role_heroine", "role_cousin", "role_rival"} {
		prompt := promptOf(nodeID)
		for _, keyword := range []string{"角色参考/定妆图", "不是剧情镜头", "虚构成年人", "禁止写实真人脸", "真实政治人物"} {
			if !strings.Contains(prompt, keyword) {
				t.Fatalf("%s prompt lacks %q: %s", nodeID, keyword, prompt)
			}
		}
	}
	for _, nodeID := range []string{"background_1", "background_2", "background_3", "background_4"} {
		prompt := promptOf(nodeID)
		for _, keyword := range []string{"图生视频首帧/场景参考图", "无人空镜", "禁止近中景可辨识人物", "禁止文字/字幕/LOGO/水印", "禁止写实真人脸", "非拼贴"} {
			if !strings.Contains(prompt, keyword) {
				t.Fatalf("%s prompt lacks %q: %s", nodeID, keyword, prompt)
			}
		}
	}
	for _, nodeID := range []string{"video_1", "video_2", "video_3", "video_4"} {
		prompt := promptOf(nodeID)
		for _, keyword := range []string{
			"selected_direction", "creative_audit", "reference_terms_used", "compatibility_check", "seedance_prompt",
			"@图片1为场景背景参考", "@图片2为男主谢无咎角色参考", "@图片3为女帝萧明凰角色参考",
			"@图片4为林晚棠角色参考", "@图片5为亚伦·霍克角色参考", "0-7.5秒", "禁止文字/字幕/LOGO/水印",
			"禁止写实真人脸", "不自造镜头词",
		} {
			if !strings.Contains(prompt, keyword) {
				t.Fatalf("%s prompt lacks %q: %s", nodeID, keyword, prompt)
			}
		}
		if strings.Contains(prompt, "图1=场景背景") {
			t.Fatalf("video %s still uses legacy image mapping: %s", nodeID, prompt)
		}
	}
	for _, keyword := range []string{"希区柯克变焦", "环绕拍摄", "真实底牌"} {
		prompt := promptOf("video_3")
		if !strings.Contains(prompt, keyword) {
			t.Fatalf("video_3 prompt lacks %q: %s", keyword, prompt)
		}
	}
}

func TestValidateGraphV2TimelineTrimAndResolution(t *testing.T) {
	graph := blankVideoCanvasTemplate().Graph
	timeline := findNodeIndex(graph, "timeline")
	var config TimelineConfig
	if err := json.Unmarshal(graph.Nodes[timeline].Config, &config); err != nil {
		t.Fatal(err)
	}
	config.Clips[0].TrimInMS = 100
	config.Clips[0].TrimOutMS = 1099
	graph.Nodes[timeline].Config, _ = json.Marshal(config)
	assertValidationCode(t, ValidateGraph(graph, true), ValidationInvalidTimeline)

	config.Clips[0].TrimOutMS = 1100
	graph.Nodes[timeline].Config, _ = json.Marshal(config)
	if errs := ValidateGraph(graph, true); len(errs) != 0 {
		t.Fatalf("valid 100ms trim rejected: %v", errs)
	}
	graph.Settings.Resolution = "4k"
	assertValidationCode(t, ValidateGraph(graph, true), ValidationInvalidResolution)
}

func TestValidateGraphV2LimitsAndSingleValuePorts(t *testing.T) {
	graph := blankVideoCanvasTemplate().Graph
	for len(graph.Nodes) <= MaxGraphNodes {
		graph.Nodes = append(graph.Nodes, Node{ID: "extra_" + string(rune(len(graph.Nodes)+100)), Type: NodeStoryBrief})
	}
	assertValidationCode(t, ValidateGraph(graph, false), ValidationGraphNodeLimit)

	graph = blankVideoCanvasTemplate().Graph
	duplicate := graph.Edges[0]
	duplicate.ID = "duplicate-input"
	graph.Edges = append(graph.Edges, duplicate)
	assertValidationCode(t, ValidateGraph(graph, false), ValidationMultipleInputEdges)
}

func TestValidateGraphV2RequiresFixedSystemNodesInDrafts(t *testing.T) {
	graph := blankVideoCanvasTemplate().Graph
	graph.Nodes = append([]Node(nil), graph.Nodes[:len(graph.Nodes)-1]...)
	assertValidationCode(t, ValidateGraph(graph, false), ValidationSystemNodeCount)

	graph = blankVideoCanvasTemplate().Graph
	for index, node := range graph.Nodes {
		if node.Type == NodeTimeline {
			graph.Nodes = append(graph.Nodes[:index], graph.Nodes[index+1:]...)
			break
		}
	}
	assertValidationCode(t, ValidateGraph(graph, false), ValidationSystemNodeCount)
}

func TestValidateGraphV2MultipleTimelinePort(t *testing.T) {
	settings := Settings{AspectRatio: AspectRatioPortrait, Resolution: Resolution720p, FPS: DefaultFPS, SceneDurationMS: SceneDurationMS,
		CharacterApprovalPolicy: ApprovalManual, StoryboardApprovalPolicy: ApprovalManual}
	graph := Graph{SchemaVersion: SchemaVersionV2, Settings: settings}
	clips := make([]TimelineClip, 0, 4)
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("video_%d", i)
		graph.Nodes = append(graph.Nodes, Node{ID: id, Type: NodeVideo, Outputs: []Port{{ID: "video", Type: PortVideo}}})
		graph.Edges = append(graph.Edges, Edge{ID: fmt.Sprintf("edge_%d", i), Source: id, SourcePort: "video", Target: "timeline", TargetPort: "clip"})
		clips = append(clips, TimelineClip{ID: fmt.Sprintf("clip_%d", i), SourceNodeID: id, SourcePort: "video", TrimOutMS: SceneDurationMS})
	}
	config, _ := json.Marshal(TimelineConfig{Clips: clips})
	graph.Nodes = append(graph.Nodes, Node{ID: "timeline", Type: NodeTimeline, Config: config, Inputs: []Port{{ID: "clip", Label: "视频片段", Type: PortVideo, Required: true, Multiple: true}}})
	if errs := ValidateGraph(graph, false); errs.Has(ValidationMultipleInputEdges) {
		t.Fatalf("multiple timeline port rejected: %v", errs)
	}
	graph.Nodes[len(graph.Nodes)-1].Inputs[0].Multiple = false
	assertValidationCode(t, ValidateGraph(graph, false), ValidationMultipleInputEdges)
}

func TestSemanticGraphHashIgnoresLayout(t *testing.T) {
	graph := blankVideoCanvasTemplate().Graph
	want, err := SemanticGraphHash(graph)
	if err != nil {
		t.Fatal(err)
	}
	graph.Nodes[0].Position.X += 900
	graph.Nodes[0].PositionMode = PositionManual
	graph.Nodes[0].Collapsed = true
	graph.Edges[0].Curve = &Position{X: 120, Y: -80}
	graph.Edges[0].Route = []Position{{X: 240, Y: 80}, {X: 320, Y: 80}}
	graph.Layout = &GraphLayout{SharedCharacterBus: &LayoutAnchor{Position: Position{X: 600, Y: 300}, PositionMode: PositionManual}}
	got, err := SemanticGraphHash(graph)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("layout changed semantic hash: %s != %s", got, want)
	}
	graph.Settings.Resolution = Resolution1080p
	changed, _ := SemanticGraphHash(graph)
	if changed == want {
		t.Fatal("resolution did not change semantic hash")
	}
}

func TestUpgradeGraphV1ToV2(t *testing.T) {
	legacy := MustBuiltinTemplateV1().Graph
	upgraded, err := UpgradeGraphV1ToV2(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.SchemaVersion != SchemaVersionV2 || upgraded.Settings.Resolution != Resolution720p || upgraded.Settings.CharacterApprovalPolicy != ApprovalManual {
		t.Fatalf("upgraded settings=%+v", upgraded.Settings)
	}
	for _, node := range upgraded.Nodes {
		if node.PositionMode != PositionAuto {
			t.Fatalf("node %s position mode=%q", node.ID, node.PositionMode)
		}
	}
	timeline := upgraded.Nodes[findNodeIndex(upgraded, "timeline")]
	var config TimelineConfig
	if json.Unmarshal(timeline.Config, &config) != nil || len(config.Clips) != 4 || len(config.ClipNodeIDs) != 0 || config.Clips[0].TrimOutMS != SceneDurationMS {
		t.Fatalf("upgraded timeline=%s", timeline.Config)
	}
	if legacy.SchemaVersion != SchemaVersionV1 {
		t.Fatal("upgrade mutated source graph")
	}
}

func findNodeIndex(graph Graph, id string) int {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}
