DROP INDEX IF EXISTS idx_portfolios_owner_user_id_created;
ALTER TABLE portfolios
    DROP COLUMN IF EXISTS owner_user_id;

DROP INDEX IF EXISTS idx_user_sessions_user_id;
DROP TABLE IF EXISTS user_sessions;

DROP INDEX IF EXISTS uq_users_work_email_ci;
DROP TABLE IF EXISTS users;

