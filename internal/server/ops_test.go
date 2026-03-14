package server

import (
	"database/sql"
	"testing"
)

func TestNullableInt64Nil(t *testing.T) {
	if nullableInt64(sql.NullInt64{Valid: false}) != nil {
		t.Fatal("expected nil for invalid int64")
	}
}
