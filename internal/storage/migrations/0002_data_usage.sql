-- Bytes consumed by AvaNet's own speed tests, per local calendar day and data
-- class, so the daily budget survives restarts. One row per (day, class).
CREATE TABLE data_usage (
    day        TEXT    NOT NULL CHECK (day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    data_class TEXT    NOT NULL CHECK (data_class IN ('fixed', 'mobile')),
    bytes      INTEGER NOT NULL CHECK (bytes >= 0),
    PRIMARY KEY (day, data_class)
) STRICT, WITHOUT ROWID;
