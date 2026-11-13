-- +goose Up
-- +goose StatementBegin
-- Speech input (docs/design/speech-input-plan.md): the composer's microphone,
-- turned into text by a transcription model.

-- A model that turns speech into text is a kind of its own: not offered in
-- chat, chosen on the 语音输入 page.
ALTER TABLE model_overrides DROP CONSTRAINT model_overrides_kind;
ALTER TABLE model_overrides
    ADD CONSTRAINT model_overrides_kind CHECK (kind IN ('text', 'image', 'embedding', 'rerank', 'transcription'));

-- USD per minute of audio, set by an administrator: transcription is billed
-- by the minute (gpt-transcribe), which the catalogue has no figure for. Wins
-- over token prices.
ALTER TABLE model_overrides ADD COLUMN audio_price numeric CHECK (audio_price >= 0);

-- A transcription in the ledger: the seconds of audio it was billed for and
-- the price per minute, when it was billed by the minute.
ALTER TABLE usage_logs ADD COLUMN audio_seconds numeric;
ALTER TABLE usage_logs ADD COLUMN audio_price numeric;
ALTER TABLE usage_logs DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation', 'subagent', 'image', 'transcription'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM usage_logs WHERE kind = 'transcription';
ALTER TABLE usage_logs DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation', 'subagent', 'image'));
ALTER TABLE usage_logs DROP COLUMN audio_price;
ALTER TABLE usage_logs DROP COLUMN audio_seconds;
ALTER TABLE model_overrides DROP COLUMN audio_price;
UPDATE model_overrides SET kind = NULL WHERE kind = 'transcription';
ALTER TABLE model_overrides DROP CONSTRAINT model_overrides_kind;
ALTER TABLE model_overrides
    ADD CONSTRAINT model_overrides_kind CHECK (kind IN ('text', 'image', 'embedding', 'rerank'));
-- +goose StatementEnd
