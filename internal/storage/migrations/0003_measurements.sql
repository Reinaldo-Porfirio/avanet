-- Measurements recorded by the background agent (Phase 4).
-- Durations are INTEGER microseconds (_us); rates are REAL megabits per second.

-- A network the computer used. Identity: interface + Wi-Fi name (empty on cable).
CREATE TABLE networks (
    id             INTEGER PRIMARY KEY,
    kind           TEXT    NOT NULL CHECK (kind IN ('ethernet', 'wifi', 'mobile', 'other')),
    interface_name TEXT    NOT NULL,
    ssid           TEXT    NOT NULL DEFAULT '',
    first_seen_ms  INTEGER NOT NULL,
    UNIQUE (kind, interface_name, ssid)
) STRICT;

-- One row per target per latency round. NULL statistics: nothing answered.
CREATE TABLE latency_samples (
    id         INTEGER PRIMARY KEY,
    at_ms      INTEGER NOT NULL,
    network_id INTEGER REFERENCES networks (id),
    target     TEXT    NOT NULL,
    sent       INTEGER NOT NULL CHECK (sent >= 0),
    received   INTEGER NOT NULL CHECK (received BETWEEN 0 AND sent),
    median_us  INTEGER,
    jitter_us  INTEGER,
    min_us     INTEGER,
    max_us     INTEGER
) STRICT;
CREATE INDEX latency_samples_at ON latency_samples (at_ms);

CREATE TABLE wifi_snapshots (
    id             INTEGER PRIMARY KEY,
    at_ms          INTEGER NOT NULL,
    network_id     INTEGER REFERENCES networks (id),
    signal_percent INTEGER NOT NULL CHECK (signal_percent BETWEEN 0 AND 100),
    rssi_dbm       INTEGER NOT NULL,
    channel        INTEGER NOT NULL,
    band           TEXT    NOT NULL,
    standard       TEXT    NOT NULL,
    rx_rate_mbps   INTEGER NOT NULL
) STRICT;
CREATE INDEX wifi_snapshots_at ON wifi_snapshots (at_ms);

CREATE TABLE speed_tests (
    id              INTEGER PRIMARY KEY,
    at_ms           INTEGER NOT NULL,
    network_id      INTEGER REFERENCES networks (id),
    trigger         TEXT    NOT NULL CHECK (trigger IN ('scheduled', 'adaptive', 'manual')),
    server          TEXT    NOT NULL,
    idle_latency_us INTEGER NOT NULL,
    download_mbps   REAL    NOT NULL,
    upload_mbps     REAL    NOT NULL,
    bytes_down      INTEGER NOT NULL,
    bytes_up        INTEGER NOT NULL,
    download_capped INTEGER NOT NULL CHECK (download_capped IN (0, 1)),
    upload_capped   INTEGER NOT NULL CHECK (upload_capped IN (0, 1)),
    local_traffic   INTEGER NOT NULL CHECK (local_traffic IN (0, 1))
) STRICT;
CREATE INDEX speed_tests_at ON speed_tests (at_ms);

-- Periods without internet. ended_ms is NULL while ongoing.
CREATE TABLE outages (
    id         INTEGER PRIMARY KEY,
    network_id INTEGER REFERENCES networks (id),
    started_ms INTEGER NOT NULL,
    ended_ms   INTEGER CHECK (ended_ms IS NULL OR ended_ms >= started_ms)
) STRICT;

-- Continuous periods in which the agent was running: the basis of "coverage".
-- A new row starts at every launch and after sleep/hibernation.
CREATE TABLE agent_sessions (
    id         INTEGER PRIMARY KEY,
    started_ms INTEGER NOT NULL,
    last_ms    INTEGER NOT NULL CHECK (last_ms >= started_ms)
) STRICT;
CREATE INDEX agent_sessions_last ON agent_sessions (last_ms);
