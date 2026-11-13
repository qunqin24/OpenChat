-- +goose Up
-- +goose StatementBegin
-- The built-in plugins' descriptions no longer say "不需要账号": whether a
-- plugin needs an account shows in how it is installed (a connect or token
-- step, or none). A server keeps the text it wrote at first start, so the
-- phrase is taken out there too - only the phrase, so a description an
-- administrator rewrote keeps the rest of their words.
UPDATE plugins SET about = replace(about, '。不需要账号；按 arXiv 的要求，', '。按 arXiv 的要求，') WHERE builtin = 'arxiv';
UPDATE plugins SET about = replace(about, '数据来自 Open-Meteo，不需要账号；', '数据来自 Open-Meteo；') WHERE builtin = 'weather';
UPDATE plugins SET about = replace(about, '（台湾、香港）。不需要账号。', '（台湾、香港）。') WHERE builtin = 'wikipedia';
UPDATE plugins SET about = replace(about, 'Hacker News 接口，不需要账号。', 'Hacker News 接口。') WHERE builtin = 'hackernews';
UPDATE plugins SET about = replace(about, '和 arXiv 插件互补：arXiv 有预印本全文，OpenAlex 有引用关系。', '') WHERE builtin = 'openalex';
-- +goose StatementEnd

-- +goose Down
-- Nothing to undo: the phrase said nothing the page does not.
SELECT 1;
