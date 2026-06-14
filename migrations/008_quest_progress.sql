-- Quest progress persistence for the dynamic scenario service.
-- dss_saveProgress() upserts the journal snapshot per storyline/quest id
-- (dynamic_scenario_service.cpp): INSERT ... ON CONFLICT (quest_id) DO UPDATE.
CREATE TABLE IF NOT EXISTS quest_progress (
    quest_id   VARCHAR(128) PRIMARY KEY,
    data       JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
