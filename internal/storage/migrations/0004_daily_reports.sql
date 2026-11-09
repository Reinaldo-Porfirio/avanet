-- Daily reports. summary_json is owned by internal/analysis; `version` tells
-- when a stored report must be rebuilt because its format changed.
CREATE TABLE daily_reports (
    day             TEXT    PRIMARY KEY CHECK (day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    profile         TEXT    NOT NULL,
    version         INTEGER NOT NULL,
    summary_json    TEXT    NOT NULL,
    generated_at_ms INTEGER NOT NULL,
    notified_at_ms  INTEGER
) STRICT, WITHOUT ROWID;

-- Reports look up the outages and sessions overlapping a day.
CREATE INDEX outages_started ON outages (started_ms);
CREATE INDEX agent_sessions_started ON agent_sessions (started_ms);
