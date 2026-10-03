CREATE TABLE reviewed_answers (
 question TEXT NOT NULL,
 options TEXT NOT NULL,
 type TEXT NOT NULL,
 answer TEXT NOT NULL CHECK(length(trim(answer)) > 0),
 source TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(question, options, type)
);
-- Conflicting or explicitly invalidated records stay in answers for review.
INSERT INTO reviewed_answers
 SELECT question, options, type, MIN(answer), 'legacy-reviewed', MIN(created_at), MAX(created_at)
 FROM answers WHERE source='legacy-reviewed' AND invalidated=0
 GROUP BY question, options, type HAVING COUNT(DISTINCT answer)=1;

CREATE TABLE upstream_calls (
 id INTEGER PRIMARY KEY,
 protocol TEXT NOT NULL,
 model TEXT NOT NULL,
 started_at INTEGER NOT NULL,
 duration_ms INTEGER NOT NULL,
 attempt INTEGER NOT NULL,
 http_status INTEGER NOT NULL,
 result TEXT NOT NULL,
 input_tokens INTEGER,
 output_tokens INTEGER,
 CHECK ((input_tokens IS NULL AND output_tokens IS NULL) OR
        (input_tokens IS NOT NULL AND output_tokens IS NOT NULL AND input_tokens >= 0 AND output_tokens >= 0))
);
CREATE INDEX upstream_calls_started ON upstream_calls(started_at);
