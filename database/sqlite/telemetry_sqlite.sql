--
-- SQLite database schema for speedtest telemetry
-- Auto-created by the sqlite backend if it doesn't exist.
-- This file is provided for reference / manual setup.
--

CREATE TABLE IF NOT EXISTS `speedtest_users` (
    `id`        INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    `timestamp` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `ip`        TEXT NOT NULL,
    `ispinfo`   TEXT,
    `extra`     TEXT,
    `ua`        TEXT NOT NULL,
    `lang`      TEXT NOT NULL,
    `dl`        TEXT,
    `ul`        TEXT,
    `ping`      TEXT,
    `jitter`    TEXT,
    `log`       TEXT,
    `uuid`      TEXT
);
