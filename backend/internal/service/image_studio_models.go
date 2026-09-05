package service

import (
	"context"
	"time"
)

// Storage profiles are retained when another profile is activated. Their
// encrypted credentials and object keys remain the source of truth for old files.
type StudioStorageProfile struct {
	ID               int64                `json:"id"`
	Name             string               `json:"name"`
	Config           ImageStorageSettings `json:"config"`
	SecretConfigured bool                 `json:"secret_configured"`
	FileCount        int64                `json:"file_count"`
	CreatedAt        time.Time            `json:"created_at"`
}
type StudioStorageState struct {
	ActiveID    int64 `json:"active_id"`
	Enabled     bool  `json:"enabled"`
	Initialized bool  `json:"-"`
}
type StudioMetadata struct {
	Prompt     string `json:"prompt"`
	Model      string `json:"model"`
	Ratio      string `json:"ratio"`
	Resolution string `json:"resolution"`
	Size       string `json:"size,omitempty"`
	Count      int    `json:"count"`
	KeyName    string `json:"key_name"`
	LegacyID   string `json:"legacy_id,omitempty"`
}
type StudioRecord struct {
	Task      ImageTaskRecord
	Metadata  StudioMetadata
	StorageID int64
}
type StudioFile struct {
	ThumbnailReady bool                 `json:"-"`
	ThumbnailURL   string               `json:"thumbnail_url,omitempty"`
	Locations      []StudioFileLocation `json:"-"`
	ID             string               `json:"id"`
	CreationID     string               `json:"-"`
	StorageID      int64                `json:"-"`
	ObjectKey      string               `json:"-"`
	Kind           string               `json:"-"`
	Position       int                  `json:"-"`
	Filename       string               `json:"filename"`
	ContentType    string               `json:"content_type"`
	Size           int64                `json:"size"`
	SHA256         string               `json:"-"`
	URL            string               `json:"url,omitempty"`
	RevisedPrompt  string               `json:"revised_prompt,omitempty"`
}
type StudioFileLocation struct {
	StorageID int64  `json:"storage_id"`
	ObjectKey string `json:"object_key"`
}
type StudioCreation struct {
	ID string `json:"id"`
	StudioMetadata
	KeyID      int64        `json:"key_id"`
	CreatedAt  int64        `json:"created_at"`
	Status     string       `json:"status"`
	Images     []StudioFile `json:"images"`
	References []StudioFile `json:"references"`
	Error      string       `json:"error,omitempty"`
}
type StudioObjectStorage interface {
	ImageStorage
	URL(context.Context, string) (string, error)
	Read(context.Context, string, int64) ([]byte, error)
	Delete(context.Context, string) error
}
type ImageStudioRepository interface {
	StorageState(context.Context) (StudioStorageState, error)
	ListStorage(context.Context) ([]StudioStorageProfile, error)
	GetStorage(context.Context, int64) (*StudioStorageProfile, error)
	AddStorage(context.Context, *StudioStorageProfile, bool) error
	ActivateStorage(context.Context, int64, bool) error
	SaveCreation(context.Context, *ImageTaskRecord, *StudioMetadata, int64) error
	GetCreation(context.Context, string, int64) (*StudioRecord, error)
	ListCreations(context.Context, int64, int, int) ([]StudioRecord, error)
	DeleteCreation(context.Context, string, int64, func([]StudioFile) error) error
	FindLegacy(context.Context, int64, string) (*StudioRecord, error)
	AddFile(context.Context, *StudioFile) error
	GetFile(context.Context, string, int64) (*StudioFile, error)
	EnsureThumbnail(context.Context, string, int64, func(StudioFile) error) (*StudioFile, error)
	ListFiles(context.Context, string) ([]StudioFile, error)
	StorageFiles(context.Context, int64, int) ([]StudioFile, int64, error)
	MoveFile(context.Context, string, int64, int64, string, func(StudioFile) error) error
}
