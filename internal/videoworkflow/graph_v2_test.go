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
	if len(templates) != 2 {
		t.Fatalf("template count=%d, want 2", len(templates))
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
	if len(ancient.Graph.Nodes) != 19 || ancient.Version != 3 || ancient.Graph.Settings.Resolution != Resolution1080p || ancient.Graph.Settings.VideoModel != "wan2.7-r2v" ||
		!strings.Contains(ancient.Name, "Wan2.7") || !strings.Contains(ancient.Description, "Wan2.7") {
		t.Fatalf("unexpected ancient template: nodes=%d settings=%+v", len(ancient.Graph.Nodes), ancient.Graph.Settings)
	}
	blank := byCode["blank_video_canvas"]
	if len(blank.Graph.Nodes) != 6 || blank.Graph.Settings.Resolution != Resolution720p || blank.Graph.Settings.VideoModel != "wan2.7-r2v" {
		t.Fatalf("unexpected blank template: nodes=%d settings=%+v", len(blank.Graph.Nodes), blank.Graph.Settings)
	}
}

func TestAncientDramaV3SafetyStyleTimelineAndWanReferences(t *testing.T) {
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
	if ancient == nil || ancient.Version != 3 || ancient.Graph.Settings.VideoModel != "wan2.7-r2v" {
		t.Fatalf("ancient v3 template=%+v", ancient)
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

	requiredNodes := map[string]bool{"brief": false, "role_heroine": false, "role_hero": false, "role_cousin": false}
	for i := 1; i <= 4; i++ {
		requiredNodes[fmt.Sprintf("background_%d", i)] = true
		requiredNodes[fmt.Sprintf("video_%d", i)] = false
	}
	for _, node := range ancient.Graph.Nodes {
		background, required := requiredNodes[node.ID]
		if !required {
			continue
		}
		var config struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(node.Config, &config); err != nil {
			t.Fatalf("node %s config: %v", node.ID, err)
		}
		for _, keyword := range []string{"二维国风动画", "虚构成年人", "非照片", "非写实", "非真人"} {
			if !strings.Contains(config.Prompt, keyword) {
				t.Fatalf("node %s prompt lacks %q: %s", node.ID, keyword, config.Prompt)
			}
		}
		if background && (!strings.Contains(config.Prompt, "9:16单镜头") || !strings.Contains(config.Prompt, "非拼贴")) {
			t.Fatalf("background %s prompt=%s", node.ID, config.Prompt)
		}
		if node.Type == NodeVideo && !strings.Contains(config.Prompt, "图1=场景背景，图2=女主，图3=男主，图4=表小姐") {
			t.Fatalf("video %s prompt lacks Wan reference mapping: %s", node.ID, config.Prompt)
		}
		delete(requiredNodes, node.ID)
	}
	if len(requiredNodes) != 0 {
		t.Fatalf("unchecked prompt nodes=%v", requiredNodes)
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
	graph.Nodes[0].Collapsed = true
	graph.Edges[0].Curve = &Position{X: 120, Y: -80}
	graph.Edges[0].Route = []Position{{X: 240, Y: 80}, {X: 320, Y: 80}}
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
