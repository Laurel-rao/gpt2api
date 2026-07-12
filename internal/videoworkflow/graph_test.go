package videoworkflow

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestValidateGraph_DuplicateNodeID(t *testing.T) {
	g := testGraph()
	g.Nodes = append(g.Nodes, g.Nodes[0])
	assertValidationCode(t, ValidateGraph(g, false), ValidationDuplicateNodeID)
}

func TestValidateGraph_Cycle(t *testing.T) {
	g := testGraph()
	g.Nodes[0].Inputs = []Port{{ID: "in", Type: PortText}}
	g.Nodes[1].Outputs = []Port{{ID: "out", Type: PortText}}
	g.Edges = append(g.Edges, Edge{ID: "back", Source: "b", SourcePort: "out", Target: "a", TargetPort: "in"})
	assertValidationCode(t, ValidateGraph(g, false), ValidationGraphCycle)
}

func TestValidateGraph_PortMismatch(t *testing.T) {
	g := testGraph()
	g.Nodes[1].Inputs[0].Type = PortImage
	assertValidationCode(t, ValidateGraph(g, false), ValidationPortTypeMismatch)
}

func TestValidateGraph_BrokenReferencesAndDuplicateIDs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Graph)
		code ValidationCode
	}{
		{"missing node", func(g *Graph) { g.Edges[0].Target = "missing" }, ValidationNodeNotFound},
		{"missing port", func(g *Graph) { g.Edges[0].TargetPort = "missing" }, ValidationPortNotFound},
		{"duplicate edge", func(g *Graph) { g.Edges = append(g.Edges, g.Edges[0]) }, ValidationDuplicateEdgeID},
		{"duplicate group", func(g *Graph) {
			group := Group{ID: "group", Type: "scene", SceneID: "scene", DurationSeconds: SceneDuration}
			g.Groups = []Group{group, group}
		}, ValidationDuplicateGroupID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := testGraph()
			test.edit(&g)
			assertValidationCode(t, ValidateGraph(g, false), test.code)
		})
	}
}

func TestValidateGraph_Limits(t *testing.T) {
	t.Run("characters", func(t *testing.T) {
		g := validSettingsGraph()
		for i := 0; i < 5; i++ {
			g.Nodes = append(g.Nodes, Node{ID: string(rune('a' + i)), Type: NodeCharacter})
		}
		assertValidationCode(t, ValidateGraph(g, false), ValidationCharacterLimit)
	})
	t.Run("scenes", func(t *testing.T) {
		g := validSettingsGraph()
		for i := 0; i < 5; i++ {
			g.Nodes = append(g.Nodes, Node{ID: string(rune('a' + i)), Type: NodeScene, DurationSeconds: SceneDuration})
		}
		assertValidationCode(t, ValidateGraph(g, false), ValidationSceneLimit)
	})
	t.Run("duration", func(t *testing.T) {
		g := validSettingsGraph()
		g.Nodes = []Node{{ID: "scene", Type: NodeScene, DurationSeconds: 14}}
		assertValidationCode(t, ValidateGraph(g, false), ValidationSceneDuration)
	})
}

func TestValidateGraph_DisabledScene(t *testing.T) {
	config, _ := json.Marshal(TimelineConfig{ClipNodeIDs: []string{"video"}})
	g := validSettingsGraph()
	g.Nodes = []Node{
		{ID: "video", Type: NodeVideo, SceneID: "scene_1"},
		{ID: "timeline", Type: NodeTimeline, Config: config},
	}
	g.Groups = []Group{{ID: "g1", Type: "scene", SceneID: "scene_1", DurationSeconds: SceneDuration, Enabled: false, NodeIDs: []string{"video"}}}
	if errs := ValidateGraph(g, false); errs.Has(ValidationTimelineSceneDisabled) {
		t.Fatalf("disabled scene should be skipped at runtime: %v", errs)
	}
}

func TestValidateGraph_DraftMayBeIncomplete(t *testing.T) {
	g := testGraph()
	g.Edges = nil
	if errs := ValidateGraph(g, false); errs.Has(ValidationRequiredInputMissing) {
		t.Fatalf("draft unexpectedly requires inputs: %v", errs)
	}
	assertValidationCode(t, ValidateGraph(g, true), ValidationRequiredInputMissing)
}

func TestValidateGraph_CompleteRequiresOutputSettings(t *testing.T) {
	g := testGraph()
	g.Settings = Settings{}
	errs := ValidateGraph(g, true)
	assertValidationCode(t, errs, ValidationInvalidAspectRatio)
	assertValidationCode(t, errs, ValidationInvalidFPS)
	assertValidationCode(t, errs, ValidationSceneDuration)
}

func TestValidateGraph_SceneGroupLimit(t *testing.T) {
	g := validSettingsGraph()
	for i := 0; i < 5; i++ {
		g.Groups = append(g.Groups, Group{ID: string(rune('a' + i)), Type: "scene", SceneID: string(rune('a' + i)), Enabled: true, DurationSeconds: SceneDuration})
	}
	assertValidationCode(t, ValidateGraph(g, false), ValidationSceneLimit)
}

func TestBuiltinTemplateV1_CompleteAndCloned(t *testing.T) {
	template, err := BuiltinTemplateV1()
	if err != nil {
		t.Fatal(err)
	}
	if errs := ValidateGraph(template.Graph, true); len(errs) != 0 {
		t.Fatalf("embedded template is invalid: %v", errs)
	}
	if template.Version != 1 || len(template.Graph.Nodes) != 19 || len(template.Graph.Groups) != 4 {
		t.Fatalf("unexpected template shape: version=%d nodes=%d groups=%d", template.Version, len(template.Graph.Nodes), len(template.Graph.Groups))
	}
	clone, err := CloneGraph(template.Graph)
	if err != nil {
		t.Fatal(err)
	}
	clone.Nodes[0].ID = "changed"
	if template.Graph.Nodes[0].ID == clone.Nodes[0].ID {
		t.Fatal("clone shares node storage with source")
	}
}

func TestInvalidation_RoleChanged(t *testing.T) {
	g := MustBuiltinTemplateV1().Graph
	got := InvalidatedNodeIDs(g, "role_hero", ChangeRoleSelection)
	want := []string{"background_1", "background_2", "background_3", "background_4", "compose", "scene_1", "scene_2", "scene_3", "scene_4", "script", "timeline", "video_1", "video_2", "video_3", "video_4"}
	if !slices.Equal(got, want) {
		t.Fatalf("invalidated nodes = %v, want %v", got, want)
	}
}

func TestInvalidation_BackgroundChanged(t *testing.T) {
	g := MustBuiltinTemplateV1().Graph
	want := []string{"compose", "timeline", "video_2"}
	if got := InvalidatedNodeIDs(g, "background_2", ChangeBackground); !slices.Equal(got, want) {
		t.Fatalf("invalidated nodes = %v, want %v", got, want)
	}
}

func TestInvalidation_TimelineReordered(t *testing.T) {
	g := MustBuiltinTemplateV1().Graph
	if got := InvalidatedNodeIDs(g, "timeline", ChangeTimelineOrder); !slices.Equal(got, []string{"compose"}) {
		t.Fatalf("invalidated nodes = %v", got)
	}
	if got := InvalidatedNodeIDs(g, "timeline", ChangeLayout); len(got) != 0 {
		t.Fatalf("layout invalidated nodes = %v", got)
	}
}

func testGraph() Graph {
	g := validSettingsGraph()
	g.Nodes = []Node{
		{ID: "a", Type: NodeStoryBrief, Outputs: []Port{{ID: "out", Type: PortText}}},
		{ID: "b", Type: NodeCharacter, Inputs: []Port{{ID: "in", Type: PortText, Required: true}}},
	}
	g.Edges = []Edge{{ID: "edge", Source: "a", SourcePort: "out", Target: "b", TargetPort: "in"}}
	return g
}

func validSettingsGraph() Graph {
	return Graph{SchemaVersion: 1, Settings: Settings{AspectRatio: AspectRatioPortrait, FPS: DefaultFPS, SceneDurationSeconds: SceneDuration}}
}

func assertValidationCode(t *testing.T, errs ValidationErrors, code ValidationCode) {
	t.Helper()
	if !errs.Has(code) {
		t.Fatalf("validation errors %v do not contain %s", errs, code)
	}
}
