ALTER TABLE chronicle_choices ADD CONSTRAINT chronicle_choices_unique_choice UNIQUE (storyline, quest, objective, account_id);
