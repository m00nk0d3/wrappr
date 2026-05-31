-- idx_auth_tokens_token_hash is redundant: the UNIQUE constraint on token_hash
-- already causes PostgreSQL to maintain an implicit B-tree index on that column.
DROP INDEX IF EXISTS idx_auth_tokens_token_hash;
