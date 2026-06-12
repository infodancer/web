-- name: GetProfile :one
SELECT * FROM profile_profiles WHERE user_key = $1;

-- name: GetProfileByHandle :one
SELECT * FROM profile_profiles WHERE handle = $1;

-- name: UpsertProfile :one
INSERT INTO profile_profiles (user_key, display_name, bio_md, links, is_public)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_key) DO UPDATE
SET display_name = EXCLUDED.display_name,
    bio_md       = EXCLUDED.bio_md,
    links        = EXCLUDED.links,
    is_public    = EXCLUDED.is_public,
    updated_at   = now()
RETURNING *;

-- name: ListPublicProfiles :many
SELECT * FROM profile_profiles
WHERE is_public AND handle IS NOT NULL
ORDER BY updated_at DESC, user_key DESC
LIMIT $1 OFFSET $2;

-- name: ClaimHandle :execrows
UPDATE profile_profiles
SET handle = $2, updated_at = now()
WHERE user_key = $1 AND handle IS NULL;

-- name: SetHandle :execrows
UPDATE profile_profiles
SET handle = $2, updated_at = now()
WHERE user_key = $1;

-- name: SetAvatarURL :execrows
UPDATE profile_profiles
SET avatar_url = $2, updated_at = now()
WHERE user_key = $1;
