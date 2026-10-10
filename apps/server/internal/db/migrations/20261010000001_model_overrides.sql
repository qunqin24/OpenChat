-- +goose Up
-- +goose StatementBegin

-- What an operator has decided about a model, over what models.dev says.
--
-- Kept apart from catalog_models because that table is replaced wholesale on
-- every refresh, and keyed on the model id alone because that is how a model
-- is offered, picked and stored on a conversation: the same id served through
-- three channels is one model to the people using it, with one set of rules.
--
-- Every column is nullable and NULL means "follow the catalogue". So a refresh
-- that corrects a context window reaches every model the operator has not
-- pinned, and resetting a field is writing NULL, not remembering a default.
CREATE TABLE model_overrides
(
    model_id          text PRIMARY KEY,
    kind              text
        CONSTRAINT model_overrides_kind CHECK (kind IN ('text', 'image', 'embedding', 'rerank')),
    reasoning         boolean,
    tool_call         boolean,
    -- Beyond text, which every chat model takes.
    input_modalities  text[]
        CONSTRAINT model_overrides_input_modalities
            CHECK (input_modalities <@ ARRAY ['image', 'audio', 'video', 'pdf']::text[]),
    context_limit     bigint CONSTRAINT model_overrides_context_limit CHECK (context_limit > 0),
    input_limit       bigint CONSTRAINT model_overrides_input_limit CHECK (input_limit > 0),
    output_limit      bigint CONSTRAINT model_overrides_output_limit CHECK (output_limit > 0),
    -- The thinking levels people may choose. NULL offers every level the model
    -- has; an empty array offers none.
    reasoning_efforts text[]
        CONSTRAINT model_overrides_reasoning_efforts
            CHECK (reasoning_efforts <@
                   ARRAY ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max']::text[]),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE model_overrides;
-- +goose StatementEnd
