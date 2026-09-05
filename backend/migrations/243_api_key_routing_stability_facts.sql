-- Complete route-chain stability facts used by offline evaluation. The field
-- is diagnostic only and never participates in the request hot path.

ALTER TABLE routing_attempts
    ADD COLUMN IF NOT EXISTS sticky_broken BOOLEAN NOT NULL DEFAULT FALSE;

