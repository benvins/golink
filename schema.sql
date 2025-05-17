CREATE TABLE IF NOT EXISTS Links (
	ID       TEXT    PRIMARY KEY,         -- normalized version of Short (foobar)
	Short    TEXT    NOT NULL DEFAULT '',
	Long     TEXT    NOT NULL DEFAULT '',
	Created  INTEGER NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW())), -- unix seconds
	LastEdit INTEGER NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW())), -- unix seconds
	Owner	 TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS Stats (
	ID       TEXT    NOT NULL DEFAULT '',
	Created  INTEGER NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW())), -- unix seconds
	Clicks   INTEGER
);
