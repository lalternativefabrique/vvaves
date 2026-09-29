
CREATE TABLE IF NOT EXISTS "user" (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL UNIQUE,
    "emailVerified" BOOLEAN NOT NULL DEFAULT false,
    image         TEXT,
    role          TEXT NOT NULL DEFAULT 'user',
    banned        BOOLEAN DEFAULT false,
    "banReason"   TEXT,
    "banExpires"  TIMESTAMPTZ,
    "createdAt"   TIMESTAMP NOT NULL DEFAULT NOW(),
    "updatedAt"   TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS "session" (
    id           TEXT PRIMARY KEY,
    "expiresAt"  TIMESTAMP NOT NULL,
    token        TEXT NOT NULL UNIQUE,
    "createdAt"  TIMESTAMP NOT NULL DEFAULT NOW(),
    "updatedAt"  TIMESTAMP NOT NULL DEFAULT NOW(),
    "ipAddress"  TEXT,
    "userAgent"  TEXT,
    "userId"     TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS session_userid_idx ON "session" ("userId");

CREATE TABLE IF NOT EXISTS "account" (
    id                       TEXT PRIMARY KEY,
    "accountId"              TEXT NOT NULL,
    "providerId"             TEXT NOT NULL,
    "userId"                 TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    "accessToken"            TEXT,
    "refreshToken"           TEXT,
    "idToken"                TEXT,
    "accessTokenExpiresAt"   TIMESTAMP,
    "refreshTokenExpiresAt"  TIMESTAMP,
    scope                    TEXT,
    password                 TEXT,
    "createdAt"              TIMESTAMP NOT NULL DEFAULT NOW(),
    "updatedAt"              TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS account_userid_idx ON "account" ("userId");

CREATE TABLE IF NOT EXISTS "verification" (
    id           TEXT PRIMARY KEY,
    identifier   TEXT NOT NULL,
    value        TEXT NOT NULL,
    "expiresAt"  TIMESTAMP NOT NULL,
    "createdAt"  TIMESTAMP NOT NULL DEFAULT NOW(),
    "updatedAt"  TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS verification_identifier_idx ON "verification" (identifier);
-- better-auth's admin plugin keeps the impersonating admin on the session;
-- 1.7.3 checks for the column at boot.
ALTER TABLE "session" ADD COLUMN IF NOT EXISTS "impersonatedBy" TEXT;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS "identityId" TEXT;

CREATE INDEX IF NOT EXISTS user_identity_id_idx ON "user" ("identityId");
