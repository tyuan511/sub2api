// Backfill existing Image Studio thumbnails using the application's storage
// profiles. Build locally, then run inside the app container with --apply.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
)

func main() {
	apply := flag.Bool("apply", false, "Generate missing thumbnails; without this flag only count them")
	flag.Parse()
	if err := run(*apply); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(apply bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Totp.EncryptionKeyConfigured {
		return fmt.Errorf("a persistent encryption key is required")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `SELECT f.id,c.user_id FROM image_studio_files f JOIN image_studio_creations c ON c.id=f.creation_id WHERE NOT f.thumbnail_ready AND c.deleted_at IS NULL AND c.task->>'status' <> 'processing' ORDER BY f.id`)
	if err != nil {
		return err
	}
	type item struct {
		id     string
		userID int64
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.userID); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	fmt.Printf("Missing thumbnails: %d; apply: %t\n", len(items), apply)
	if !apply {
		return nil
	}
	encryptor, err := repository.NewAESEncryptor(cfg)
	if err != nil {
		return err
	}
	svc := service.NewImageStudioService(repository.NewImageStudioRepository(db), encryptor, repository.ProvideImageStorageFactory(), nil, nil, cfg)
	failed := 0
	for _, i := range items {
		if _, err = svc.Thumbnail(ctx, i.id, i.userID); err != nil {
			failed++
			fmt.Printf("Failed %s: %s\n", i.id, err)
			continue
		}
		fmt.Printf("Completed %s\n", i.id)
	}
	fmt.Printf("Completed: %d; failed: %d\n", len(items)-failed, failed)
	if failed > 0 {
		return fmt.Errorf("some thumbnails could not be generated; originals were preserved")
	}
	return nil
}
