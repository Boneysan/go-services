CREATE TABLE IF NOT EXISTS parties (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS party_members (
    party_id UUID NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    character_id BIGINT NOT NULL,
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (party_id, character_id)
);

CREATE TABLE IF NOT EXISTS party_stash (
    party_id UUID NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    item_sheet_id VARCHAR(255) NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (party_id, item_sheet_id)
);

CREATE TABLE IF NOT EXISTS world_state (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    state_key VARCHAR(255) NOT NULL,
    state_value JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, state_key)
);
