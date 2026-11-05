-- Persist the exact gateway billing identity before sending a probe request.
ALTER TABLE channel_group_probe_records
    ADD COLUMN billing_api_key_id BIGINT REFERENCES api_keys(id),
    ADD COLUMN billing_request_id VARCHAR(255),
    ADD CONSTRAINT channel_group_probe_billing_identity_pair CHECK (
        (billing_api_key_id IS NULL) = (billing_request_id IS NULL)
    );

CREATE UNIQUE INDEX idx_channel_group_probe_billing_identity
    ON channel_group_probe_records (billing_request_id, billing_api_key_id)
    WHERE billing_request_id IS NOT NULL;
