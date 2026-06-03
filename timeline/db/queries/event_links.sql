-- name: ListLinkedEvents :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color,
       el.relationship,
       el.description AS link_description
FROM timeline_event_links el
JOIN timeline_events e ON (e.id = CASE WHEN el.event_a_id = @event_id THEN el.event_b_id ELSE el.event_a_id END)
LEFT JOIN timeline_categories c ON c.id = e.category_id
WHERE el.event_a_id = @event_id OR el.event_b_id = @event_id
ORDER BY e.event_date ASC;

-- name: DeleteEventLink :exec
DELETE FROM timeline_event_links
WHERE event_a_id = @event_a_id AND event_b_id = @event_b_id;

-- name: DeleteEventPerson :exec
DELETE FROM timeline_event_people
WHERE event_id = @event_id AND person_id = @person_id;

-- name: AddEventLink :exec
INSERT INTO timeline_event_links (event_a_id, event_b_id, relationship, description)
VALUES (@event_a_id, @event_b_id, @relationship, @description)
ON CONFLICT DO NOTHING;
