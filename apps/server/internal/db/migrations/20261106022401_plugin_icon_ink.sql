-- +goose Up
-- +goose StatementBegin
-- The colour a plugin's glyph is drawn in, for a brand whose mark is not the
-- dark-or-white the background would choose: McDonald's yellow arches on its
-- red. Empty keeps that choice.
ALTER TABLE plugins ADD COLUMN icon_ink text NOT NULL DEFAULT '';

-- Two built-ins as they first shipped, put right where no administrator
-- changed them: Notion's mark is a black outline on white, not reversed on
-- black, and McDonald's arches are yellow on red.
UPDATE plugins
SET icon_color = '#FFFFFF'
WHERE builtin = 'notion'
  AND icon_color = '#000000';
UPDATE plugins
SET icon_color = '#DA291C',
    icon_ink   = '#FFC72C'
WHERE builtin = 'mcdonalds'
  AND icon_color = '#FBC817';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plugins DROP COLUMN icon_ink;
-- +goose StatementEnd
