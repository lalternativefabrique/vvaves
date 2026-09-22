ALTER TABLE "user" ADD COLUMN IF NOT EXISTS "identityId" TEXT;

CREATE INDEX IF NOT EXISTS user_identity_id_idx ON "user" ("identityId");
