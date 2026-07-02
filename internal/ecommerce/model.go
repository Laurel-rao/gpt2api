package ecommerce

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusQueued   = "queued"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

const (
	AssetTitle  = "title_image"
	AssetMain   = "main_image"
	AssetWhite  = "white_image"
	AssetDetail = "detail_image"
	AssetPrice  = "price_image"
	AssetVideo  = "product_video"
)

var assetTypes = []string{AssetTitle, AssetMain, AssetWhite, AssetDetail, AssetPrice}

func latestAssetsByType(assets []Asset) []Asset {
	latest := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		prev, ok := latest[asset.AssetType]
		if !ok || asset.ID >= prev.ID {
			latest[asset.AssetType] = asset
		}
	}
	out := make([]Asset, 0, len(latest))
	for _, assetType := range append(append([]string{}, assetTypes...), AssetVideo) {
		if asset, ok := latest[assetType]; ok {
			out = append(out, asset)
			delete(latest, assetType)
		}
	}
	for _, asset := range latest {
		out = append(out, asset)
	}
	return out
}

const (
	LibraryKindProduct = "product"
	LibraryKindModel   = "model"

	LibraryScopePrivate = "private"
	LibraryScopePublic  = "public"

	LibraryReviewDraft    = "draft"
	LibraryReviewPending  = "pending"
	LibraryReviewApproved = "approved"
	LibraryReviewRejected = "rejected"
)

type Platform struct {
	ID          uint64       `db:"id" json:"id"`
	Code        string       `db:"code" json:"code"`
	Name        string       `db:"name" json:"name"`
	Language    string       `db:"language" json:"language"`
	FieldSchema RawJSON      `db:"field_schema" json:"field_schema,omitempty"`
	Remark      string       `db:"remark" json:"remark"`
	Enabled     bool         `db:"enabled" json:"enabled"`
	CreatedAt   time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time    `db:"updated_at" json:"updated_at"`
	DeletedAt   sql.NullTime `db:"deleted_at" json:"-"`
}

type PromptTemplate struct {
	ID            uint64       `db:"id" json:"id"`
	Code          string       `db:"code" json:"code"`
	Name          string       `db:"name" json:"name"`
	ContentPrompt string       `db:"content_prompt" json:"content_prompt"`
	ImagePrompt   string       `db:"image_prompt" json:"image_prompt"`
	VideoPrompt   string       `db:"video_prompt" json:"video_prompt"`
	Remark        string       `db:"remark" json:"remark"`
	Enabled       bool         `db:"enabled" json:"enabled"`
	CreatedAt     time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time    `db:"updated_at" json:"updated_at"`
	DeletedAt     sql.NullTime `db:"deleted_at" json:"-"`
}

type StyleTemplate struct {
	ID           uint64       `db:"id" json:"id"`
	Code         string       `db:"code" json:"code"`
	Name         string       `db:"name" json:"name"`
	StylePrompt  string       `db:"style_prompt" json:"style_prompt"`
	LayoutConfig RawJSON      `db:"layout_config" json:"layout_config,omitempty"`
	Remark       string       `db:"remark" json:"remark"`
	Enabled      bool         `db:"enabled" json:"enabled"`
	CreatedAt    time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time    `db:"updated_at" json:"updated_at"`
	DeletedAt    sql.NullTime `db:"deleted_at" json:"-"`
}

type Task struct {
	ID               uint64     `db:"id" json:"id"`
	TaskID           string     `db:"task_id" json:"task_id"`
	UserID           uint64     `db:"user_id" json:"user_id"`
	PlatformID       uint64     `db:"platform_id" json:"platform_id"`
	PromptTemplateID uint64     `db:"prompt_template_id" json:"prompt_template_id"`
	StyleTemplateID  uint64     `db:"style_template_id" json:"style_template_id"`
	Language         string     `db:"language" json:"language"`
	Requirement      string     `db:"requirement" json:"requirement"`
	ReferenceImages  RawJSON    `db:"reference_images" json:"reference_images,omitempty"`
	ProductAssetID   string     `db:"product_asset_id" json:"product_asset_id,omitempty"`
	ModelAssetID     string     `db:"model_asset_id" json:"model_asset_id,omitempty"`
	Status           string     `db:"status" json:"status"`
	Progress         int        `db:"progress" json:"progress"`
	OutputJSON       RawJSON    `db:"output_json" json:"output_json,omitempty"`
	OutputHTML       string     `db:"output_html" json:"output_html,omitempty"`
	Error            string     `db:"error" json:"error,omitempty"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	StartedAt        *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt       *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	DeletedAt        *time.Time `db:"deleted_at" json:"deleted_at,omitempty"`
	DeletedBy        uint64     `db:"deleted_by" json:"deleted_by,omitempty"`
}

type TaskRow struct {
	Task
	PlatformName string `db:"platform_name" json:"platform_name"`
	PromptName   string `db:"prompt_name" json:"prompt_name"`
	StyleName    string `db:"style_name" json:"style_name"`
}

type Asset struct {
	ID          uint64     `db:"id" json:"id"`
	TaskID      string     `db:"task_id" json:"task_id"`
	AssetType   string     `db:"asset_type" json:"asset_type"`
	ImageTaskID string     `db:"image_task_id" json:"image_task_id"`
	URL         string     `db:"url" json:"url"`
	FileID      string     `db:"file_id" json:"file_id"`
	Prompt      string     `db:"prompt" json:"prompt"`
	Status      string     `db:"status" json:"status"`
	Progress    int        `db:"progress" json:"progress"`
	Error       string     `db:"error,omitempty" json:"error,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	StartedAt   *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt  *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
}

type LibraryAsset struct {
	ID           uint64       `db:"id" json:"id"`
	AssetID      string       `db:"asset_id" json:"asset_id"`
	OwnerUserID  uint64       `db:"owner_user_id" json:"owner_user_id"`
	Kind         string       `db:"kind" json:"kind"`
	Scope        string       `db:"scope" json:"scope"`
	ReviewStatus string       `db:"review_status" json:"review_status"`
	Name         string       `db:"name" json:"name"`
	Code         string       `db:"code" json:"code"`
	CoverURL     string       `db:"cover_url" json:"cover_url"`
	GalleryJSON  RawJSON      `db:"gallery_json" json:"gallery_json,omitempty"`
	TagsJSON     RawJSON      `db:"tags_json" json:"tags_json,omitempty"`
	DetailJSON   RawJSON      `db:"detail_json" json:"detail_json,omitempty"`
	Enabled      bool         `db:"enabled" json:"enabled"`
	ReviewNote   string       `db:"review_note" json:"review_note,omitempty"`
	ReviewedBy   uint64       `db:"reviewed_by" json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time   `db:"reviewed_at" json:"reviewed_at,omitempty"`
	CreatedAt    time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time    `db:"updated_at" json:"updated_at"`
	DeletedAt    sql.NullTime `db:"deleted_at" json:"-"`
}

type LibraryAssetFile struct {
	ID         uint64    `db:"id" json:"id"`
	AssetID    string    `db:"asset_id" json:"asset_id"`
	FileUsage  string    `db:"file_usage" json:"file_usage"`
	OriginName string    `db:"origin_name" json:"origin_name"`
	MIME       string    `db:"mime" json:"mime"`
	SizeBytes  int64     `db:"size_bytes" json:"size_bytes"`
	Width      int       `db:"width" json:"width"`
	Height     int       `db:"height" json:"height"`
	SHA256     string    `db:"sha256" json:"sha256"`
	URL        string    `db:"url" json:"url"`
	SortOrder  int       `db:"sort_order" json:"sort_order"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

type Output struct {
	ShopTitle      string                   `json:"shop_title"`
	ProductTitle   string                   `json:"product_title"`
	Description    string                   `json:"description"`
	PriceCopy      string                   `json:"price_copy"`
	ProductInfo    ProductInfo              `json:"product_info"`
	PriceInfo      PriceInfo                `json:"price_info"`
	MarketingCopy  []string                 `json:"marketing_copy"`
	DetailSections []DetailSection          `json:"detail_sections"`
	PlatformFields map[string]string        `json:"platform_fields"`
	ImageSpecs     map[string]ImageSpec     `json:"image_specs"`
	ImageTextPlans map[string]ImageTextPlan `json:"image_text_plans"`
}

type ProductInfo struct {
	Category       string   `json:"category"`
	CanonicalTitle string   `json:"canonical_title"`
	ShortTitle     string   `json:"short_title"`
	CoreValue      string   `json:"core_value"`
	KeySpecs       []string `json:"key_specs"`
	SellingPoints  []string `json:"selling_points"`
	TargetAudience string   `json:"target_audience"`
}

type PriceInfo struct {
	Currency      string `json:"currency"`
	SalePrice     string `json:"sale_price"`
	OriginalPrice string `json:"original_price"`
	PriceText     string `json:"price_text"`
	PromotionText string `json:"promotion_text"`
	CTA           string `json:"cta"`
}

type DetailSection struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type ImageSpec struct {
	Size        string `json:"size"`
	AspectRatio string `json:"aspect_ratio"`
	Clarity     string `json:"clarity"`
}

type ImageTextPlan struct {
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle"`
	PriceText     string   `json:"price_text"`
	PromotionText string   `json:"promotion_text"`
	CTA           string   `json:"cta"`
	Badges        []string `json:"badges"`
	SellingPoints []string `json:"selling_points"`
	Specs         []string `json:"specs"`
	Notes         []string `json:"notes"`
}

func NewTaskID() string { return "ecm_" + uuid.NewString() }

func NewLibraryAssetID() string { return "eal_" + uuid.NewString() }

func isAssetWorking(status string) bool {
	return status == StatusQueued || status == StatusRunning
}

func isUUID(s string) bool {
	_, err := uuid.Parse(strings.TrimSpace(s))
	return err == nil
}

// RawJSON 让 MySQL JSON NULL 可以安全扫进 Go，再按普通 JSON 输出。
type RawJSON json.RawMessage

func (r *RawJSON) Scan(value interface{}) error {
	if value == nil {
		*r = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*r = append((*r)[0:0], v...)
	case string:
		*r = append((*r)[0:0], v...)
	default:
		return fmt.Errorf("unsupported JSON scan type %T", value)
	}
	return nil
}

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return json.RawMessage(r).MarshalJSON()
}

func (r RawJSON) RawMessage() json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	return json.RawMessage(r)
}
