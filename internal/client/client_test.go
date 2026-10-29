package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/stubapi"
)

func TestGetSubAccountPaginates(t *testing.T) {
	stub, err := stubapi.New()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	c := New(srv.URL+stubapi.BasePath, "synthetic", "test")
	ctx := context.Background()
	for i := range pageSize + 1 {
		if _, err := c.CreateSubAccount(ctx, CreateSubAccountRequest{CompanyName: "Example", Handle: fmt.Sprintf("acct%04d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	last := fmt.Sprintf("acct%04d", pageSize)
	got, err := c.GetSubAccount(ctx, last)
	if err != nil || got == nil || got.Handle != last {
		t.Fatalf("GetSubAccount(%s) = %v, %v", last, got, err)
	}
	if got, err := c.GetSubAccount(ctx, "missing"); err != nil || got != nil {
		t.Fatalf("GetSubAccount(missing) = %v, %v; want nil, nil", got, err)
	}
	if errs := stub.ValidationErrors(); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "synthetic" {
			t.Errorf("X-Api-Key = %q", r.Header.Get("X-Api-Key"))
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":["no such key","try again"]}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "synthetic", "test").UpdateDKIMKeyStatus(context.Background(), "example.test", "mc1", "retired")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound(%v) = false", err)
	}
	want := "PATCH /domains/example.test/dkim-keys/mc1: HTTP 404: no such key; try again"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}
