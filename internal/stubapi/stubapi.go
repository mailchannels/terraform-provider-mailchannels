// Package stubapi is a stateful, in-memory stand-in for the MailChannels
// Email API. It validates every request and response against the public
// OpenAPI spec (openapi.yaml, copied from transactional-api), so the
// provider's acceptance tests fail when the provider sends, or relies on,
// something the real API does not document.
package stubapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

//go:embed openapi.yaml
var spec []byte

// BasePath is the path prefix of the Email API, as in DefaultBaseURL.
const BasePath = "/tx/v1"

// Request is one API call the stub received.
type Request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query,omitempty"`
	Status int    `json:"status"`
}

type dkimKey struct {
	Domain, Selector, Algorithm, PublicKey, Status string
	KeyLength                                      int64
	CreatedAt, StatusModifiedAt                    time.Time
}

type subAccount struct {
	Handle, CompanyName string
	Enabled             bool
	Limit               int64
}

type suppression struct {
	Recipient, Source string
	Types             []string
	Notes             *string
	CreatedAt         time.Time
}

// Server is the stub. Create it with New and serve it with net/http.
type Server struct {
	router routers.Router

	mu               sync.Mutex
	requests         []Request
	validationErrors []string
	dkim             map[string]*dkimKey // domain/selector
	webhooks         []string
	subAccounts      map[string]*subAccount
	subAccountOrder  []string
	suppressions     map[string]*suppression // recipient/source
	nextHandle       int
}

// New loads the embedded OpenAPI spec and returns an empty stub.
func New() (*Server, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("loading OpenAPI spec: %w", err)
	}
	// Match paths without the server URL; ServeHTTP strips BasePath.
	doc.Servers = nil
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("building router: %w", err)
	}
	s := &Server{router: router}
	s.Reset()
	return s, nil
}

// Reset clears all state and the request log.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
	s.validationErrors = nil
	s.dkim = map[string]*dkimKey{}
	s.webhooks = nil
	s.subAccounts = map[string]*subAccount{}
	s.subAccountOrder = nil
	s.suppressions = map[string]*suppression{}
}

// Requests returns a copy of the request log.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// ValidationErrors returns every request or response that did not match
// the OpenAPI spec.
func (s *Server) ValidationErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.validationErrors...)
}

// State is a snapshot of the stub's resources, for assertions.
type State struct {
	DKIMKeys     map[string]string `json:"dkim_keys"` // domain/selector -> status
	Webhooks     []string          `json:"webhooks"`
	SubAccounts  map[string]int64  `json:"sub_accounts"` // handle -> limit
	Suppressions []string          `json:"suppressions"` // recipient/source
}

// Snapshot returns the current state.
func (s *Server) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := State{DKIMKeys: map[string]string{}, Webhooks: append([]string{}, s.webhooks...), SubAccounts: map[string]int64{}, Suppressions: []string{}}
	for k, v := range s.dkim {
		st.DKIMKeys[k] = v.Status
	}
	for k, v := range s.subAccounts {
		st.SubAccounts[k] = v.Limit
	}
	for k := range s.suppressions {
		st.Suppressions = append(st.Suppressions, k)
	}
	sort.Strings(st.Suppressions)
	return st
}

// ServeHTTP serves the Email API under BasePath, plus /__stub/requests,
// /__stub/state and /__stub/reset for test harnesses.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/__stub/requests":
		writeJSON(w, http.StatusOK, map[string]any{"requests": s.Requests(), "validation_errors": s.ValidationErrors()})
		return
	case "/__stub/state":
		writeJSON(w, http.StatusOK, s.Snapshot())
		return
	case "/__stub/reset":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.Reset()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !strings.HasPrefix(r.URL.Path, BasePath+"/") {
		http.NotFound(w, r)
		return
	}

	body, _ := io.ReadAll(r.Body)
	apiReq := r.Clone(r.Context())
	apiReq.URL.Path = strings.TrimPrefix(r.URL.Path, BasePath)
	apiReq.URL.RawPath = ""
	apiReq.Body = io.NopCloser(bytes.NewReader(body))

	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	s.serveAPI(rec, apiReq, body)

	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: apiReq.URL.Path, Query: r.URL.RawQuery, Status: rec.status})
	s.mu.Unlock()

	for k, v := range rec.header {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body.Bytes())
}

func (s *Server) recordValidationError(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	s.mu.Lock()
	s.validationErrors = append(s.validationErrors, msg)
	s.mu.Unlock()
	return msg
}

func (s *Server) serveAPI(w *recorder, r *http.Request, body []byte) {
	route, params, err := s.router.FindRoute(r)
	if err != nil {
		msg := s.recordValidationError("%s %s: no such operation in the OpenAPI spec: %v", r.Method, r.URL.Path, err)
		writeErrors(w, http.StatusNotFound, msg)
		return
	}
	in := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: params,
		Route:      route,
		Options:    &openapi3filter.Options{MultiError: true},
	}
	ctx := context.Background()
	if err := openapi3filter.ValidateRequest(ctx, in); err != nil {
		msg := s.recordValidationError("%s %s: request does not match the OpenAPI spec: %v", r.Method, r.URL.Path, err)
		writeErrors(w, http.StatusBadRequest, msg)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	s.mu.Lock()
	s.handle(w, r, route.Path, params, body)
	s.mu.Unlock()

	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 w.status,
		Header:                 w.header,
		Options:                &openapi3filter.Options{MultiError: true, IncludeResponseStatus: true},
	}
	out.SetBodyBytes(w.body.Bytes())
	if err := openapi3filter.ValidateResponse(ctx, out); err != nil {
		msg := s.recordValidationError("%s %s: stub response %d does not match the OpenAPI spec: %v", r.Method, r.URL.Path, w.status, err)
		w.reset()
		writeErrors(w, http.StatusInternalServerError, msg)
	}
}

// handle runs with s.mu held.
func (s *Server) handle(w *recorder, r *http.Request, route string, p map[string]string, body []byte) {
	q := r.URL.Query()
	op := r.Method + " " + route
	switch op {
	case "POST /domains/{domain}/dkim-keys":
		var in struct {
			Selector  string `json:"selector"`
			Algorithm string `json:"algorithm"`
			KeyLength int64  `json:"key_length"`
		}
		_ = json.Unmarshal(body, &in)
		if in.Algorithm == "" {
			in.Algorithm = "rsa"
		}
		if in.KeyLength == 0 {
			in.KeyLength = 2048
		}
		if in.KeyLength%1024 != 0 {
			writeErrors(w, http.StatusBadRequest, "key_length must be a multiple of 1024")
			return
		}
		id := p["domain"] + "/" + in.Selector
		if _, ok := s.dkim[id]; ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		pub, err := publicKey(int(in.KeyLength))
		if err != nil {
			writeErrors(w, http.StatusInternalServerError, err.Error())
			return
		}
		now := time.Now().UTC().Truncate(time.Second)
		k := &dkimKey{Domain: p["domain"], Selector: in.Selector, Algorithm: in.Algorithm, KeyLength: in.KeyLength, PublicKey: pub, Status: "active", CreatedAt: now, StatusModifiedAt: now}
		s.dkim[id] = k
		writeJSON(w, http.StatusCreated, k.json(false))

	case "GET /domains/{domain}/dkim-keys":
		keys := []map[string]any{}
		ids := sortedKeys(s.dkim)
		for _, id := range ids {
			k := s.dkim[id]
			if k.Domain != p["domain"] || (q.Get("selector") != "" && k.Selector != q.Get("selector")) || (q.Get("status") != "" && k.Status != q.Get("status")) {
				continue
			}
			keys = append(keys, k.json(q.Get("include_dns_record") == "true"))
		}
		keys = paginate(keys, q, 10)
		writeJSON(w, http.StatusOK, map[string]any{"keys": keys})

	case "PATCH /domains/{domain}/dkim-keys/{selector}":
		var in struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(body, &in)
		k, ok := s.dkim[p["domain"]+"/"+p["selector"]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if in.Status == "rotated" && k.Status != "active" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		k.Status = in.Status
		k.StatusModifiedAt = time.Now().UTC().Truncate(time.Second)
		w.WriteHeader(http.StatusNoContent)

	case "GET /webhook":
		out := []map[string]string{}
		for _, e := range s.webhooks {
			out = append(out, map[string]string{"webhook": e})
		}
		writeJSON(w, http.StatusOK, out)

	case "POST /webhook":
		endpoint := q.Get("endpoint")
		if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, e := range s.webhooks {
			if e == endpoint {
				w.WriteHeader(http.StatusConflict)
				return
			}
		}
		s.webhooks = append(s.webhooks, endpoint)
		w.WriteHeader(http.StatusCreated)

	case "DELETE /webhook":
		s.webhooks = nil
		w.WriteHeader(http.StatusNoContent)

	case "POST /sub-account":
		var in struct {
			CompanyName string `json:"company_name"`
			Handle      string `json:"handle"`
		}
		_ = json.Unmarshal(body, &in)
		if len(in.CompanyName) < 3 || len(in.CompanyName) > 128 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if in.Handle == "" {
			s.nextHandle++
			in.Handle = fmt.Sprintf("generated%06d", s.nextHandle)
		}
		if !validHandle(in.Handle) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, ok := s.subAccounts[in.Handle]; ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		a := &subAccount{Handle: in.Handle, CompanyName: in.CompanyName, Enabled: true, Limit: -1}
		s.subAccounts[a.Handle] = a
		s.subAccountOrder = append(s.subAccountOrder, a.Handle)
		writeJSON(w, http.StatusCreated, a.json())

	case "GET /sub-account":
		out := []map[string]any{}
		for _, h := range s.subAccountOrder {
			out = append(out, s.subAccounts[h].json())
		}
		writeJSON(w, http.StatusOK, paginate(out, q, 1000))

	case "DELETE /sub-account/{handle}":
		delete(s.subAccounts, p["handle"])
		order := s.subAccountOrder[:0]
		for _, h := range s.subAccountOrder {
			if h != p["handle"] {
				order = append(order, h)
			}
		}
		s.subAccountOrder = order
		w.WriteHeader(http.StatusNoContent)

	case "POST /sub-account/{handle}/activate", "POST /sub-account/{handle}/suspend":
		a, ok := s.subAccounts[p["handle"]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		a.Enabled = strings.HasSuffix(route, "/activate")
		w.WriteHeader(http.StatusNoContent)

	case "GET /sub-account/{handle}/limit", "PUT /sub-account/{handle}/limit", "DELETE /sub-account/{handle}/limit":
		a, ok := s.subAccounts[p["handle"]]
		if !ok {
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]int64{"sends": a.Limit})
		case http.MethodPut:
			var in struct {
				Sends int64 `json:"sends"`
			}
			_ = json.Unmarshal(body, &in)
			a.Limit = in.Sends
			writeJSON(w, http.StatusOK, map[string]any{"limit": map[string]int64{"sends": a.Limit}})
		default:
			a.Limit = -1
			w.WriteHeader(http.StatusNoContent)
		}

	case "POST /suppression-list":
		var in struct {
			Entries []struct {
				Recipient string   `json:"recipient"`
				Types     []string `json:"suppression_types"`
				Notes     *string  `json:"notes"`
			} `json:"suppression_entries"`
		}
		_ = json.Unmarshal(body, &in)
		for _, e := range in.Entries {
			if _, ok := s.suppressions[strings.ToLower(e.Recipient)+"/api"]; ok {
				w.WriteHeader(http.StatusConflict)
				return
			}
		}
		for _, e := range in.Entries {
			types := e.Types
			if len(types) == 0 {
				types = []string{"non-transactional"}
			}
			s.suppressions[strings.ToLower(e.Recipient)+"/api"] = &suppression{Recipient: strings.ToLower(e.Recipient), Source: "api", Types: types, Notes: e.Notes, CreatedAt: time.Now().UTC().Truncate(time.Second)}
		}
		w.WriteHeader(http.StatusCreated)

	case "GET /suppression-list":
		out := []map[string]any{}
		for _, id := range sortedKeys(s.suppressions) {
			e := s.suppressions[id]
			if (q.Get("recipient") != "" && e.Recipient != strings.ToLower(q.Get("recipient"))) || (q.Get("source") != "" && e.Source != q.Get("source")) {
				continue
			}
			out = append(out, map[string]any{"recipient": e.Recipient, "source": e.Source, "suppression_types": e.Types, "notes": e.Notes, "created_at": e.CreatedAt.Format(time.RFC3339)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"suppression_list": paginate(out, q, 1000)})

	case "DELETE /suppression-list/recipients/{recipient}":
		source := q.Get("source")
		if source == "" {
			source = "api"
		}
		recipient := strings.ToLower(p["recipient"])
		for id, e := range s.suppressions {
			if e.Recipient == recipient && (source == "all" || e.Source == source) {
				delete(s.suppressions, id)
			}
		}
		w.WriteHeader(http.StatusNoContent)

	case "POST /check-domain":
		var in struct {
			Domain string `json:"domain"`
		}
		_ = json.Unmarshal(body, &in)
		dkim := []map[string]string{}
		for _, id := range sortedKeys(s.dkim) {
			k := s.dkim[id]
			if k.Domain == in.Domain && (k.Status == "active" || k.Status == "rotated") {
				dkim = append(dkim, map[string]string{"dkim_domain": k.Domain, "dkim_selector": k.Selector, "dkim_key_status": k.Status, "verdict": "passed", "reason": "stub: key is " + k.Status})
			}
		}
		passed := map[string]string{"verdict": "passed", "reason": "stub"}
		writeJSON(w, http.StatusOK, map[string]any{
			"check_results": map[string]any{
				"dkim":            dkim,
				"domain_lockdown": passed,
				"sender_domain":   map[string]any{"verdict": "passed", "a": passed, "mx": passed},
				"spf":             map[string]string{"verdict": "passed", "reason": "stub", "spfRecord": "v=spf1 include:relay.mailchannels.net ~all"},
			},
			"references": []string{},
		})

	default:
		writeErrors(w, http.StatusNotImplemented, "stub does not implement "+op)
	}
}

func (k *dkimKey) json(withDNS bool) map[string]any {
	out := map[string]any{
		"domain": k.Domain, "selector": k.Selector, "algorithm": k.Algorithm, "key_length": k.KeyLength,
		"public_key": k.PublicKey, "status": k.Status,
		"created_at": k.CreatedAt.Format(time.RFC3339), "status_modified_at": k.StatusModifiedAt.Format(time.RFC3339),
	}
	if withDNS {
		// Same format as transactional-api generated/go/dkim.go.
		out["dkim_dns_records"] = []map[string]string{{
			"name":  k.Selector + "._domainkey." + k.Domain,
			"type":  "TXT",
			"value": fmt.Sprintf("v=DKIM1; k=%s; p=%s", k.Algorithm, k.PublicKey),
		}}
	}
	return out
}

func (a *subAccount) json() map[string]any {
	return map[string]any{"handle": a.Handle, "company_name": a.CompanyName, "enabled": a.Enabled}
}

func validHandle(h string) bool {
	if len(h) < 3 || len(h) > 128 {
		return false
	}
	for _, c := range h {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func publicKey(bits int) (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

func paginate[T any](items []T, q map[string][]string, defaultLimit int) []T {
	get := func(name string, def int) int {
		if v, ok := q[name]; ok && len(v) > 0 {
			if n, err := strconv.Atoi(v[0]); err == nil {
				return n
			}
		}
		return def
	}
	offset, limit := get("offset", 0), get("limit", defaultLimit)
	if offset >= len(items) {
		return []T{}
	}
	items = items[offset:]
	if limit < len(items) {
		items = items[:limit]
	}
	return items
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) reset() {
	r.header = http.Header{}
	r.status = http.StatusOK
	r.body.Reset()
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recorder) WriteHeader(code int)        { r.status = code }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrors(w http.ResponseWriter, status int, msgs ...string) {
	writeJSON(w, status, map[string][]string{"errors": msgs})
}
