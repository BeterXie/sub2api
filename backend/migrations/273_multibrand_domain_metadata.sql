-- Operational observations are entered manually; they never enable a domain.
ALTER TABLE domains ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
