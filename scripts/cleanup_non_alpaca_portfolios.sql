-- Remove catalog portfolios that are not linked to an Alpaca account.
-- Keeps rows with alpaca_key_id OR alpaca_account_id set.
-- Child rows (sync state, proposals, agent sessions) cascade via FK.

BEGIN;

SELECT portfolio_id, name, owner_user_id, alpaca_account_id
FROM portfolios
WHERE COALESCE(NULLIF(TRIM(alpaca_key_id), ''), '') = ''
  AND COALESCE(NULLIF(TRIM(alpaca_account_id), ''), '') = '';

DELETE FROM portfolios
WHERE COALESCE(NULLIF(TRIM(alpaca_key_id), ''), '') = ''
  AND COALESCE(NULLIF(TRIM(alpaca_account_id), ''), '') = '';

SELECT portfolio_id, name, alpaca_account_id
FROM portfolios
ORDER BY created_at;

COMMIT;
