package utils

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"
)

// ToFloat32 converts a float64 slice to a float32 slice.
// Essential for preparing data for pgvector operations.
func ToFloat32(f64 []float64) []float32 {
	f32 := make([]float32, len(f64))
	for i, v := range f64 {
		f32[i] = float32(v)
	}
	return f32
}

// ToPgVector wraps a float32 slice into a pgvector.Vector object.
func ToPgVector(f32 []float32) pgvector.Vector {
	return pgvector.NewVector(f32)
}

// TimeTo provides helpers for converting time.Time to pgtype compatible formats.
var TimeTo = timeToConverter{}

type timeToConverter struct{}

func (t2 timeToConverter) PGTimestamptz(t time.Time) (pgtype.Timestamptz, error) {
	var tsz pgtype.Timestamptz
	if err := tsz.Scan(t); err != nil {
		return pgtype.Timestamptz{}, err
	}
	tsz.Valid = !t.IsZero()
	return tsz, nil
}
