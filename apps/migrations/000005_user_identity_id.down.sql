DROP INDEX IF EXISTS user_identity_id_idx;

ALTER TABLE "user" DROP COLUMN IF EXISTS "identityId";
