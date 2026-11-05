-- Serialize cleanup and index creation with writers, including older application versions.
LOCK TABLE channel_group_probe_records IN SHARE ROW EXCLUSIVE MODE;

UPDATE channel_group_probe_records
SET status = 'failed', error_message = '检测任务已中断', completed_at = NOW()
WHERE status = 'running' AND created_at < NOW() - INTERVAL '5 minutes';

WITH duplicates AS (
    SELECT id, row_number() OVER (PARTITION BY group_id, user_id ORDER BY created_at DESC, id DESC) AS position
    FROM channel_group_probe_records WHERE status = 'running'
)
UPDATE channel_group_probe_records p
SET status = 'failed', error_message = '检测任务已中断', completed_at = NOW()
FROM duplicates d WHERE p.id = d.id AND d.position > 1;

CREATE UNIQUE INDEX channel_group_probe_one_running_per_user_group
ON channel_group_probe_records (group_id, user_id) WHERE status = 'running';
