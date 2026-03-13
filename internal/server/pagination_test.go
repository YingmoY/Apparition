package server

import (
	"net/http/httptest"
	"testing"
)

func TestParsePaginationDefaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/api?page=0&pageSize=0", nil)
	page, pageSize, offset := parsePagination(req)
	if page != 1 || pageSize != 20 || offset != 0 {
		t.Fatalf("unexpected default pagination: page=%d size=%d offset=%d", page, pageSize, offset)
	}
}

func TestParsePaginationValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api?page=3&pageSize=10", nil)
	page, pageSize, offset := parsePagination(req)
	if page != 3 || pageSize != 10 || offset != 20 {
		t.Fatalf("unexpected parsed pagination: page=%d size=%d offset=%d", page, pageSize, offset)
	}
}
