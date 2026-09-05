package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type studioReadFailure struct{ service.StudioObjectStorage }

type studioSettingsRepository struct {
	service.SettingRepository
	db *sql.DB
}

func (r studioSettingsRepository) GetValue(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM fixture_settings WHERE key=$1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}
func (r studioSettingsRepository) Set(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO fixture_settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, key, value)
	return err
}

func (s studioReadFailure) Read(context.Context, string, int64) ([]byte, error) {
	return nil, fmt.Errorf("fixture read-back failure")
}

type studioDeleteFailure struct {
	service.StudioObjectStorage
	calls *int
}

func (s studioDeleteFailure) Delete(ctx context.Context, key string) error {
	*s.calls++
	if *s.calls == 2 {
		return fmt.Errorf("fixture deletion failure")
	}
	return s.StudioObjectStorage.Delete(ctx, key)
}

// Opt-in: real PostgreSQL and S3, with an isolated temporary schema and fixed
// image bytes. Never calls a paid generation provider or alters app user data.
func TestImageStudioDatabaseHistoryAndStorageMigration(t *testing.T) {
	dsn := os.Getenv("IMAGE_STUDIO_TEST_DATABASE_URL")
	endpoint := os.Getenv("IMAGE_STUDIO_TEST_S3_ENDPOINT")
	if dsn == "" || endpoint == "" {
		t.Skip("local PostgreSQL/S3 integration environment not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	schema := "studio_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	defer func() { _, _ = db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()
	_, err = db.ExecContext(ctx, `SET search_path TO `+schema)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE users(id BIGINT PRIMARY KEY); INSERT INTO users VALUES(7),(8)`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("240_image_studio_history.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must be idempotent")
	deletionMigration, err := migrations.FS.ReadFile("241_image_studio_file_locations.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.ExecContext(ctx, string(deletionMigration))
		require.NoError(t, err)
	}
	repo := repository.NewImageStudioRepository(db)
	cfg := &config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("ab", 32), EncryptionKeyConfigured: true}}
	encryptor, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)
	factory := repository.ProvideImageStorageFactory()
	svc := service.NewImageStudioService(repo, encryptor, factory, nil, nil, cfg)
	profile := func(name string) *service.StudioStorageProfile {
		return &service.StudioStorageProfile{Name: name, Config: service.ImageStorageSettings{Enabled: true, Endpoint: endpoint, Region: "us-east-1", Bucket: os.Getenv("IMAGE_STUDIO_TEST_S3_BUCKET"), Prefix: "integration-tests/" + schema + "/" + name + "/", AccessKeyID: os.Getenv("IMAGE_STUDIO_TEST_S3_ACCESS_KEY"), SecretAccessKey: os.Getenv("IMAGE_STUDIO_TEST_S3_SECRET"), ForcePathStyle: true, PresignExpiry: 24, MaxDownloadBytes: 33554432}}
	}
	first, second := profile("first"), profile("second")
	addStorage := func(p *service.StudioStorageProfile) error {
		secret, err := encryptor.Encrypt(p.Config.SecretAccessKey)
		if err != nil {
			return err
		}
		p.Config.SecretAccessKey = secret
		return repo.AddStorage(ctx, p, false)
	}
	require.NoError(t, addStorage(first))
	const fixture = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII="
	png, err := base64.StdEncoding.DecodeString(fixture)
	require.NoError(t, err)
	ref := service.OpenAIImagesUpload{FileName: "reference.png", ContentType: "image/png", Data: png}
	meta := service.StudioMetadata{Prompt: "Database image fixture", Model: "gpt-image-2", Ratio: "21:9", Resolution: "4K", Size: "3024x1296", Count: 1, KeyName: "test-key-name"}
	task, tasks, err := svc.Start(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, meta, []service.OpenAIImagesUpload{ref})
	require.NoError(t, err)
	require.ErrorContains(t, svc.Delete(ctx, task.ID, 7), "图片仍在生成")
	require.NoError(t, addStorage(second))
	require.NoError(t, repo.ActivateStorage(ctx, second.ID, false))
	_, _, err = svc.MigrateStorage(ctx, first.ID, second.ID)
	require.ErrorContains(t, err, "原存储仍有图片任务执行中")
	// In-flight generation uses its captured profile even after switching/off.
	require.NoError(t, tasks.Complete(ctx, task.ID, 200, json.RawMessage(`{"data":[{"b64_json":"`+fixture+`","revised_prompt":"revised fixture"}]}`)))
	svc = service.NewImageStudioService(repo, encryptor, factory, nil, nil, cfg)
	creation, err := svc.Get(ctx, task.ID, 7)
	require.NoError(t, err)
	require.Equal(t, "completed", creation.Status)
	require.Equal(t, meta.Size, creation.Size)
	require.Equal(t, meta.Ratio, creation.Ratio)
	require.Len(t, creation.Images, 1)
	require.Len(t, creation.References, 1)
	require.Contains(t, creation.Images[0].URL, "/first/")
	require.Equal(t, "revised fixture", creation.Images[0].RevisedPrompt)
	_, err = svc.Get(ctx, task.ID, 8)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	_, err = svc.File(ctx, creation.Images[0].ID, 8)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	require.ErrorIs(t, svc.Delete(ctx, task.ID, 8), service.ErrImageTaskNotFound)
	var persisted, secret string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT task::text FROM image_studio_creations WHERE id=$1`, task.ID).Scan(&persisted))
	require.NotContains(t, persisted, "http://")
	require.NotContains(t, persisted, "https://")
	require.NotContains(t, persisted, "b64_json")
	require.Contains(t, persisted, "image_id")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT config->>'secret_access_key' FROM image_studio_storage_profiles WHERE id=$1`, first.ID).Scan(&secret))
	require.NotEqual(t, os.Getenv("IMAGE_STUDIO_TEST_S3_SECRET"), secret)
	_, profiles, err := svc.StorageSettings(ctx)
	require.NoError(t, err)
	for _, p := range profiles {
		require.Empty(t, p.Config.SecretAccessKey)
	}
	download := func(id string) {
		file, err := svc.File(ctx, id, 7)
		require.NoError(t, err)
		resp, err := http.Get(file.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, 200, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, png, body)
	}
	download(creation.Images[0].ID)
	badFactory := func(ctx context.Context, c *config.ImageStorageConfig) (service.ImageStorage, error) {
		storage, err := factory(ctx, c)
		if err != nil {
			return nil, err
		}
		if strings.Contains(c.Prefix, "second/") {
			return studioReadFailure{storage.(service.StudioObjectStorage)}, nil
		}
		return storage, nil
	}
	failureSvc := service.NewImageStudioService(repo, encryptor, badFactory, nil, nil, cfg)
	_, _, err = failureSvc.MigrateStorage(ctx, first.ID, second.ID)
	require.Error(t, err)
	unchanged, err := svc.File(ctx, creation.Images[0].ID, 7)
	require.NoError(t, err)
	require.Contains(t, unchanged.URL, "/first/")
	moved, remaining, err := svc.MigrateStorage(ctx, first.ID, second.ID)
	require.NoError(t, err)
	require.Equal(t, 2, moved)
	require.Zero(t, remaining)
	migrated, err := svc.Get(ctx, task.ID, 7)
	require.NoError(t, err)
	require.Equal(t, creation.ID, migrated.ID)
	require.Equal(t, creation.Images[0].ID, migrated.Images[0].ID)
	require.Contains(t, migrated.Images[0].URL, "/second/")
	download(migrated.Images[0].ID)
	download(migrated.References[0].ID)
	// Files still exist in the old bucket/path for rollback.
	oldResponse, err := http.Get(creation.Images[0].URL)
	require.NoError(t, err)
	oldResponse.Body.Close()
	require.Equal(t, 200, oldResponse.StatusCode)
	require.NoError(t, repo.ActivateStorage(ctx, second.ID, true))
	importedMeta := meta
	importedMeta.LegacyID = "browser-record-1"
	imported, err := svc.Import(ctx, 7, importedMeta, "completed", "", 0, []service.OpenAIImagesUpload{ref}, []service.OpenAIImagesUpload{ref})
	require.NoError(t, err)
	again, err := svc.Import(ctx, 7, importedMeta, "completed", "", 0, []service.OpenAIImagesUpload{ref}, []service.OpenAIImagesUpload{ref})
	require.NoError(t, err)
	require.Equal(t, imported.ID, again.ID)
	for i := 0; i < 21; i++ {
		record, runner, err := svc.Start(ctx, service.ImageTaskOwner{UserID: 8, APIKeyID: 12}, meta, nil)
		require.NoError(t, err)
		require.NoError(t, runner.Fail(ctx, record.ID, 400, json.RawMessage(`{"message":"fixture"}`)))
	}
	page, more, err := svc.List(ctx, 8, 1)
	require.NoError(t, err)
	require.Len(t, page, 20)
	require.True(t, more)
	page, more, err = svc.List(ctx, 8, 2)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.False(t, more)
	// Exercise the new submission and JWT history handler boundary, including
	// multipart references and the canonical path received by the gateway runner.
	paths := make(chan string, 1)
	handler := NewImageStudioHandler(svc, &AsyncImageHandler{
		openAI: &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}},
		execute: func(_ string, c *gin.Context) {
			paths <- c.Request.URL.Path
			c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": fixture}}})
		},
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, Name: "HTTP fixture Key", Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true}})
	})
	router.POST("/v1/images/studio/edits", handler.Submit)
	router.GET("/api/v1/image-studio/history/:id", handler.Get)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range map[string]string{"model": "gpt-image-2", "prompt": "HTTP image fixture", "size": "1024x1024", "n": "1"} {
		require.NoError(t, form.WriteField(k, v))
	}
	part, err := form.CreateFormFile("image[]", "reference.png")
	require.NoError(t, err)
	_, err = part.Write(png)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	req := httptest.NewRequest(http.MethodPost, "/v1/images/studio/edits", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	accepted := httptest.NewRecorder()
	router.ServeHTTP(accepted, req)
	require.Equal(t, http.StatusAccepted, accepted.Code, accepted.Body.String())
	var submitted struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &submitted))
	require.Eventually(t, func() bool {
		item, err := svc.Get(ctx, submitted.ID, 7)
		return err == nil && item.Status == service.ImageTaskStatusCompleted
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, "/v1/images/edits", <-paths)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/image-studio/history/"+submitted.ID, nil))
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Data service.StudioCreation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Images, 1)
	require.Len(t, envelope.Data.References, 1)
	require.Equal(t, "HTTP fixture Key", envelope.Data.KeyName)
	require.NotContains(t, response.Body.String(), fixture)
	download(envelope.Data.Images[0].ID)
	// Existing rows from before copy tracking retain both the original and the
	// current storage location when the upgrade migration backfills their files.
	_, err = db.ExecContext(ctx, `UPDATE image_studio_files SET storage_locations='[]' WHERE creation_id=$1`, task.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(deletionMigration))
	require.NoError(t, err)
	backfilled, err := repo.ListFiles(ctx, task.ID)
	require.NoError(t, err)
	for _, file := range backfilled {
		require.Len(t, file.Locations, 2)
	}
	deleteCalls := 0
	deleteFactory := func(ctx context.Context, c *config.ImageStorageConfig) (service.ImageStorage, error) {
		storage, err := factory(ctx, c)
		if err != nil {
			return nil, err
		}
		return studioDeleteFailure{storage.(service.StudioObjectStorage), &deleteCalls}, nil
	}
	deleteSvc := service.NewImageStudioService(repo, encryptor, deleteFactory, nil, nil, cfg)
	require.ErrorIs(t, deleteSvc.Delete(ctx, task.ID, 8), service.ErrImageTaskNotFound)
	require.Zero(t, deleteCalls, "another account must never delete objects")
	require.ErrorContains(t, deleteSvc.Delete(ctx, task.ID, 7), "图片清理未完成")
	_, err = svc.Get(ctx, task.ID, 7)
	require.NoError(t, err, "failed cleanup must retain its history and file manifest")
	require.NoError(t, svc.Delete(ctx, task.ID, 7), "retry must tolerate already removed objects")
	var creationCount, fileCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_creations WHERE id=$1`, task.ID).Scan(&creationCount))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_files WHERE creation_id=$1`, task.ID).Scan(&fileCount))
	require.Zero(t, creationCount)
	require.Zero(t, fileCount)
	for _, file := range append(append(creation.Images, creation.References...), append(migrated.Images, migrated.References...)...) {
		response, err := http.Get(file.URL)
		require.NoError(t, err)
		response.Body.Close()
		require.Equal(t, http.StatusNotFound, response.StatusCode, "current objects and retained migration copies must be gone")
	}
	download(imported.Images[0].ID)
	_, err = svc.File(ctx, creation.Images[0].ID, 7)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)

	t.Run("shared async settings control new tasks and preserve old files", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `CREATE TABLE fixture_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)`)
		require.NoError(t, err)
		settingsRepo := studioSettingsRepository{db: db}
		backup := service.NewBackupService(settingsRepo, cfg, encryptor, nil, nil)
		settings := service.NewImageStorageSettingService(settingsRepo, encryptor, backup, factory, config.ImageStorageConfig{})
		studio := service.NewImageStudioService(repo, encryptor, factory, settings, nil, cfg)
		a, b := profile("shared-a").Config, profile("shared-b").Config
		_, err = settings.Update(ctx, a)
		require.NoError(t, err)
		pending, runner, err := studio.Start(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, meta, nil)
		require.NoError(t, err)
		_, err = settings.Update(ctx, b)
		require.NoError(t, err)
		require.True(t, studio.Available(ctx))
		require.NoError(t, runner.Complete(ctx, pending.ID, 200, json.RawMessage(`{"data":[{"b64_json":"`+fixture+`"}]}`)))
		old, err := studio.Get(ctx, pending.ID, 7)
		require.NoError(t, err)
		require.Contains(t, old.Images[0].URL, "/shared-a/")
		next, nextRunner, err := studio.Start(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, meta, nil)
		require.NoError(t, err)
		require.NoError(t, nextRunner.Complete(ctx, next.ID, 200, json.RawMessage(`{"data":[{"b64_json":"`+fixture+`"}]}`)))
		current, err := studio.Get(ctx, next.ID, 7)
		require.NoError(t, err)
		require.Contains(t, current.Images[0].URL, "/shared-b/")
		state, snapshots, err := studio.StorageSettings(ctx)
		require.NoError(t, err)
		b.Enabled = false
		_, err = settings.Update(ctx, b)
		require.NoError(t, err)
		require.False(t, studio.Available(ctx), "the existing shared switch controls studio availability")
		download(old.Images[0].ID)
		b.Enabled = true
		_, err = settings.Update(ctx, b)
		require.NoError(t, err)
		require.True(t, studio.Available(ctx))
		again, sameSnapshots, err := studio.StorageSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, state.ActiveID, again.ActiveID)
		require.Len(t, sameSnapshots, len(snapshots), "unchanged settings and toggles must reuse snapshots")
		_, err = backup.UpdateS3Config(ctx, service.BackupS3Config{Endpoint: a.Endpoint, Region: a.Region, Bucket: a.Bucket, AccessKeyID: a.AccessKeyID, SecretAccessKey: a.SecretAccessKey, ForcePathStyle: true})
		require.NoError(t, err)
		_, err = settings.Update(ctx, service.ImageStorageSettings{Enabled: true, ReuseBackupS3: true, Prefix: a.Prefix})
		require.NoError(t, err)
		reused, reusedSnapshots, err := studio.StorageSettings(ctx)
		require.NoError(t, err)
		require.True(t, reused.Enabled)
		require.NotEqual(t, state.ActiveID, reused.ActiveID)
		require.Len(t, reusedSnapshots, len(snapshots), "resolved backup credentials reuse the original equivalent snapshot")
		download(current.Images[0].ID)
		_, err = repo.GetCreation(ctx, pending.ID, 7)
		require.NoError(t, err)
	})
}
