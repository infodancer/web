-- +goose Up
CREATE TABLE IF NOT EXISTS contact_submissions (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	name             TEXT    NOT NULL,
	email            TEXT    NOT NULL,
	subject          TEXT    NOT NULL DEFAULT '',
	message          TEXT    NOT NULL,
	ip               TEXT    NOT NULL DEFAULT '',
	user_agent       TEXT    NOT NULL DEFAULT '',
	created_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	email_status     TEXT    NOT NULL DEFAULT 'pending',
	email_attempts   INTEGER NOT NULL DEFAULT 0,
	email_last_error TEXT    NOT NULL DEFAULT '',
	sent_at          TEXT
);

CREATE INDEX IF NOT EXISTS ix_contact_submissions_status
	ON contact_submissions(email_status);

-- +goose Down
DROP INDEX IF EXISTS ix_contact_submissions_status;
DROP TABLE IF EXISTS contact_submissions;
