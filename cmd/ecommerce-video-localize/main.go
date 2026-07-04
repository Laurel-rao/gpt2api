package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/432539/gpt2api/internal/config"
	"github.com/432539/gpt2api/internal/db"
	"github.com/432539/gpt2api/internal/ecommerce"
	"github.com/432539/gpt2api/internal/settings"
	"github.com/432539/gpt2api/internal/videogen"
)

type videoAssetRow struct {
	ID          uint64 `db:"id"`
	URL         string `db:"url"`
	ImageTaskID string `db:"image_task_id"`
}

func main() {
	configPath := flag.String("c", "configs/config.yaml", "config file path")
	limit := flag.Int("limit", 0, "max assets to localize, 0 means all")
	timeout := flag.Duration("timeout", 30*time.Minute, "total timeout")
	dryRun := flag.Bool("dry-run", false, "list candidates without downloading")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cfg, err := config.Load(*configPath)
	if err != nil {
		exitf("load config: %v", err)
	}
	sqldb, err := db.NewMySQL(cfg.MySQL)
	if err != nil {
		exitf("mysql init: %v", err)
	}
	defer sqldb.Close()

	query := `
SELECT id, COALESCE(url, '') AS url, COALESCE(image_task_id, '') AS image_task_id
  FROM ecommerce_assets
 WHERE asset_type='product_video'
   AND status='success'
   AND COALESCE(url, '') <> ''
   AND url NOT LIKE '/ecommerce-assets/%'
 ORDER BY id ASC`
	if *limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", *limit)
	}
	var rows []videoAssetRow
	if err := sqldb.SelectContext(ctx, &rows, query); err != nil {
		exitf("query assets: %v", err)
	}

	dao := ecommerce.NewDAO(sqldb)
	videoClient := newVideoClient(ctx, sqldb, cfg)
	var ok, failed int
	for _, row := range rows {
		if *dryRun {
			fmt.Printf("candidate id=%d url=%s\n", row.ID, row.URL)
			continue
		}
		sourceURL := row.URL
		saved, err := ecommerce.SaveVideoFromURL(ctx, fmt.Sprintf("video_%d", row.ID), sourceURL)
		if err != nil && videoClient != nil && strings.TrimSpace(row.ImageTaskID) != "" {
			refreshedURL, refreshErr := refreshVideoURL(ctx, videoClient, row.ImageTaskID)
			if refreshErr != nil {
				fmt.Fprintf(os.Stderr, "refresh failed id=%d: %v\n", row.ID, refreshErr)
			} else if refreshedURL != "" && refreshedURL != sourceURL {
				sourceURL = refreshedURL
				saved, err = ecommerce.SaveVideoFromURL(ctx, fmt.Sprintf("video_%d", row.ID), sourceURL)
			}
		}
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "failed id=%d: %v\n", row.ID, err)
			continue
		}
		if err := dao.UpdateAssetFile(ctx, row.ID, saved.URL, "local_video:"+saved.SHA256); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "update failed id=%d: %v\n", row.ID, err)
			continue
		}
		ok++
		fmt.Printf("localized id=%d url=%s\n", row.ID, saved.URL)
	}
	fmt.Printf("done candidates=%d localized=%d failed=%d\n", len(rows), ok, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func newVideoClient(ctx context.Context, sqldb *sqlx.DB, cfg *config.Config) *videogen.Client {
	settingsDAO := settings.NewDAO(sqldb)
	settingsSvc := settings.NewService(settingsDAO)
	if err := settingsSvc.Reload(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "settings reload failed: %v\n", err)
		return nil
	}
	videoGenBaseURL := cfg.VideoGen.BaseURL
	videoGenAPIKey := cfg.VideoGen.APIKey
	videoGenModel := cfg.VideoGen.Model
	switch cfg.VideoGen.ChannelType {
	case videogen.ChannelAPIYISeedance:
		videoGenBaseURL = cfg.VideoGen.APIYI.BaseURL
		videoGenAPIKey = cfg.VideoGen.APIYI.APIKey
		videoGenModel = cfg.VideoGen.APIYI.Model
	case videogen.ChannelAPIYIWan27:
		videoGenBaseURL = cfg.VideoGen.APIYIWan27.BaseURL
		videoGenAPIKey = cfg.VideoGen.APIYIWan27.APIKey
		videoGenModel = cfg.VideoGen.APIYIWan27.Model
	case videogen.ChannelAPIYIHappyHorse:
		videoGenBaseURL = cfg.VideoGen.APIYIHappyHorse.BaseURL
		videoGenAPIKey = cfg.VideoGen.APIYIHappyHorse.APIKey
		videoGenModel = cfg.VideoGen.APIYIHappyHorse.Model
	}
	client := videogen.NewClient(videogen.Config{
		ChannelType:   cfg.VideoGen.ChannelType,
		BaseURL:       videoGenBaseURL,
		APIKey:        videoGenAPIKey,
		APIKeyEnv:     cfg.VideoGen.APIKeyEnv,
		Model:         videoGenModel,
		TimeoutSec:    cfg.VideoGen.TimeoutSec,
		DurationSec:   cfg.VideoGen.DurationSec,
		AspectRatio:   cfg.VideoGen.AspectRatio,
		Resolution:    cfg.VideoGen.Resolution,
		GenerateAudio: cfg.VideoGen.GenerateAudio,
	})
	client.SetConfigProvider(settingsSvc)
	if !client.Enabled() {
		fmt.Fprintln(os.Stderr, "videogen refresh disabled: api key is empty")
		return nil
	}
	return client
}

func refreshVideoURL(ctx context.Context, client *videogen.Client, taskID string) (string, error) {
	result, err := client.GetTask(ctx, taskID)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	if strings.ToLower(strings.TrimSpace(result.Status)) != "completed" {
		return "", fmt.Errorf("upstream status %s", result.Status)
	}
	return strings.TrimSpace(result.ResultURL), nil
}

func exitf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, strings.TrimSpace(msg))
	os.Exit(1)
}
