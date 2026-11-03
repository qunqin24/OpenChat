-- +goose Up
-- +goose StatementBegin
-- A plugin's brand icon from Simple Icons (simpleicons.org, CC0): its slug, and
-- the SVG path (in a 24 x 24 box) copied in when an administrator picks it, so
-- no page has to load the icon set to draw it. icon_color carries its colour;
-- with no path, icon_text is drawn instead.
ALTER TABLE plugins
    ADD COLUMN icon_slug text NOT NULL DEFAULT '',
    ADD COLUMN icon_path text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plugins DROP COLUMN icon_path, DROP COLUMN icon_slug;
-- +goose StatementEnd
