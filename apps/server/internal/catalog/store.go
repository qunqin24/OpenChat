package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// stateKey is the app_settings row the refresh's own bookkeeping lives in. One
// row rather than a table: it is a single object with three fields and no
// history worth keeping.
const stateKey = "catalog_state"

// Store is the database as a sync sees it.
//
// A sync reaches the database only through this interface, and the interface
// has no method that can write model_settings. That is the point: switching a
// model on is this installation's decision, and replacing a cache of somebody
// else's file is not an occasion to revisit it.
type Store interface {
	LoadState(ctx context.Context) (State, error)
	SaveState(ctx context.Context, state State) error
	Replace(ctx context.Context, providers []Provider) error
}

// PgStore is the Postgres implementation of Store.
type PgStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

var _ Store = (*PgStore)(nil)

func NewStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool, q: sqlc.New(pool)}
}

// LoadState reads the bookkeeping. A database that has never fetched the
// catalogue has no row, which reads as the zero state and therefore as stale.
func (s *PgStore) LoadState(ctx context.Context) (State, error) {
	raw, err := s.q.GetSetting(ctx, stateKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return State{}, nil
		}
		return State{}, err
	}

	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *PgStore) SaveState(ctx context.Context, state State) error {
	value, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: stateKey, Value: value})
}

// Replace swaps the whole snapshot inside one transaction, so a reader sees
// either the old catalogue or the new one and never the gap between them.
//
// Delete and reinsert rather than upsert with a prune: the upstream file is the
// whole truth every time it arrives, and at a few thousand rows working out
// which ones changed costs more than rewriting all of them. model_settings is
// not in this transaction at all, so a model that vanishes upstream and comes
// back a week later comes back with the switch the operator left it on.
func (s *PgStore) Replace(ctx context.Context, providers []Provider) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	if err := q.DeleteCatalogModels(ctx); err != nil {
		return err
	}
	if err := q.DeleteCatalogProviders(ctx); err != nil {
		return err
	}

	providerRows := make([]sqlc.InsertCatalogProvidersParams, 0, len(providers))
	modelRows := make([]sqlc.InsertCatalogModelsParams, 0, len(providers)*32)

	for _, provider := range providers {
		providerRows = append(providerRows, sqlc.InsertCatalogProvidersParams{
			ID:   provider.ID,
			Name: provider.Name,
			Doc:  provider.Doc,
			Env:  list(provider.Env),
		})

		for _, model := range provider.Models {
			row, err := modelRow(provider.ID, model)
			if err != nil {
				return err
			}
			modelRows = append(modelRows, row)
		}
	}

	if _, err := q.InsertCatalogProviders(ctx, providerRows); err != nil {
		return err
	}
	if _, err := q.InsertCatalogModels(ctx, modelRows); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func modelRow(providerID string, model Model) (sqlc.InsertCatalogModelsParams, error) {
	inputCost, err := numeric(model.InputCost)
	if err != nil {
		return sqlc.InsertCatalogModelsParams{}, err
	}
	outputCost, err := numeric(model.OutputCost)
	if err != nil {
		return sqlc.InsertCatalogModelsParams{}, err
	}

	raw := model.Raw
	if len(raw) == 0 {
		raw = []byte("{}")
	}

	return sqlc.InsertCatalogModelsParams{
		ProviderID:       providerID,
		ModelID:          model.ID,
		Name:             model.Name,
		Description:      model.Description,
		Family:           model.Family,
		Attachment:       model.Attachment,
		Reasoning:        model.Reasoning,
		ToolCall:         model.ToolCall,
		StructuredOutput: model.StructuredOutput,
		ContextLimit:     model.ContextLimit,
		OutputLimit:      model.OutputLimit,
		InputCost:        inputCost,
		OutputCost:       outputCost,
		ReleaseDate:      model.ReleaseDate,
		Knowledge:        model.Knowledge,
		InputModalities:  list(model.InputModalities),
		OutputModalities: list(model.OutputModalities),
		Raw:              raw,
	}, nil
}

// list turns an absent slice into an empty one. COPY sends a nil slice as NULL,
// which the NOT NULL array columns refuse, and "upstream said nothing" means an
// empty list here rather than an unknown one.
func list(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// numeric converts a cost to the column's type through its text form, so the
// decimal upstream stated is the decimal that gets stored rather than the
// nearest binary float to it.
func numeric(value float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(value, 'f', -1, 64)); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}
