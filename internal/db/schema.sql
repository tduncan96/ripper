CREATE TABLE IF NOT EXISTS Runs (
    RunID INTEGER PRIMARY KEY AUTOINCREMENT,
    StartTime TEXT NOT NULL DEFAULT (DATETIME('now')),
    EndTime TEXT NOT NULL DEFAULT (DATETIME('now')),
    ExitCode INTEGER,
    Device TEXT,
    Title TEXT,
    RawTitle TEXT,
    Destination TEXT,
    TotalRipMB INTEGER,
    TotalMvMB INTEGER,
    RipLog TEXT
)