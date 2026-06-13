-- Task 5.5.6 (campaign export/import): the export bundle is anchored on a
-- campaign row. A private shard runs a single campaign, so a fresh database is
-- seeded with one default campaign (well-known id) and `campaign-api`
-- export/import works out of the box. Idempotent — safe to re-run.

CREATE TABLE IF NOT EXISTS campaigns (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    sessions_played INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Default single-shard campaign. The fixed UUID lets docs/RECOVERY.md and the
-- GM dashboard reference it without a lookup.
INSERT INTO campaigns (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'My Campaign')
ON CONFLICT (id) DO NOTHING;
