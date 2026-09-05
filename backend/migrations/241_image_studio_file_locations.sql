-- Keep track of retained migration copies so deleting a creation can remove
-- both its current objects and the copies left in previous storage profiles.
ALTER TABLE image_studio_files ADD COLUMN IF NOT EXISTS storage_locations JSONB NOT NULL DEFAULT '[]';
UPDATE image_studio_files f SET storage_locations = (
    SELECT jsonb_agg(DISTINCT location) FROM (
        SELECT jsonb_build_object('storage_id', f.storage_profile_id, 'object_key', f.object_key) AS location
        UNION
        SELECT jsonb_build_object('storage_id', c.storage_profile_id, 'object_key',
            COALESCE(p.config->>'prefix', '') || 'assets/' || f.id::text ||
            CASE f.content_type WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/webp' THEN '.webp' ELSE '.png' END)
        FROM image_studio_creations c JOIN image_studio_storage_profiles p ON p.id=c.storage_profile_id
        WHERE c.id=f.creation_id
    ) locations
) WHERE storage_locations='[]'::jsonb;
