-- Conventions for every table:
--   * STRICT tables, so SQLite enforces column types;
--   * timestamps are INTEGER Unix milliseconds in UTC, suffixed _ms
--     (compact, fast range queries; converted to local time only for display).

-- User preferences (profile, data budget, ...). Values are validated by the
-- application before being written.
CREATE TABLE settings (
    key           TEXT    PRIMARY KEY,
    value         TEXT    NOT NULL,
    updated_at_ms INTEGER NOT NULL
) STRICT;
