-- +goose up

ALTER TABLE modes
    ADD COLUMN moderation_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN moderation_reason TEXT;

ALTER TABLE chats
    ADD COLUMN moderation_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN moderation_reason TEXT;

ALTER TABLE modes
    ADD CONSTRAINT chk_modes_moderation_status
    CHECK (moderation_status IN ('pending', 'allowed', 'blocked'));

ALTER TABLE chats
    ADD CONSTRAINT chk_chats_moderation_status
    CHECK (moderation_status IN ('pending', 'allowed', 'blocked'));

-- +goose down
ALTER TABLE chats DROP CONSTRAINT chk_chats_moderation_status;
ALTER TABLE chats DROP COLUMN moderation_reason, DROP COLUMN moderation_status;
ALTER TABLE modes DROP CONSTRAINT chk_modes_moderation_status;
ALTER TABLE modes DROP COLUMN moderation_reason, DROP COLUMN moderation_status;
