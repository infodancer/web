-- name: InsertSubmission :one
INSERT INTO contact_submissions (name, email, subject, message, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at;

-- name: MarkSent :exec
UPDATE contact_submissions
SET email_status = 'sent',
    sent_at = NOW(),
    email_attempts = email_attempts + 1
WHERE id = $1;

-- name: MarkFailed :exec
UPDATE contact_submissions
SET email_status = 'failed',
    email_attempts = email_attempts + 1,
    email_last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id);

-- name: PendingSubmissions :many
SELECT id, name, email, subject, message, ip, user_agent, created_at
FROM contact_submissions
WHERE email_status IN ('pending', 'failed')
  AND email_attempts < sqlc.arg(max_attempts)
ORDER BY id ASC
LIMIT sqlc.arg(row_limit);
