package memory

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgtypeTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
