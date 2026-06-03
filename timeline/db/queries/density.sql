-- name: GetEventDensityByYear :many
SELECT date_trunc('year', event_date)::date AS bucket_date,
       COUNT(*) AS event_count
FROM events
WHERE timeline_id = @timeline_id
GROUP BY bucket_date
ORDER BY bucket_date ASC;

-- name: GetEventDensityByMonth :many
SELECT date_trunc('month', event_date)::date AS bucket_date,
       COUNT(*) AS event_count
FROM events
WHERE timeline_id = @timeline_id
GROUP BY bucket_date
ORDER BY bucket_date ASC;
