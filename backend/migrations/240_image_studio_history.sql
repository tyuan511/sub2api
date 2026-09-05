CREATE TABLE IF NOT EXISTS image_studio_storage_profiles (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    config JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS image_studio_storage_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    active_id BIGINT REFERENCES image_studio_storage_profiles(id),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    initialized BOOLEAN NOT NULL DEFAULT FALSE
);
INSERT INTO image_studio_storage_state (id) VALUES (1) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS image_studio_creations (
    id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    storage_profile_id BIGINT NOT NULL REFERENCES image_studio_storage_profiles(id),
    task JSONB NOT NULL,
    metadata JSONB NOT NULL,
    legacy_id VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (user_id, legacy_id)
);
CREATE INDEX IF NOT EXISTS idx_image_studio_user_history ON image_studio_creations (user_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS image_studio_files (
    id UUID PRIMARY KEY,
    creation_id VARCHAR(64) NOT NULL REFERENCES image_studio_creations(id) ON DELETE CASCADE,
    storage_profile_id BIGINT NOT NULL REFERENCES image_studio_storage_profiles(id),
    object_key TEXT NOT NULL,
    kind VARCHAR(12) NOT NULL CHECK (kind IN ('reference', 'output')),
    position INTEGER NOT NULL,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    sha256 VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (storage_profile_id, object_key)
);
CREATE INDEX IF NOT EXISTS idx_image_studio_creation_files ON image_studio_files (creation_id, kind, position);
CREATE INDEX IF NOT EXISTS idx_image_studio_storage_files ON image_studio_files (storage_profile_id, id);
