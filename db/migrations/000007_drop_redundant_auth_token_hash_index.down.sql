-- Restore the explicit index that was dropped in the up migration.
-- Note: the UNIQUE constraint index on token_hash still exists; this recreates
-- the named alias that was present in migration 000006.
CREATE INDEX IF NOT EXISTS idx_auth_tokens_token_hash ON auth_tokens(token_hash);
