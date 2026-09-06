package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type imageStudioRepository struct{ db *sql.DB }

func NewImageStudioRepository(db *sql.DB) service.ImageStudioRepository {
	return &imageStudioRepository{db: db}
}
func (r *imageStudioRepository) StorageState(ctx context.Context) (s service.StudioStorageState, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(active_id,0),enabled,initialized FROM image_studio_storage_state WHERE id=1`).Scan(&s.ActiveID, &s.Enabled, &s.Initialized)
	return
}
func (r *imageStudioRepository) ListStorage(ctx context.Context) ([]service.StudioStorageProfile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.name,p.config,p.created_at,(SELECT COUNT(*) FROM image_studio_files f WHERE f.storage_profile_id=p.id) FROM image_studio_storage_profiles p ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.StudioStorageProfile{}
	for rows.Next() {
		var p service.StudioStorageProfile
		var raw []byte
		if err = rows.Scan(&p.ID, &p.Name, &raw, &p.CreatedAt, &p.FileCount); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &p.Config); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *imageStudioRepository) GetStorage(ctx context.Context, id int64) (*service.StudioStorageProfile, error) {
	var p service.StudioStorageProfile
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,name,config,created_at FROM image_studio_storage_profiles WHERE id=$1`, id).Scan(&p.ID, &p.Name, &raw, &p.CreatedAt)
	if err != nil {
		return nil, studioNotFound(err)
	}
	err = json.Unmarshal(raw, &p.Config)
	return &p, err
}
func (r *imageStudioRepository) AddStorage(ctx context.Context, p *service.StudioStorageProfile, bootstrap bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var initialized bool
	if err = tx.QueryRowContext(ctx, `SELECT initialized FROM image_studio_storage_state WHERE id=1 FOR UPDATE`).Scan(&initialized); err != nil {
		return err
	}
	if bootstrap && initialized {
		return nil
	}
	raw, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO image_studio_storage_profiles(name,config) VALUES($1,$2) RETURNING id,created_at`, p.Name, string(raw)).Scan(&p.ID, &p.CreatedAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE image_studio_storage_state SET active_id=$1,enabled=$2,initialized=TRUE WHERE id=1`, p.ID, p.Config.Enabled); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *imageStudioRepository) ActivateStorage(ctx context.Context, id int64, enabled bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE image_studio_storage_state SET active_id=NULLIF($1,0),enabled=$2,initialized=TRUE WHERE id=1`, id, enabled)
	return err
}
func (r *imageStudioRepository) SaveCreation(ctx context.Context, task *service.ImageTaskRecord, meta *service.StudioMetadata, storageID int64) error {
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	if meta == nil {
		result, err := r.db.ExecContext(ctx, `UPDATE image_studio_creations SET task=$2,created_at=to_timestamp(($2::jsonb->>'created_at')::bigint) WHERE id=$1`, task.ID, string(raw))
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return service.ErrImageTaskNotFound
		}
		return nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if meta.LegacyID != "" {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('image-studio-import:' || $1::text,0))`, task.UserID); err != nil {
			return err
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_creations WHERE user_id=$1 AND legacy_id IS NOT NULL`, task.UserID).Scan(&count); err != nil {
			return err
		}
		if count >= 20 {
			return infraerrors.BadRequest("LEGACY_IMPORT_LIMIT", "最多导入 20 条旧浏览器记录")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO image_studio_creations(id,user_id,storage_profile_id,task,metadata,legacy_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, task.ID, task.UserID, storageID, string(raw), string(data), meta.LegacyID, time.Unix(task.CreatedAt, 0))
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO image_studio_creations(id,user_id,storage_profile_id,task,metadata,legacy_id,created_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7)`, task.ID, task.UserID, storageID, string(raw), string(data), meta.LegacyID, time.Unix(task.CreatedAt, 0))
	return err
}

type studioScanner interface{ Scan(...any) error }

func scanStudio(row studioScanner) (*service.StudioRecord, error) {
	var out service.StudioRecord
	var task, meta []byte
	if err := row.Scan(&task, &meta, &out.StorageID); err != nil {
		return nil, studioNotFound(err)
	}
	if err := json.Unmarshal(task, &out.Task); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(meta, &out.Metadata); err != nil {
		return nil, err
	}
	return &out, nil
}
func studioNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrImageTaskNotFound
	}
	return err
}
func (r *imageStudioRepository) GetCreation(ctx context.Context, id string, userID int64) (*service.StudioRecord, error) {
	return scanStudio(r.db.QueryRowContext(ctx, `SELECT task,metadata,storage_profile_id FROM image_studio_creations WHERE id=$1 AND ($2::bigint=0 OR user_id=$2) AND deleted_at IS NULL`, id, userID))
}
func (r *imageStudioRepository) FindLegacy(ctx context.Context, userID int64, id string) (*service.StudioRecord, error) {
	return scanStudio(r.db.QueryRowContext(ctx, `SELECT task,metadata,storage_profile_id FROM image_studio_creations WHERE user_id=$1 AND legacy_id=$2`, userID, id))
}
func (r *imageStudioRepository) ListCreations(ctx context.Context, userID int64, offset, limit int) ([]service.StudioRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT task,metadata,storage_profile_id FROM image_studio_creations WHERE user_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC,id DESC OFFSET $2 LIMIT $3`, userID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.StudioRecord{}
	for rows.Next() {
		item, err := scanStudio(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// Hold the creation lock until object cleanup and the database delete finish.
// Migration takes the same lock, so it cannot create a new copy during deletion.
func (r *imageStudioRepository) DeleteCreation(ctx context.Context, id string, userID int64, cleanup func([]service.StudioFile) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var processing bool
	err = tx.QueryRowContext(ctx, `SELECT task->>'status'='processing' AND created_at>NOW()-INTERVAL '35 minutes' FROM image_studio_creations WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, userID).Scan(&processing)
	if err != nil {
		return studioNotFound(err)
	}
	if processing {
		return infraerrors.Conflict("IMAGE_TASK_PROCESSING", "图片仍在生成，请完成后再删除")
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+studioFileColumns+` FROM image_studio_files f WHERE creation_id=$1 ORDER BY f.id`, id)
	if err != nil {
		return err
	}
	files, err := scanStudioFiles(rows)
	rows.Close()
	if err != nil {
		return err
	}
	if err = cleanup(files); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM image_studio_creations WHERE id=$1 AND user_id=$2`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *imageStudioRepository) AddFile(ctx context.Context, f *service.StudioFile) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO image_studio_files(id,creation_id,storage_profile_id,object_key,kind,position,filename,content_type,size_bytes,sha256,storage_locations,thumbnail_ready) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,jsonb_build_array(jsonb_build_object('storage_id',$3::bigint,'object_key',$4::text)),$11) ON CONFLICT(id) DO UPDATE SET storage_profile_id=EXCLUDED.storage_profile_id,object_key=EXCLUDED.object_key,filename=EXCLUDED.filename,content_type=EXCLUDED.content_type,size_bytes=EXCLUDED.size_bytes,sha256=EXCLUDED.sha256,storage_locations=image_studio_files.storage_locations || EXCLUDED.storage_locations,thumbnail_ready=EXCLUDED.thumbnail_ready`, f.ID, f.CreationID, f.StorageID, f.ObjectKey, f.Kind, f.Position, f.Filename, f.ContentType, f.Size, f.SHA256, f.ThumbnailReady)
	return err
}

const studioFileColumns = `f.id,f.creation_id,f.storage_profile_id,f.object_key,f.kind,f.position,f.filename,f.content_type,f.size_bytes,f.sha256,f.storage_locations,f.thumbnail_ready`

func scanStudioFile(row studioScanner) (*service.StudioFile, error) {
	var f service.StudioFile
	var locations []byte
	err := row.Scan(&f.ID, &f.CreationID, &f.StorageID, &f.ObjectKey, &f.Kind, &f.Position, &f.Filename, &f.ContentType, &f.Size, &f.SHA256, &locations, &f.ThumbnailReady)
	if err == nil {
		err = json.Unmarshal(locations, &f.Locations)
	}
	return &f, studioNotFound(err)
}
func (r *imageStudioRepository) GetFile(ctx context.Context, id string, userID int64) (*service.StudioFile, error) {
	return scanStudioFile(r.db.QueryRowContext(ctx, `SELECT `+studioFileColumns+` FROM image_studio_files f JOIN image_studio_creations c ON c.id=f.creation_id WHERE f.id=$1 AND c.user_id=$2 AND c.deleted_at IS NULL`, id, userID))
}
func (r *imageStudioRepository) EnsureThumbnail(ctx context.Context, id string, userID int64, create func(service.StudioFile) error) (*service.StudioFile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var creationID string
	err = tx.QueryRowContext(ctx, `SELECT c.id FROM image_studio_creations c JOIN image_studio_files f ON f.creation_id=c.id WHERE f.id=$1 AND c.user_id=$2 AND c.deleted_at IS NULL FOR UPDATE OF c`, id, userID).Scan(&creationID)
	if err != nil {
		return nil, studioNotFound(err)
	}
	file, err := scanStudioFile(tx.QueryRowContext(ctx, `SELECT `+studioFileColumns+` FROM image_studio_files f WHERE f.id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	if !file.ThumbnailReady {
		if err = create(*file); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE image_studio_files SET thumbnail_ready=TRUE WHERE id=$1`, id); err != nil {
			return nil, err
		}
		file.ThumbnailReady = true
	}
	return file, tx.Commit()
}
func (r *imageStudioRepository) ListFiles(ctx context.Context, id string) ([]service.StudioFile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+studioFileColumns+` FROM image_studio_files f WHERE f.creation_id=$1 ORDER BY f.kind,f.position,f.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStudioFiles(rows)
}
func scanStudioFiles(rows *sql.Rows) ([]service.StudioFile, error) {
	out := []service.StudioFile{}
	for rows.Next() {
		f, err := scanStudioFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}
func (r *imageStudioRepository) StorageFiles(ctx context.Context, id int64, limit int) ([]service.StudioFile, int64, error) {
	var pending bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM image_studio_creations WHERE storage_profile_id=$1 AND task->>'status'='processing' AND created_at>NOW()-INTERVAL '35 minutes')`, id).Scan(&pending); err != nil {
		return nil, 0, err
	}
	if pending {
		return nil, 0, infraerrors.Conflict("IMAGE_STORAGE_BUSY", "原存储仍有图片任务执行中，请等待完成后再迁移")
	}
	var total int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_files WHERE storage_profile_id=$1`, id).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+studioFileColumns+` FROM image_studio_files f WHERE storage_profile_id=$1 ORDER BY id LIMIT $2`, id, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files, err := scanStudioFiles(rows)
	return files, total, err
}
func (r *imageStudioRepository) MoveFile(ctx context.Context, id string, from, to int64, key string, copyFile func(service.StudioFile) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var creationID string
	err = tx.QueryRowContext(ctx, `SELECT c.id FROM image_studio_creations c JOIN image_studio_files f ON f.creation_id=c.id WHERE f.id=$1 FOR UPDATE OF c`, id).Scan(&creationID)
	if err != nil {
		return studioNotFound(err)
	}
	file, err := scanStudioFile(tx.QueryRowContext(ctx, `SELECT `+studioFileColumns+` FROM image_studio_files f WHERE f.id=$1 AND f.storage_profile_id=$2 FOR UPDATE`, id, from))
	if err != nil {
		return infraerrors.Conflict("IMAGE_STORAGE_CHANGED", "文件位置已更改，请刷新后重试").WithCause(err)
	}
	copyErr := copyFile(*file)
	// Keep the target in the cleanup manifest even if read-back verification fails.
	locations, err := json.Marshal(append(file.Locations, service.StudioFileLocation{StorageID: from, ObjectKey: file.ObjectKey}, service.StudioFileLocation{StorageID: to, ObjectKey: key}))
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE image_studio_files SET storage_locations=$2 WHERE id=$1`, id, string(locations)); err != nil {
		return err
	}
	if copyErr == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE image_studio_files SET storage_profile_id=$2,object_key=$3 WHERE id=$1`, id, to, key); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return copyErr
}
