package videoworkflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ValidationCode string

const (
	ValidationDuplicateNodeID       ValidationCode = "duplicate_node_id"
	ValidationDuplicateEdgeID       ValidationCode = "duplicate_edge_id"
	ValidationDuplicateGroupID      ValidationCode = "duplicate_group_id"
	ValidationNodeNotFound          ValidationCode = "node_not_found"
	ValidationPortNotFound          ValidationCode = "port_not_found"
	ValidationPortTypeMismatch      ValidationCode = "port_type_mismatch"
	ValidationRequiredInputMissing  ValidationCode = "required_input_missing"
	ValidationGraphCycle            ValidationCode = "graph_cycle"
	ValidationCharacterLimit        ValidationCode = "character_limit_exceeded"
	ValidationSceneLimit            ValidationCode = "scene_limit_exceeded"
	ValidationSceneDuration         ValidationCode = "invalid_scene_duration"
	ValidationTimelineSceneDisabled ValidationCode = "timeline_scene_disabled"
	ValidationInvalidAspectRatio    ValidationCode = "invalid_aspect_ratio"
	ValidationInvalidResolution     ValidationCode = "invalid_resolution"
	ValidationInvalidFPS            ValidationCode = "invalid_fps"
	ValidationInvalidSchemaVersion  ValidationCode = "invalid_schema_version"
	ValidationGraphNodeLimit        ValidationCode = "node_limit_exceeded"
	ValidationGraphEdgeLimit        ValidationCode = "edge_limit_exceeded"
	ValidationMultipleInputEdges    ValidationCode = "multiple_input_edges"
	ValidationInvalidTimeline       ValidationCode = "invalid_timeline"
	ValidationInvalidApprovalPolicy ValidationCode = "invalid_approval_policy"
	ValidationSystemNodeCount       ValidationCode = "invalid_system_node_count"
	ValidationInvalidImageTransform ValidationCode = "invalid_image_transform"
	ValidationInvalidAssetBinding   ValidationCode = "invalid_asset_binding"
)

type ValidationError struct {
	Code    ValidationCode `json:"code"`
	NodeID  string         `json:"node_id,omitempty"`
	EdgeID  string         `json:"edge_id,omitempty"`
	GroupID string         `json:"group_id,omitempty"`
	Message string         `json:"message"`
}

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %s", e[0].Code, e[0].Message)
}

func (e ValidationErrors) Has(code ValidationCode) bool {
	for _, item := range e {
		if item.Code == code {
			return true
		}
	}
	return false
}

func ValidateGraph(g Graph, requireComplete bool) ValidationErrors {
	errs := make(ValidationErrors, 0)
	if g.SchemaVersion != SchemaVersionV1 && g.SchemaVersion != SchemaVersionV2 {
		errs = append(errs, ValidationError{Code: ValidationInvalidSchemaVersion, Message: "图版本仅支持 1 或 2"})
	}
	if len(g.Nodes) > MaxGraphNodes {
		errs = append(errs, ValidationError{Code: ValidationGraphNodeLimit, Message: "节点不能超过 64 个"})
	}
	if len(g.Edges) > MaxGraphEdges {
		errs = append(errs, ValidationError{Code: ValidationGraphEdgeLimit, Message: "连线不能超过 128 条"})
	}
	nodes := make(map[string]Node, len(g.Nodes))
	characters := 0
	scenes := 0
	timelineCount := 0
	composeCount := 0
	for _, node := range g.Nodes {
		if _, exists := nodes[node.ID]; exists || node.ID == "" {
			errs = append(errs, ValidationError{Code: ValidationDuplicateNodeID, NodeID: node.ID, Message: "节点 ID 必须唯一且非空"})
			continue
		}
		nodes[node.ID] = node
		switch node.Type {
		case NodeCharacter:
			characters++
		case NodeScene:
			scenes++
			if node.DurationSeconds != SceneDuration {
				errs = append(errs, ValidationError{Code: ValidationSceneDuration, NodeID: node.ID, Message: "场景时长必须为 15 秒"})
			}
		case NodeTimeline:
			timelineCount++
		case NodeCompose:
			composeCount++
		}
		if node.Type == NodeCharacter || node.Type == NodeBackground {
			assetID, versionID := nodeBoundAsset(node)
			if (assetID == "") != (versionID == "") {
				errs = append(errs, ValidationError{Code: ValidationInvalidAssetBinding, NodeID: node.ID, Message: "绑定素材必须同时包含 asset_id 和 asset_version_id"})
			}
			var config struct {
				ImageTransform *ImageTransform `json:"image_transform"`
			}
			if json.Unmarshal(node.Config, &config) == nil && config.ImageTransform != nil {
				if err := validateImageTransform(*config.ImageTransform); err != nil {
					errs = append(errs, ValidationError{Code: ValidationInvalidImageTransform, NodeID: node.ID, Message: err.Error()})
				}
			}
		}
	}
	if characters > MaxCharacters {
		errs = append(errs, ValidationError{Code: ValidationCharacterLimit, Message: "人物节点不能超过 4 个"})
	}
	if scenes > MaxScenes {
		errs = append(errs, ValidationError{Code: ValidationSceneLimit, Message: "场景节点不能超过 4 个"})
	}

	if (requireComplete || g.Settings.EffectiveSceneDurationMS() != 0) && g.Settings.EffectiveSceneDurationMS() != SceneDurationMS {
		errs = append(errs, ValidationError{Code: ValidationSceneDuration, Message: "全局场景时长必须为 15 秒"})
	}
	if (requireComplete || g.Settings.FPS != 0) && g.Settings.FPS != DefaultFPS {
		errs = append(errs, ValidationError{Code: ValidationInvalidFPS, Message: "输出帧率必须为 30fps"})
	}
	if (requireComplete || g.Settings.AspectRatio != "") && g.Settings.AspectRatio != AspectRatioPortrait && g.Settings.AspectRatio != AspectRatioLandscape && g.Settings.AspectRatio != AspectRatioSquare {
		errs = append(errs, ValidationError{Code: ValidationInvalidAspectRatio, Message: "画幅仅支持 9:16、16:9 或 1:1"})
	}
	resolution := g.Settings.EffectiveResolution()
	if (requireComplete || g.Settings.Resolution != "") && resolution != Resolution720p && resolution != Resolution1080p {
		errs = append(errs, ValidationError{Code: ValidationInvalidResolution, Message: "分辨率仅支持 720p 或 1080p"})
	}
	if g.SchemaVersion == SchemaVersionV2 {
		if !validCharacterApprovalPolicy(g.Settings.CharacterApprovalPolicy) {
			errs = append(errs, ValidationError{Code: ValidationInvalidApprovalPolicy, Message: "角色审批策略仅支持 manual 或 auto_first"})
		}
		if !validStoryboardApprovalPolicy(g.Settings.StoryboardApprovalPolicy) {
			errs = append(errs, ValidationError{Code: ValidationInvalidApprovalPolicy, Message: "分镜审批策略仅支持 manual 或 auto"})
		}
	}
	if (requireComplete || g.SchemaVersion == SchemaVersionV2) && (timelineCount != 1 || composeCount != 1) {
		errs = append(errs, ValidationError{Code: ValidationSystemNodeCount, Message: "工作流必须且只能包含一个时间线节点和一个成片节点"})
	} else if timelineCount > 1 || composeCount > 1 {
		errs = append(errs, ValidationError{Code: ValidationSystemNodeCount, Message: "时间线节点和成片节点各只能有一个"})
	}

	edgeIDs := make(map[string]struct{}, len(g.Edges))
	incoming := make(map[string]map[string]bool, len(g.Nodes))
	adj := make(map[string][]string, len(g.Nodes))
	indegree := make(map[string]int, len(g.Nodes))
	for id := range nodes {
		indegree[id] = 0
	}
	for _, edge := range g.Edges {
		if _, exists := edgeIDs[edge.ID]; exists || edge.ID == "" {
			errs = append(errs, ValidationError{Code: ValidationDuplicateEdgeID, EdgeID: edge.ID, Message: "连线 ID 必须唯一且非空"})
			continue
		}
		edgeIDs[edge.ID] = struct{}{}
		source, sourceOK := nodes[edge.Source]
		target, targetOK := nodes[edge.Target]
		if !sourceOK || !targetOK {
			errs = append(errs, ValidationError{Code: ValidationNodeNotFound, EdgeID: edge.ID, Message: "连线引用了不存在的节点"})
			continue
		}
		sourcePort, sourcePortOK := findPort(source.Outputs, edge.SourcePort)
		targetPort, targetPortOK := findPort(target.Inputs, edge.TargetPort)
		if !sourcePortOK || !targetPortOK {
			errs = append(errs, ValidationError{Code: ValidationPortNotFound, EdgeID: edge.ID, Message: "连线引用了不存在的端口"})
			continue
		}
		if sourcePort.Type != targetPort.Type {
			errs = append(errs, ValidationError{Code: ValidationPortTypeMismatch, EdgeID: edge.ID, Message: "连线两端口类型不一致"})
			continue
		}
		if incoming[target.ID] == nil {
			incoming[target.ID] = make(map[string]bool)
		}
		if incoming[target.ID][targetPort.ID] && !targetPort.Multiple {
			errs = append(errs, ValidationError{Code: ValidationMultipleInputEdges, EdgeID: edge.ID, NodeID: target.ID, Message: "单值输入端口只能连接一条边: " + targetPort.ID})
			continue
		}
		incoming[target.ID][targetPort.ID] = true
		adj[source.ID] = append(adj[source.ID], target.ID)
		indegree[target.ID]++
	}
	if requireComplete {
		for _, node := range g.Nodes {
			for _, port := range node.Inputs {
				if port.Required && !incoming[node.ID][port.ID] {
					errs = append(errs, ValidationError{Code: ValidationRequiredInputMissing, NodeID: node.ID, Message: "必需输入端口未连接: " + port.ID})
				}
			}
		}
	}
	if hasCycle(indegree, adj) {
		errs = append(errs, ValidationError{Code: ValidationGraphCycle, Message: "工作流图不能包含循环依赖"})
	}

	groupIDs := make(map[string]struct{}, len(g.Groups))
	sceneGroups := 0
	for _, group := range g.Groups {
		if _, exists := groupIDs[group.ID]; exists || group.ID == "" {
			errs = append(errs, ValidationError{Code: ValidationDuplicateGroupID, GroupID: group.ID, Message: "分组 ID 必须唯一且非空"})
			continue
		}
		groupIDs[group.ID] = struct{}{}
		if group.Type == "scene" {
			sceneGroups++
			if group.DurationSeconds != SceneDuration {
				errs = append(errs, ValidationError{Code: ValidationSceneDuration, GroupID: group.ID, Message: "场景分组时长必须为 15 秒"})
			}
		}
	}
	if sceneGroups > MaxScenes {
		errs = append(errs, ValidationError{Code: ValidationSceneLimit, Message: "场景分组不能超过 4 个"})
	}
	for _, node := range g.Nodes {
		if node.Type != NodeTimeline {
			continue
		}
		var cfg TimelineConfig
		if len(node.Config) == 0 {
			if requireComplete {
				errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线配置不能为空"})
			}
			continue
		}
		if unmarshalConfig(node.Config, &cfg) != nil {
			errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线配置必须是合法 JSON"})
			continue
		}
		connectedSources := make(map[string]bool)
		for _, edge := range g.Edges {
			if edge.Target == node.ID {
				connectedSources[edge.Source+"\x00"+edge.SourcePort] = true
			}
		}
		clips := normalizedTimelineClips(cfg)
		if len(clips) > MaxScenes || (requireComplete && len(clips) == 0) {
			errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线必须包含 1–4 个片段"})
		}
		clipIDs := make(map[string]struct{}, len(clips))
		for _, timelineClip := range clips {
			clipID := timelineClip.SourceNodeID
			if timelineClip.ID == "" {
				errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线片段 ID 不能为空"})
			}
			if _, exists := clipIDs[timelineClip.ID]; exists {
				errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线片段 ID 必须唯一: " + timelineClip.ID})
			}
			clipIDs[timelineClip.ID] = struct{}{}
			clip, ok := nodes[clipID]
			if !ok {
				errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线引用了不存在的节点: " + clipID})
				continue
			}
			if clip.Type != NodeVideo {
				errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线只能引用视频节点: " + clipID})
			}
			if requireComplete && !connectedSources[timelineClip.SourceNodeID+"\x00"+timelineClip.SourcePort] {
				errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线片段未连接对应视频输出: " + timelineClip.ID})
			}
			if g.SchemaVersion == SchemaVersionV2 {
				if timelineClip.SourcePort == "" {
					errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线片段必须指定输出端口"})
				} else if port, ok := findPort(clip.Outputs, timelineClip.SourcePort); !ok || port.Type != PortVideo {
					errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: "时间线片段输出端口必须为 video 类型"})
				}
				if err := validateTimelineTrim(timelineClip); err != nil {
					errs = append(errs, ValidationError{Code: ValidationInvalidTimeline, NodeID: node.ID, Message: err.Error()})
				}
			}
		}
	}
	return errs
}

func normalizedTimelineClips(cfg TimelineConfig) []TimelineClip {
	if len(cfg.Clips) > 0 {
		return cfg.Clips
	}
	out := make([]TimelineClip, 0, len(cfg.ClipNodeIDs))
	for i, nodeID := range cfg.ClipNodeIDs {
		out = append(out, TimelineClip{ID: fmt.Sprintf("clip_%d", i+1), SourceNodeID: nodeID, SourcePort: "video", TrimOutMS: SceneDurationMS})
	}
	return out
}

func validateTimelineTrim(clip TimelineClip) error {
	if clip.TrimInMS < 0 || clip.TrimOutMS > SceneDurationMS || clip.TrimOutMS <= clip.TrimInMS {
		return fmt.Errorf("片段 %s 裁剪范围必须在 0–15000ms 内", clip.ID)
	}
	if clip.TrimInMS%TimelineStepMS != 0 || clip.TrimOutMS%TimelineStepMS != 0 {
		return fmt.Errorf("片段 %s 裁剪必须按 100ms 调整", clip.ID)
	}
	if clip.TrimOutMS-clip.TrimInMS < MinClipMS {
		return fmt.Errorf("片段 %s 裁剪后不能短于 1000ms", clip.ID)
	}
	return nil
}

func validCharacterApprovalPolicy(policy ApprovalPolicy) bool {
	return policy == ApprovalManual || policy == ApprovalAutoFirst
}

func validStoryboardApprovalPolicy(policy ApprovalPolicy) bool {
	return policy == ApprovalManual || policy == ApprovalAuto
}

func findPort(ports []Port, id string) (Port, bool) {
	for _, port := range ports {
		if port.ID == id {
			return port, true
		}
	}
	return Port{}, false
}

const (
	backgroundEnvironmentPortID   = "environment"
	backgroundLegacyScenePortID   = "scene"
	backgroundEnvironmentPortLabel = "环境（地点/灯光/静物）"
)

// NormalizeBackgroundEnvironmentPorts 将背景节点输入口 scene 归一为 environment，
// 并把指向该口的边 TargetPort 一并改写。已规范图可安全重复调用。
func NormalizeBackgroundEnvironmentPorts(g *Graph) {
	if g == nil {
		return
	}
	backgroundIDs := make(map[string]struct{})
	for i := range g.Nodes {
		node := &g.Nodes[i]
		if node.Type != NodeBackground {
			continue
		}
		backgroundIDs[node.ID] = struct{}{}
		hasEnvironment := false
		inputs := make([]Port, 0, len(node.Inputs))
		for _, port := range node.Inputs {
			if port.ID == backgroundLegacyScenePortID {
				port.ID = backgroundEnvironmentPortID
				if strings.TrimSpace(port.Label) == "" || port.Label == "场景描述" {
					port.Label = backgroundEnvironmentPortLabel
				}
			}
			if port.ID == backgroundEnvironmentPortID {
				if hasEnvironment {
					continue
				}
				hasEnvironment = true
				if strings.TrimSpace(port.Label) == "" {
					port.Label = backgroundEnvironmentPortLabel
				}
			}
			inputs = append(inputs, port)
		}
		if !hasEnvironment {
			inputs = append(inputs, Port{
				ID: backgroundEnvironmentPortID, Label: backgroundEnvironmentPortLabel,
				Type: PortScene, Required: true,
			})
		}
		node.Inputs = inputs
	}
	for i := range g.Edges {
		edge := &g.Edges[i]
		if _, ok := backgroundIDs[edge.Target]; !ok {
			continue
		}
		if edge.TargetPort == backgroundLegacyScenePortID {
			edge.TargetPort = backgroundEnvironmentPortID
		}
	}
}

func hasCycle(indegree map[string]int, adj map[string][]string) bool {
	queue := make([]string, 0, len(indegree))
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adj[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	return visited != len(indegree)
}

func unmarshalConfig(raw []byte, target any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}
