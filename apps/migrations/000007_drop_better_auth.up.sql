-- Identity lives at urbangate (ADR 0009) and membership in members (ADR
-- 0013); nothing reads the Better Auth tables any more.
DROP TABLE IF EXISTS "session";
DROP TABLE IF EXISTS "account";
DROP TABLE IF EXISTS "verification";
DROP TABLE IF EXISTS "user";
