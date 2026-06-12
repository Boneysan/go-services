-- Task 4.2c (character persistence migration): the save-file identity is
-- account_<uid>_<slot>_pdr.bin, so imports need (account_id, slot) as a
-- natural key to be idempotent. source_file/imported_at record provenance
-- for the Step-2 dual-write diff job.

ALTER TABLE characters ADD COLUMN IF NOT EXISTS slot SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE characters ADD COLUMN IF NOT EXISTS source_file TEXT;
ALTER TABLE characters ADD COLUMN IF NOT EXISTS imported_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS characters_account_slot_key
    ON characters (account_id, slot);
