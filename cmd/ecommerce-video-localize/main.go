package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/432539/gpt2api/internal/config"
	"github.com/432539/gpt2api/internal/db"
	"github.com/432539/gpt2api/internal/ecommerce"
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
	var ok, failed int
	for _, row := range rows {
		if *dryRun {
			fmt.Printf("candidate id=%d url=%s\n", row.ID, row.URL)
			continue
		}
		saved, err := ecommerce.SaveVideoFromURL(ctx, fmt.Sprintf("video_%d", row.ID), row.URL)
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

func exitf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, strings.TrimSpace(msg))
	os.Exit(1)
}
