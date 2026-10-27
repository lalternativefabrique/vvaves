ALTER TABLE transcription_job DROP COLUMN IF EXISTS end_user;
ALTER TABLE cost_outbox DROP COLUMN IF EXISTS end_user_id;
