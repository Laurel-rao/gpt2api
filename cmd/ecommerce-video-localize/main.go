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

type videoRefreshClient struct {
	Channel string
	Client  *videogen.Client
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
	refreshClients := newVideoClients(ctx, sqldb, cfg)
	var ok, failed int
	for _, row := range rows {
		if *dryRun {
			fmt.Printf("candidate id=%d url=%s\n", row.ID, row.URL)
			continue
		}
		sourceURL := row.URL
		localURL, fileID, err := localizeVideoURL(ctx, row, sourceURL)
		if err != nil && len(refreshClients) > 0 && strings.TrimSpace(row.ImageTaskID) != "" {
			localURL, fileID, err = localizeWithRefreshedURLs(ctx, row, sourceURL, refreshClients)
		}
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "failed id=%d: %v\n", row.ID, err)
			continue
		}
		if err := dao.UpdateAssetFile(ctx, row.ID, localURL, fileID); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "update failed id=%d: %v\n", row.ID, err)
			continue
		}
		ok++
		fmt.Printf("localized id=%d url=%s\n", row.ID, localURL)
	}
	fmt.Printf("done candidates=%d localized=%d failed=%d\n", len(rows), ok, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func newVideoClients(ctx context.Context, sqldb *sqlx.DB, cfg *config.Config) []videoRefreshClient {
	settingsDAO := settings.NewDAO(sqldb)
	settingsSvc := settings.NewService(settingsDAO)
	if err := settingsSvc.Reload(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "settings reload failed: %v\n", err)
		return nil
	}
	channels := []string{
		settingsSvc.VideoGenChannelType(),
		videogen.ChannelAPIYISeedance,
		videogen.ChannelAPIYIWan27,
		videogen.ChannelAPIYIHappyHorse,
		videogen.ChannelEchoon,
	}
	var out []videoRefreshClient
	seen := map[string]struct{}{}
	for _, channel := range channels {
		channel = strings.TrimSpace(channel)
		if channel == "" {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		clientCfg := settingsSvc.VideoGenConfigForChannel(channel)
		if strings.TrimSpace(clientCfg.APIKey) == "" {
			clientCfg = startupVideoConfigForChannel(cfg, channel)
		}
		if strings.TrimSpace(clientCfg.APIKey) == "" {
			continue
		}
		out = append(out, videoRefreshClient{Channel: channel, Client: videogen.NewClient(clientCfg)})
	}
	if len(out) == 0 {
		fmt.Fprintln(os.Stderr, "videogen refresh disabled: api key is empty")
	}
	return out
}

func startupVideoConfigForChannel(cfg *config.Config, channel string) videogen.Config {
	out := videogen.Config{
		ChannelType:   channel,
		TimeoutSec:    cfg.VideoGen.TimeoutSec,
		DurationSec:   cfg.VideoGen.DurationSec,
		AspectRatio:   cfg.VideoGen.AspectRatio,
		Resolution:    cfg.VideoGen.Resolution,
		GenerateAudio: cfg.VideoGen.GenerateAudio,
		APIKeyEnv:     cfg.VideoGen.APIKeyEnv,
	}
	switch channel {
	case videogen.ChannelAPIYISeedance:
		out.BaseURL = cfg.VideoGen.APIYI.BaseURL
		out.APIKey = cfg.VideoGen.APIYI.APIKey
		out.Model = cfg.VideoGen.APIYI.Model
	case videogen.ChannelAPIYIWan27:
		out.BaseURL = cfg.VideoGen.APIYIWan27.BaseURL
		out.APIKey = cfg.VideoGen.APIYIWan27.APIKey
		out.Model = cfg.VideoGen.APIYIWan27.Model
	case videogen.ChannelAPIYIHappyHorse:
		out.BaseURL = cfg.VideoGen.APIYIHappyHorse.BaseURL
		out.APIKey = cfg.VideoGen.APIYIHappyHorse.APIKey
		out.Model = cfg.VideoGen.APIYIHappyHorse.Model
	default:
		out.ChannelType = videogen.ChannelEchoon
		out.BaseURL = cfg.VideoGen.BaseURL
		out.APIKey = cfg.VideoGen.APIKey
		out.Model = cfg.VideoGen.Model
	}
	return out
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

func localizeVideoURL(ctx context.Context, row videoAssetRow, sourceURL string) (string, string, error) {
	saved, err := ecommerce.SaveVideoFromURL(ctx, fmt.Sprintf("video_%d", row.ID), sourceURL)
	if err != nil {
		return "", "", err
	}
	return saved.URL, "local_video:" + saved.SHA256, nil
}

func localizeWithRefreshedURLs(ctx context.Context, row videoAssetRow, originalURL string, clients []videoRefreshClient) (string, string, error) {
	seen := map[string]struct{}{originalURL: {}}
	var lastErr error
	for _, refreshClient := range clients {
		refreshedURL, refreshErr := refreshVideoURL(ctx, refreshClient.Client, row.ImageTaskID)
		if refreshErr != nil {
			lastErr = refreshErr
			fmt.Fprintf(os.Stderr, "refresh failed id=%d channel=%s: %v\n", row.ID, refreshClient.Channel, refreshErr)
			continue
		}
		if refreshedURL == "" {
			lastErr = fmt.Errorf("empty refreshed url")
			continue
		}
		if _, ok := seen[refreshedURL]; ok {
			continue
		}
		seen[refreshedURL] = struct{}{}
		localURL, fileID, err := localizeVideoURL(ctx, row, refreshedURL)
		if err == nil {
			fmt.Printf("refreshed id=%d channel=%s\n", row.ID, refreshClient.Channel)
			return localURL, fileID, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download refreshed failed id=%d channel=%s: %v\n", row.ID, refreshClient.Channel, err)
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("no refreshed result url")
}

func exitf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, strings.TrimSpace(msg))
	os.Exit(1)
}
