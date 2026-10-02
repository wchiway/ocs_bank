CREATE TABLE answers (
 cache_key TEXT PRIMARY KEY,
 question TEXT NOT NULL,
 options TEXT NOT NULL,
 type TEXT NOT NULL,
 answer TEXT NOT NULL CHECK(length(trim(answer)) > 0),
 source TEXT NOT NULL,
 model TEXT NOT NULL,
 prompt_version TEXT NOT NULL,
 input_tokens INTEGER NOT NULL DEFAULT 0,
 output_tokens INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 invalidated INTEGER NOT NULL DEFAULT 0 CHECK(invalidated IN (0, 1))
);
CREATE INDEX answers_expiry ON answers(expires_at);
