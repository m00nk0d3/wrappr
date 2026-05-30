DROP TRIGGER IF EXISTS trigger_set_updated_at ON users;
DROP TRIGGER IF EXISTS trigger_set_updated_at ON companies;

DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS companies;

DROP FUNCTION IF EXISTS set_updated_at;
