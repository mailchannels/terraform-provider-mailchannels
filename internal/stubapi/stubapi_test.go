package stubapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStubValidatesAgainstSpec(t *testing.T) {
	stub, err := New()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	call := func(method, path, body string, withKey bool) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+BasePath+path, strings.NewReader(body))
		if withKey {
			req.Header.Set("X-Api-Key", "synthetic")
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, _ := call("POST", "/domains/example.test/dkim-keys", `{"selector":"mc1"}`, true); code != 201 {
		t.Fatalf("create DKIM key: got %d", code)
	}
	if code, body := call("GET", "/domains/example.test/dkim-keys?selector=mc1&include_dns_record=true", "", true); code != 200 || !strings.Contains(body, `"name":"mc1._domainkey.example.test"`) {
		t.Fatalf("get DKIM key: %d %s", code, body)
	}
	if code, _ := call("POST", "/sub-account", `{"company_name":"Example","handle":"example1"}`, true); code != 201 {
		t.Fatalf("create sub-account: got %d", code)
	}
	if len(stub.ValidationErrors()) != 0 {
		t.Fatalf("unexpected validation errors: %v", stub.ValidationErrors())
	}

	// Each of these breaks the spec and must be rejected and recorded.
	invalid := []struct{ method, path, body string }{
		{"POST", "/domains/example.test/dkim-keys", `{"selector":"mc2","algorithm":"ed25519"}`},
		{"POST", "/sub-account", `{"handle":"nocompany"}`},
		{"PUT", "/sub-account/example1/limit", `{"sends":-5}`},
		{"POST", "/webhook", ``},
		{"GET", "/no-such-endpoint", ``},
	}
	for _, c := range invalid {
		if code, _ := call(c.method, c.path, c.body, true); code < 400 {
			t.Errorf("%s %s %s: got %d, want an error", c.method, c.path, c.body, code)
		}
	}
	if code, _ := call("GET", "/webhook", "", false); code != 400 {
		t.Errorf("missing X-Api-Key: got %d, want 400", code)
	}
	if got := len(stub.ValidationErrors()); got != len(invalid)+1 {
		t.Errorf("got %d validation errors, want %d: %v", got, len(invalid)+1, stub.ValidationErrors())
	}
}
