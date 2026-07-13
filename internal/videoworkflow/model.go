package videoworkflow

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	SchemaVersionV1 = 1
	SchemaVersionV2 = 2
	MaxCharacters   = 4
	MaxScenes       = 4
	MaxGraphNodes   = 64
	MaxGraphEdges   = 128
	SceneDuration   = 15
	SceneDurationMS = 15_000
	TimelineStepMS  = 100
	MinClipMS       = 1_000
	DefaultFPS      = 30
	AssetQuotaBytes = int64(5 * 1024 * 1024 * 1024)
	MaxImageBytes   = int64(20 * 1024 * 1024)
	MaxVideoBytes   = int64(300 * 1024 * 1024)
)

type NodeType string

const (
	NodeStoryBrief NodeType = "story_brief"
	NodeCharacter  NodeType = "character"
	NodeScript     NodeType = "script"
	NodeScene      NodeType = "scene"
	NodeBackground NodeType = "background"
	NodeVideo      NodeType = "video"
	NodeTimeline   NodeType = "timeline"
	NodeCompose    NodeType = "compose"
)

type PortType string

const (
	PortText      PortType = "text"
	PortScript    PortType = "script"
	PortScene     PortType = "scene"
	PortImage     PortType = "image"
	PortImageSet  PortType = "image_set"
	PortVideo     PortType = "video"
	PortVideoList PortType = "video_list"
)

type AspectRatio string

const (
	AspectRatioPortrait  AspectRatio = "9:16"
	AspectRatioLandscape AspectRatio = "16:9"
	AspectRatioSquare    AspectRatio = "1:1"
)

type Resolution string

const (
	Resolution720p  Resolution = "720p"
	Resolution1080p Resolution = "1080p"
)

type ApprovalPolicy string

const (
	ApprovalManual    ApprovalPolicy = "manual"
	ApprovalAuto      ApprovalPolicy = "auto"
	ApprovalAutoFirst ApprovalPolicy = "auto_first"
)

type RunMode string

const (
	RunModeFull       RunMode = "full"
	RunModeNodeOnly   RunMode = "node_only"
	RunModeDownstream RunMode = "downstream"
)

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type PositionMode string

const (
	PositionAuto   PositionMode = "auto"
	PositionManual PositionMode = "manual"
)

type Size struct {
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
}

type Port struct {
	ID       string   `json:"id"`
	Label    string   `json:"label,omitempty"`
	Type     PortType `json:"type"`
	Required bool     `json:"required,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
}

type Node struct {
	ID              string          `json:"id"`
	Type            NodeType        `json:"type"`
	Version         int             `json:"version,omitempty"`
	RoleID          string          `json:"role_id,omitempty"`
	SceneID         string          `json:"scene_id,omitempty"`
	AssetID         string          `json:"asset_id,omitempty"`
	AssetVersionID  string          `json:"asset_version_id,omitempty"`
	DurationSeconds int             `json:"duration_seconds,omitempty"`
	Position        Position        `json:"position"`
	PositionMode    PositionMode    `json:"position_mode,omitempty"`
	Size            Size            `json:"size,omitempty"`
	Collapsed       bool            `json:"collapsed,omitempty"`
	Enabled         *bool           `json:"enabled,omitempty"`
	Locked          bool            `json:"locked,omitempty"`
	Config          json.RawMessage `json:"config,omitempty"`
	Inputs          []Port          `json:"inputs,omitempty"`
	Outputs         []Port          `json:"outputs,omitempty"`
}

type Edge struct {
	ID         string     `json:"id"`
	Source     string     `json:"source"`
	SourcePort string     `json:"source_port"`
	Target     string     `json:"target"`
	TargetPort string     `json:"target_port"`
	Curve      *Position  `json:"curve,omitempty"`
	Route      []Position `json:"route,omitempty"`
}

type Group struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	SceneID         string   `json:"scene_id,omitempty"`
	Enabled         bool     `json:"enabled"`
	DurationSeconds int      `json:"duration_seconds,omitempty"`
	NodeIDs         []string `json:"node_ids"`
	Position        Position `json:"position"`
	Size            Size     `json:"size,omitempty"`
	Collapsed       bool     `json:"collapsed,omitempty"`
}

type LayoutAnchor struct {
	Position     Position     `json:"position"`
	PositionMode PositionMode `json:"position_mode"`
}

type GraphLayout struct {
	SharedCharacterBus *LayoutAnchor `json:"shared_character_bus,omitempty"`
}

type Settings struct {
	AspectRatio              AspectRatio    `json:"aspect_ratio"`
	Resolution               Resolution     `json:"resolution,omitempty"`
	FPS                      int            `json:"fps"`
	SceneDurationMS          int            `json:"scene_duration_ms,omitempty"`
	SceneDurationSeconds     int            `json:"scene_duration_seconds,omitempty"`
	CharacterApprovalPolicy  ApprovalPolicy `json:"character_approval_policy,omitempty"`
	StoryboardApprovalPolicy ApprovalPolicy `json:"storyboard_approval_policy,omitempty"`
	TextModel                string         `json:"text_model,omitempty"`
	ImageModel               string         `json:"image_model,omitempty"`
	VideoModel               string         `json:"video_model,omitempty"`
}

func (s Settings) EffectiveSceneDurationMS() int {
	if s.SceneDurationMS > 0 {
		return s.SceneDurationMS
	}
	if s.SceneDurationSeconds > 0 {
		return s.SceneDurationSeconds * 1000
	}
	return 0
}

func (s Settings) EffectiveResolution() Resolution {
	if s.Resolution == "" {
		return Resolution720p
	}
	return s.Resolution
}

type TimelineClip struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	SourcePort   string `json:"source_port"`
	TrimInMS     int    `json:"trim_in_ms"`
	TrimOutMS    int    `json:"trim_out_ms"`
}

type TimelineConfig struct {
	Clips       []TimelineClip `json:"clips,omitempty"`
	ClipNodeIDs []string       `json:"clip_node_ids,omitempty"` // Graph v1 兼容
}

type ImageTransform struct {
	Crop           NormalizedCrop `json:"crop"`
	Rotation       int            `json:"rotation"`
	FlipHorizontal bool           `json:"flip_horizontal"`
	FlipVertical   bool           `json:"flip_vertical"`
}

type NormalizedCrop struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Graph struct {
	SchemaVersion int          `json:"schema_version"`
	Settings      Settings     `json:"settings"`
	Nodes         []Node       `json:"nodes"`
	Edges         []Edge       `json:"edges"`
	Groups        []Group      `json:"groups,omitempty"`
	Layout        *GraphLayout `json:"layout,omitempty"`
}

func (g *Graph) Scan(value any) error {
	if value == nil {
		*g = Graph{}
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("videoworkflow: unsupported graph scan type %T", value)
	}
	return json.Unmarshal(raw, g)
}

func (g Graph) Value() (driver.Value, error) {
	return json.Marshal(g)
}

func (s Settings) Value() (driver.Value, error) {
	return json.Marshal(s)
}

type RunStatus string

const (
	RunQueued                     RunStatus = "queued"
	RunRunning                    RunStatus = "running"
	RunAwaitingCharacterApproval  RunStatus = "awaiting_character_approval"
	RunAwaitingStoryboardApproval RunStatus = "awaiting_storyboard_approval"
	RunCancelPending              RunStatus = "cancel_pending"
	RunCanceled                   RunStatus = "canceled"
	RunSucceeded                  RunStatus = "succeeded"
	RunFailed                     RunStatus = "failed"
)

type NodeRunStatus string

const (
	NodeRunQueued           NodeRunStatus = "queued"
	NodeRunRunning          NodeRunStatus = "running"
	NodeRunAwaitingApproval NodeRunStatus = "awaiting_approval"
	NodeRunCancelPending    NodeRunStatus = "cancel_pending"
	NodeRunCanceled         NodeRunStatus = "canceled"
	NodeRunSucceeded        NodeRunStatus = "succeeded"
	NodeRunFailed           NodeRunStatus = "failed"
)

type AssetStatus string

const (
	AssetPending AssetStatus = "pending"
	AssetReady   AssetStatus = "ready"
	AssetFailed  AssetStatus = "failed"
	AssetDeleted AssetStatus = "deleted"
)

type ChargeStatus string

const (
	ChargeReserved ChargeStatus = "reserved"
	ChargeSettled  ChargeStatus = "settled"
	ChargeRefunded ChargeStatus = "refunded"
)

type Template struct {
	ID          uint64    `db:"id" json:"id"`
	Code        string    `db:"code" json:"code"`
	Version     int       `db:"version" json:"version"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description"`
	SourceURL   string    `db:"source_url" json:"source_url"`
	Graph       Graph     `db:"graph_json" json:"graph"`
	Enabled     bool      `db:"enabled" json:"enabled"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

type Workflow struct {
	ID              string     `db:"workflow_id" json:"id"`
	UserID          uint64     `db:"user_id" json:"user_id"`
	TemplateID      *uint64    `db:"template_id" json:"template_id,omitempty"`
	TemplateVersion int        `db:"template_version" json:"template_version"`
	Name            string     `db:"name" json:"name"`
	Revision        uint64     `db:"revision" json:"revision"`
	Graph           Graph      `db:"graph_json" json:"graph"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
	DeletedAt       *time.Time `db:"deleted_at" json:"-"`
}

type Run struct {
	ID              string     `db:"run_id" json:"id"`
	WorkflowID      string     `db:"workflow_id" json:"workflow_id"`
	UserID          uint64     `db:"user_id" json:"user_id"`
	Revision        uint64     `db:"workflow_revision" json:"workflow_revision"`
	GraphSnapshot   Graph      `db:"graph_snapshot" json:"graph_snapshot"`
	Status          RunStatus  `db:"status" json:"status"`
	Progress        int        `db:"progress" json:"progress"`
	RequestID       string     `db:"request_id" json:"request_id,omitempty"`
	RunMode         RunMode    `db:"run_mode" json:"run_mode,omitempty"`
	StartNodeID     string     `db:"start_node_id" json:"start_node_id,omitempty"`
	EstimateToken   string     `db:"estimate_token" json:"-"`
	EstimateHash    string     `db:"estimate_hash" json:"-"`
	EstimatedCost   int64      `db:"estimated_credits" json:"estimated_credits,omitempty"`
	ActualCost      int64      `db:"actual_credits" json:"actual_credits,omitempty"`
	OutputVersionID string     `db:"output_version_id" json:"output_version_id,omitempty"`
	LeaseOwner      string     `db:"lease_owner" json:"-"`
	LeaseExpiresAt  *time.Time `db:"lease_expires_at" json:"-"`
	HeartbeatAt     *time.Time `db:"heartbeat_at" json:"-"`
	ErrorCode       string     `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage    string     `db:"error_message" json:"error_message,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	StartedAt       *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt      *time.Time `db:"finished_at" json:"finished_at,omitempty"`
}

// RunListItem 是生成历史的轻量摘要，不返回不可变画布快照和节点运行明细。
type RunListItem struct {
	ID              string     `db:"run_id" json:"id"`
	WorkflowID      string     `db:"workflow_id" json:"workflow_id"`
	Revision        uint64     `db:"workflow_revision" json:"workflow_revision"`
	Status          RunStatus  `db:"status" json:"status"`
	Progress        int        `db:"progress" json:"progress"`
	RunMode         RunMode    `db:"run_mode" json:"run_mode,omitempty"`
	StartNodeID     string     `db:"start_node_id" json:"start_node_id,omitempty"`
	EstimatedCost   int64      `db:"estimated_credits" json:"estimated_credits,omitempty"`
	ActualCost      int64      `db:"actual_credits" json:"actual_credits,omitempty"`
	OutputVersionID string     `db:"output_version_id" json:"output_version_id,omitempty"`
	ErrorCode       string     `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage    string     `db:"error_message" json:"error_message,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	StartedAt       *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt      *time.Time `db:"finished_at" json:"finished_at,omitempty"`
}

type NodeRun struct {
	ID              string          `db:"node_run_id" json:"id"`
	RunID           string          `db:"run_id" json:"run_id"`
	NodeID          string          `db:"node_id" json:"node_id"`
	NodeType        NodeType        `db:"node_type" json:"node_type"`
	InputHash       string          `db:"input_hash" json:"input_hash"`
	Status          NodeRunStatus   `db:"status" json:"status"`
	Progress        int             `db:"progress" json:"progress"`
	ModelSnapshot   json.RawMessage `db:"model_snapshot" json:"model_snapshot,omitempty"`
	UpstreamTaskID  string          `db:"upstream_task_id" json:"upstream_task_id,omitempty"`
	ProviderState   json.RawMessage `db:"provider_state" json:"provider_state,omitempty"`
	OutputVersionID string          `db:"output_version_id" json:"output_version_id,omitempty"`
	Output          json.RawMessage `db:"output_json" json:"output,omitempty"`
	CreditCost      int64           `db:"credit_cost" json:"credit_cost"`
	CacheHit        bool            `db:"cache_hit" json:"cache_hit"`
	Attempt         int             `db:"attempt" json:"attempt"`
	ErrorCode       string          `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage    string          `db:"error_message" json:"error_message,omitempty"`
	CreatedAt       time.Time       `db:"created_at" json:"created_at"`
	StartedAt       *time.Time      `db:"started_at" json:"started_at,omitempty"`
	FinishedAt      *time.Time      `db:"finished_at" json:"finished_at,omitempty"`
}

type Asset struct {
	ID               string         `db:"asset_id" json:"id"`
	OwnerUserID      uint64         `db:"owner_user_id" json:"owner_user_id"`
	Kind             string         `db:"kind" json:"kind"`
	Name             string         `db:"name" json:"name"`
	Status           AssetStatus    `db:"status" json:"status"`
	CurrentVersionID string         `db:"current_version_id" json:"current_version_id,omitempty"`
	Versions         []AssetVersion `db:"-" json:"versions,omitempty"`
	CreatedAt        time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time      `db:"updated_at" json:"updated_at"`
	DeletedAt        *time.Time     `db:"deleted_at" json:"deleted_at,omitempty"`
}

type AssetVersion struct {
	ID                 string          `db:"version_id" json:"id"`
	AssetID            string          `db:"asset_id" json:"asset_id"`
	OwnerUserID        uint64          `db:"owner_user_id" json:"owner_user_id"`
	Version            uint64          `db:"version" json:"version"`
	Status             AssetStatus     `db:"status" json:"status"`
	MIMEType           string          `db:"mime_type" json:"mime_type"`
	StorageKey         string          `db:"storage_key" json:"-"`
	FilePath           string          `db:"file_path" json:"-"`
	ParentVersionID    string          `db:"parent_version_id" json:"parent_version_id,omitempty"`
	SourceType         string          `db:"source_type" json:"source_type"`
	Metadata           json.RawMessage `db:"metadata_json" json:"metadata,omitempty"`
	CreatedByRunID     string          `db:"created_by_run_id" json:"created_by_run_id,omitempty"`
	CreatedByNodeRunID string          `db:"created_by_node_run_id" json:"created_by_node_run_id,omitempty"`
	SizeBytes          int64           `db:"size_bytes" json:"size_bytes"`
	SHA256             string          `db:"sha256" json:"sha256"`
	Width              int             `db:"width" json:"width,omitempty"`
	Height             int             `db:"height" json:"height,omitempty"`
	DurationMS         int64           `db:"duration_ms" json:"duration_ms,omitempty"`
	InputHash          string          `db:"input_hash" json:"input_hash,omitempty"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
	DeletedAt          *time.Time      `db:"deleted_at" json:"deleted_at,omitempty"`
	PreviewURL         string          `db:"-" json:"preview_url,omitempty"`
}

type AssetReference struct {
	ID        uint64    `db:"id" json:"id"`
	VersionID string    `db:"version_id" json:"version_id"`
	UserID    uint64    `db:"user_id" json:"user_id"`
	RefType   string    `db:"ref_type" json:"ref_type"`
	RefID     string    `db:"ref_id" json:"ref_id"`
	NodeID    string    `db:"node_id" json:"node_id,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type ChargeReservation struct {
	ID              string       `db:"charge_id" json:"id"`
	UserID          uint64       `db:"user_id" json:"user_id"`
	RunID           string       `db:"run_id" json:"run_id"`
	NodeRunID       string       `db:"node_run_id" json:"node_run_id"`
	IdempotencyKey  string       `db:"idempotency_key" json:"idempotency_key"`
	Amount          int64        `db:"amount" json:"amount"`
	ActualAmount    int64        `db:"actual_amount" json:"actual_amount"`
	PlatformOverage int64        `db:"platform_overage" json:"platform_overage"`
	BillingRef      string       `db:"billing_ref" json:"billing_ref,omitempty"`
	Status          ChargeStatus `db:"status" json:"status"`
	CreatedAt       time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time    `db:"updated_at" json:"updated_at"`
}

type Approval struct {
	ID           string          `db:"approval_id" json:"id"`
	RunID        string          `db:"run_id" json:"run_id"`
	NodeRunID    string          `db:"node_run_id" json:"node_run_id"`
	NodeID       string          `db:"node_id" json:"node_id"`
	InputHash    string          `db:"input_hash" json:"input_hash"`
	Type         string          `db:"approval_type" json:"approval_type"`
	Payload      json.RawMessage `db:"payload_json" json:"payload,omitempty"`
	Status       string          `db:"status" json:"status"`
	DecisionNote string          `db:"decision_note" json:"decision_note,omitempty"`
	ExpiresAt    time.Time       `db:"expires_at" json:"expires_at"`
	DecidedAt    *time.Time      `db:"decided_at" json:"decided_at,omitempty"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at" json:"updated_at"`
}

func NewWorkflowID() string { return "vwf_" + uuid.NewString() }
func NewRunID() string      { return "vwr_" + uuid.NewString() }
func NewNodeRunID() string  { return "vwn_" + uuid.NewString() }
func NewAssetID() string    { return "vwa_" + uuid.NewString() }
func NewVersionID() string  { return "vwv_" + uuid.NewString() }
func NewChargeID() string   { return "vwc_" + uuid.NewString() }
func NewApprovalID() string { return "vwap_" + uuid.NewString() }

var (
	ErrNotFound         = errors.New("videoworkflow: not found")
	ErrInvalidInput     = errors.New("videoworkflow: invalid input")
	ErrRevisionConflict = errors.New("videoworkflow: revision conflict")
	ErrRequestConflict  = errors.New("videoworkflow: request_id conflicts with an existing run")
	ErrInvalidState     = errors.New("videoworkflow: invalid state transition")
	ErrInvalidGraph     = errors.New("videoworkflow: invalid graph")
	ErrRunsDraining     = errors.New("videoworkflow: new runs are temporarily disabled")
)
