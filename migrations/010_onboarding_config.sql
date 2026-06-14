-- Task 5.8-D (mid-campaign onboarding): when the GM uses the OnboardingWizard to
-- parachute a new player into an active campaign the desired starting conditions
-- (level, gold, spawn location) need to survive until the player's first EGS login.
-- EGS reads this row on ENTITY_ENTRY, applies the settings, then removes it.

CREATE TABLE IF NOT EXISTS character_onboarding_config (
    character_id    UUID PRIMARY KEY REFERENCES characters(id) ON DELETE CASCADE,
    campaign_id     UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    starting_level  SMALLINT NOT NULL DEFAULT 1,
    starting_gold   INTEGER  NOT NULL DEFAULT 0,
    spawn_location  TEXT     NOT NULL DEFAULT 'starting_zone',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at      TIMESTAMPTZ   -- set by EGS once applied; NULL = pending
);
