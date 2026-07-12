package videoworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/432539/gpt2api/internal/videogen"
)

type AssetVersionRef struct {
	NodeID    string `json:"node_id"`
	VersionID string `json:"version_id"`
}

type semanticNode struct {
	Type            NodeType        `json:"type"`
	Version         int             `json:"version"`
	RoleID          string          `json:"role_id,omitempty"`
	SceneID         string          `json:"scene_id,omitempty"`
	AssetID         string          `json:"asset_id,omitempty"`
	AssetVersionID  string          `json:"asset_version_id,omitempty"`
	DurationSeconds int             `json:"duration_seconds,omitempty"`
	Config          json.RawMessage `json:"config,omitempty"`
	Inputs          []Port          `json:"inputs,omitempty"`
	Outputs         []Port          `json:"outputs,omitempty"`
}

type semanticSettings struct {
	AspectRatio              AspectRatio    `json:"aspect_ratio"`
	Resolution               Resolution     `json:"resolution"`
	FPS                      int            `json:"fps"`
	SceneDurationMS          int            `json:"scene_duration_ms"`
	CharacterApprovalPolicy  ApprovalPolicy `json:"character_approval_policy,omitempty"`
	StoryboardApprovalPolicy ApprovalPolicy `json:"storyboard_approval_policy,omitempty"`
	TextModel                string         `json:"text_model,omitempty"`
	ImageModel               string         `json:"image_model,omitempty"`
	VideoModel               string         `json:"video_model,omitempty"`
}

func InputHash(g Graph, nodeID string, upstream []AssetVersionRef) (string, error) {
	return inputHash(g, nodeID, upstream, nil)
}

// InputHashWithProviderSnapshot 将视频渠道的非敏感执行身份纳入缓存键。
func InputHashWithProviderSnapshot(g Graph, nodeID string, upstream []AssetVersionRef, providerSnapshot json.RawMessage) (string, error) {
	normalized, err := normalizeVideoProviderSnapshot(providerSnapshot)
	if err != nil {
		return "", err
	}
	return inputHash(g, nodeID, upstream, normalized)
}

func inputHash(g Graph, nodeID string, upstream []AssetVersionRef, providerSnapshot json.RawMessage) (string, error) {
	var node *Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == nodeID {
			node = &g.Nodes[i]
			break
		}
	}
	if node == nil {
		return "", fmt.Errorf("%w: node %s", ErrNotFound, nodeID)
	}
	payload := struct {
		SchemaVersion    int               `json:"schema_version"`
		Node             semanticNode      `json:"node"`
		Settings         semanticSettings  `json:"settings"`
		Upstream         []AssetVersionRef `json:"upstream"`
		ProviderSnapshot json.RawMessage   `json:"video_provider,omitempty"`
	}{
		SchemaVersion: g.SchemaVersion,
		Node: semanticNode{
			Type: node.Type, Version: semanticNodeVersion(*node), RoleID: node.RoleID, SceneID: node.SceneID, AssetID: node.AssetID, AssetVersionID: node.AssetVersionID,
			DurationSeconds: node.DurationSeconds, Config: normalizeJSON(node.Config),
			Inputs: node.Inputs, Outputs: node.Outputs,
		},
		Settings: semanticSettings{
			AspectRatio: g.Settings.AspectRatio, Resolution: g.Settings.EffectiveResolution(), FPS: g.Settings.FPS,
			SceneDurationMS: g.Settings.EffectiveSceneDurationMS(), CharacterApprovalPolicy: g.Settings.CharacterApprovalPolicy,
			StoryboardApprovalPolicy: g.Settings.StoryboardApprovalPolicy, TextModel: g.Settings.TextModel,
			ImageModel: g.Settings.ImageModel, VideoModel: g.Settings.VideoModel,
		},
		Upstream:         append([]AssetVersionRef(nil), upstream...),
		ProviderSnapshot: providerSnapshot,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal input hash: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeVideoProviderSnapshot(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var snapshot videogen.TaskConfigSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode video provider snapshot: %w", err)
	}
	snapshot.ChannelType = strings.ToLower(strings.TrimSpace(snapshot.ChannelType))
	snapshot.BaseURL = strings.TrimRight(strings.TrimSpace(snapshot.BaseURL), "/")
	snapshot.Model = strings.TrimSpace(snapshot.Model)
	snapshot.AspectRatio = strings.TrimSpace(snapshot.AspectRatio)
	snapshot.Resolution = strings.ToLower(strings.TrimSpace(snapshot.Resolution))
	normalized, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode video provider snapshot: %w", err)
	}
	return normalized, nil
}

func SemanticGraphHash(g Graph) (string, error) {
	type semanticEdge struct{ Source, SourcePort, Target, TargetPort string }
	type semanticGroup struct {
		ID, Type, SceneID string
		Enabled           bool
		DurationSeconds   int
		NodeIDs           []string
	}
	nodes := make([]struct {
		ID   string       `json:"id"`
		Node semanticNode `json:"node"`
	}, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		nodes = append(nodes, struct {
			ID   string       `json:"id"`
			Node semanticNode `json:"node"`
		}{ID: node.ID, Node: semanticNode{Type: node.Type, Version: semanticNodeVersion(node), RoleID: node.RoleID,
			SceneID: node.SceneID, AssetID: node.AssetID, AssetVersionID: node.AssetVersionID, DurationSeconds: node.DurationSeconds, Config: normalizeJSON(node.Config), Inputs: node.Inputs, Outputs: node.Outputs}})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	edges := make([]semanticEdge, 0, len(g.Edges))
	for _, edge := range g.Edges {
		edges = append(edges, semanticEdge{edge.Source, edge.SourcePort, edge.Target, edge.TargetPort})
	}
	sort.Slice(edges, func(i, j int) bool {
		return fmt.Sprint(edges[i]) < fmt.Sprint(edges[j])
	})
	groups := make([]semanticGroup, 0, len(g.Groups))
	for _, group := range g.Groups {
		nodeIDs := append([]string(nil), group.NodeIDs...)
		sort.Strings(nodeIDs)
		groups = append(groups, semanticGroup{ID: group.ID, Type: group.Type, SceneID: group.SceneID, Enabled: group.Enabled, DurationSeconds: group.DurationSeconds, NodeIDs: nodeIDs})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	payload := struct {
		SchemaVersion int             `json:"schema_version"`
		Settings      Settings        `json:"settings"`
		Nodes         any             `json:"nodes"`
		Edges         []semanticEdge  `json:"edges"`
		Groups        []semanticGroup `json:"groups"`
	}{SchemaVersion: g.SchemaVersion, Settings: g.Settings, Nodes: nodes, Edges: edges, Groups: groups}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func semanticNodeVersion(node Node) int {
	if node.Version <= 0 {
		return 1
	}
	return node.Version
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		encoded, _ := json.Marshal(string(raw))
		return encoded
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return normalized
}
