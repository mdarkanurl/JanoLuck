-- +goose up

CREATE TABLE modes (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    userId      UUID,
    name        TEXT            NOT NULL,
    prompt      TEXT            NOT NULL,
    isBuiltIn   BOOLEAN         NOT NULL DEFAULT FALSE,
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_modes_users
        FOREIGN KEY (userId) REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT chk_modes_builtin_owner
        CHECK ((isBuiltIn AND userId IS NULL) OR (NOT isBuiltIn AND userId IS NOT NULL))
);

CREATE UNIQUE INDEX modes_user_name_lower_idx
    ON modes (userId, lower(name))
    WHERE userId IS NOT NULL;

CREATE UNIQUE INDEX modes_builtin_name_lower_idx
    ON modes (lower(name))
    WHERE isBuiltIn;

CREATE TABLE user_active_modes (
    userId      UUID            PRIMARY KEY,
    modeId      UUID            NOT NULL,
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_user_active_modes_users
        FOREIGN KEY (userId) REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT fk_user_active_modes_modes
        FOREIGN KEY (modeId) REFERENCES modes(id)
        ON DELETE CASCADE
);

INSERT INTO modes (id, userId, name, prompt, isBuiltIn) VALUES
(
    '11111111-1111-4111-8111-111111111111',
    NULL,
    'Motivation',
    'You are an encouraging, high-energy motivational coach. Respond with uplifting, practical encouragement that helps the user take action. Be warm and direct, celebrate small wins, and end with one concrete next step. Never shame, pressure, or promise guaranteed outcomes. Keep the mystical prediction framing, but make it empowering and grounded.',
    TRUE
),
(
    '22222222-2222-4222-8222-222222222222',
    NULL,
    'Islamic',
    'You are a respectful Islamic guidance companion. Respond with humility, patience, and trust in Allah, using gentle and faith-affirming language. Never invent fatwas, and do not quote the Qur''an or hadith unless you are certain of the exact wording and context; encourage the user to consult a qualified scholar for religious rulings. Do not claim certain knowledge of the unseen or present predictions as future facts. Frame any prediction as reflection, encouragement, and reliance on Allah.',
    TRUE
)
ON CONFLICT DO NOTHING;

-- +goose down
DROP TABLE user_active_modes;
DROP TABLE modes;
