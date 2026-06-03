-- name: GetPerson :one
SELECT * FROM people
WHERE timeline_id = @timeline_id AND slug = @slug;

-- name: GetPersonByID :one
SELECT * FROM people
WHERE id = @id;

-- name: ListPeople :many
SELECT * FROM people
WHERE timeline_id = @timeline_id
ORDER BY sort_name ASC, name ASC;

-- name: CreatePerson :one
INSERT INTO people (timeline_id, slug, name, sort_name, bio, born_date, died_date, photo_url)
VALUES (@timeline_id, @slug, @name, @sort_name, @bio, @born_date, @died_date, @photo_url)
RETURNING *;

-- name: ListEventPeople :many
SELECT p.*, ep.role, ep.explainer, ep.sort_order
FROM people p
JOIN event_people ep ON ep.person_id = p.id
WHERE ep.event_id = @event_id
ORDER BY ep.sort_order ASC, p.name ASC;

-- name: ListPersonEvents :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color,
       ep.role,
       ep.explainer
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
JOIN event_people ep ON ep.event_id = e.id
WHERE ep.person_id = @person_id
ORDER BY e.event_date ASC, e.sort_order ASC;

-- name: UpdatePerson :one
UPDATE people SET
    name = @name,
    sort_name = @sort_name,
    bio = @bio,
    born_date = @born_date,
    died_date = @died_date,
    photo_url = @photo_url,
    updated_at = NOW()
WHERE id = @id AND timeline_id = @timeline_id
RETURNING *;

-- name: DeletePerson :exec
DELETE FROM people WHERE id = @id AND timeline_id = @timeline_id;

-- name: SearchPeople :many
SELECT * FROM people
WHERE timeline_id = @timeline_id
  AND (name ILIKE '%' || @query || '%' OR sort_name ILIKE '%' || @query || '%')
ORDER BY sort_name ASC, name ASC
LIMIT @max_results;

-- name: AddEventPerson :exec
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
VALUES (@event_id, @person_id, @role, @explainer, @sort_order)
ON CONFLICT DO NOTHING;
