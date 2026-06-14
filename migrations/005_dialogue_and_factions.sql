CREATE TABLE IF NOT EXISTS faction_standings (
    account_id BIGINT NOT NULL,
    faction VARCHAR(64) NOT NULL,
    standing INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, faction)
);

CREATE TABLE IF NOT EXISTS chronicle_choices (
    id SERIAL PRIMARY KEY,
    storyline VARCHAR(128) NOT NULL,
    quest VARCHAR(128) NOT NULL,
    objective VARCHAR(128) NOT NULL,
    choice_id VARCHAR(128) NOT NULL,
    account_id BIGINT NOT NULL,
    decided_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_chronicle_storyline ON chronicle_choices(storyline, quest);
