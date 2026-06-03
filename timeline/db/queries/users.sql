-- name: UpsertUser :one
INSERT INTO timeline_users (sub_hash, display_name)
VALUES (@sub_hash, @display_name)
ON CONFLICT (sub_hash) DO UPDATE
    SET display_name = EXCLUDED.display_name,
        last_seen_at = now()
RETURNING *;

-- name: SetUserAdmin :exec
UPDATE timeline_users SET is_admin = @is_admin WHERE id = @id;
