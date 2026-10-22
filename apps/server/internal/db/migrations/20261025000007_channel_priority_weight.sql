-- +goose Up
-- +goose StatementBegin

-- Which channel answers a model several channels offer. `sort_order` said that
-- (ascending), but nothing ever set it - the panel had no field - so the oldest
-- channel always won and the others only stood in when it was switched off.
--
-- In its place, as New API does it: a run goes to the highest `priority` among
-- the channels that offer its model, and among channels of the same priority to
-- one picked at random in proportion to its `weight`. A request that fails before
-- the model has written anything moves to the next channel in that order
-- (agent: failover). Larger priority first, the way an operator says "this one
-- first"; the old order is kept by negating it.
ALTER TABLE channels
    ADD COLUMN priority integer NOT NULL DEFAULT 0,
    ADD COLUMN weight   integer NOT NULL DEFAULT 1
        CONSTRAINT channels_weight_range CHECK (weight BETWEEN 1 AND 1000);

UPDATE channels SET priority = -sort_order WHERE sort_order <> 0;

DROP INDEX IF EXISTS channels_order_idx;
ALTER TABLE channels DROP COLUMN sort_order;
CREATE INDEX channels_priority_idx ON channels (priority DESC, created_at);

-- A channel's name is how an operator tells it from the others - in the list,
-- on a model's card, in the usage log - so two may not share one, ignoring
-- case and the spaces around it. Names that already clash keep the oldest as
-- it is and number the rest: "Relay", "Relay (2)", "Relay (3)".
WITH numbered AS (SELECT id,
                         row_number() OVER (PARTITION BY lower(btrim(name)) ORDER BY created_at, id) AS n
                  FROM channels)
UPDATE channels c
SET name = left(btrim(c.name), 74) || ' (' || numbered.n || ')'
FROM numbered
WHERE numbered.id = c.id
  AND numbered.n > 1;

CREATE UNIQUE INDEX channels_name_lower_idx ON channels (lower(btrim(name)));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS channels_name_lower_idx;
ALTER TABLE channels ADD COLUMN sort_order integer NOT NULL DEFAULT 0;
UPDATE channels SET sort_order = -priority;
DROP INDEX IF EXISTS channels_priority_idx;
ALTER TABLE channels DROP COLUMN weight, DROP COLUMN priority;
CREATE INDEX channels_order_idx ON channels (sort_order, created_at);

-- +goose StatementEnd
