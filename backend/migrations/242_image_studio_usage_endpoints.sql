-- Studio wraps the Images API; keep usage reports on its canonical endpoints.
-- Before normalization recognized Studio, the OpenAI fallback incorrectly
-- recorded /v1/responses. Only repair that fallback and wrapper paths.
UPDATE usage_logs
SET inbound_endpoint = CASE inbound_endpoint
        WHEN '/v1/images/studio/generations' THEN '/v1/images/generations'
        WHEN '/v1/images/studio/edits' THEN '/v1/images/edits'
    END,
    upstream_endpoint = CASE
        WHEN upstream_endpoint IS NULL OR btrim(upstream_endpoint) IN (
            '', '/v1/responses', '/v1/images/studio/generations', '/v1/images/studio/edits'
        ) THEN CASE inbound_endpoint
            WHEN '/v1/images/studio/generations' THEN '/v1/images/generations'
            WHEN '/v1/images/studio/edits' THEN '/v1/images/edits'
        END
        ELSE upstream_endpoint
    END
WHERE inbound_endpoint IN ('/v1/images/studio/generations', '/v1/images/studio/edits');
