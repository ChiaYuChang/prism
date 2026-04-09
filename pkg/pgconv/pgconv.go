package pgconv

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func PgTimestamptzToTimePtr(pgt pgtype.Timestamptz) *time.Time {
	if pgt.Valid {
		return &pgt.Time
	}
	return nil
}

func PgUUIDToUUIDPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
}

func PgUUIDToUUID(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func PgTextToStringPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func StringPtrToPgText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}

func TimePtrToPgTimestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

func Int16PtrToPgInt2(v *int16) pgtype.Int2 {
	if v == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: *v, Valid: true}
}

func Int32PtrToPgInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func Int64PtrToPgInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func DurationPtrToPgInterval(v *time.Duration) pgtype.Interval {
	if v == nil {
		return pgtype.Interval{}
	}
	return pgtype.Interval{Microseconds: int64(*v / time.Microsecond), Valid: true}
}

func UUIDPtrToPgUUID(v *uuid.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *v, Valid: true}
}

func UUIDToPgUUID(v uuid.UUID) pgtype.UUID {
	if v == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: v, Valid: true}
}
