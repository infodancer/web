-- +goose Up
-- +goose StatementBegin

-- embed_recipe records everything besides the model and vector dimension that
-- determines a stored vector: the task prefix, the chunk target, and the header
-- layout rendered into each chunk (DESIGN §5.3). Change one of those and
-- embed_model and embed_dim are unchanged, so the corpus keeps serving
-- old-recipe vectors against new-recipe queries -- strictly worse than either
-- recipe used consistently, and nothing reported it.
--
-- NULL means "not recorded yet", which is the state every store written before
-- this migration is in. The first index run after it adopts the current recipe
-- silently and the next change is the one caught; forcing a re-embed of every
-- existing corpus at upgrade would be worse than detecting one change later.
ALTER TABLE search_index_meta ADD COLUMN embed_recipe text;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE search_index_meta DROP COLUMN IF EXISTS embed_recipe;
-- +goose StatementEnd
