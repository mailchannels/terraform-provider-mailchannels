// Package client is a small HTTP client for the parts of the MailChannels
// Email API that the Terraform provider manages.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the public Email API endpoint.
const DefaultBaseURL = "https://api.mailchannels.net/tx/v1"

// pageSize is the largest page the list endpoints accept.
const pageSize = 1000

// Client calls the Email API with a single API key.
type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

// New returns a client for baseURL (DefaultBaseURL when empty).
func New(baseURL, apiKey, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		userAgent:  userAgent,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// APIError is a non-2xx response from the Email API.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	msg := strings.TrimSpace(e.Body)
	var parsed struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal([]byte(msg), &parsed) == nil && len(parsed.Errors) > 0 {
		msg = strings.Join(parsed.Errors, "; ")
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.StatusCode, msg)
}

// IsNotFound reports whether err is an HTTP 404 from the Email API.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(data)}
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: decoding response: %w", method, path, err)
		}
	}
	return nil
}

// DKIM keys

// DKIMDNSRecord is the suggested DNS record for a DKIM key.
type DKIMDNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// DKIMKey is a MailChannels-managed DKIM key pair.
type DKIMKey struct {
	Domain           string          `json:"domain"`
	Selector         string          `json:"selector"`
	Algorithm        string          `json:"algorithm"`
	KeyLength        int64           `json:"key_length"`
	PublicKey        string          `json:"public_key"`
	Status           string          `json:"status"`
	CreatedAt        *string         `json:"created_at,omitempty"`
	StatusModifiedAt *string         `json:"status_modified_at,omitempty"`
	DNSRecords       []DKIMDNSRecord `json:"dkim_dns_records,omitempty"`
}

// CreateDKIMKeyRequest is the body of POST /domains/{domain}/dkim-keys.
type CreateDKIMKeyRequest struct {
	Selector  string `json:"selector"`
	Algorithm string `json:"algorithm,omitempty"`
	KeyLength int64  `json:"key_length,omitempty"`
}

func dkimPath(domain string) string {
	return "/domains/" + url.PathEscape(domain) + "/dkim-keys"
}

// CreateDKIMKey creates a key pair for domain.
func (c *Client) CreateDKIMKey(ctx context.Context, domain string, in CreateDKIMKeyRequest) (*DKIMKey, error) {
	var out DKIMKey
	if err := c.do(ctx, http.MethodPost, dkimPath(domain), nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDKIMKey returns the key for domain and selector, including its DNS
// record, or nil when the API has no such key.
func (c *Client) GetDKIMKey(ctx context.Context, domain, selector string) (*DKIMKey, error) {
	q := url.Values{"selector": {selector}, "include_dns_record": {"true"}}
	var out struct {
		Keys []DKIMKey `json:"keys"`
	}
	if err := c.do(ctx, http.MethodGet, dkimPath(domain), q, nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Keys {
		if out.Keys[i].Selector == selector {
			return &out.Keys[i], nil
		}
	}
	return nil, nil
}

// UpdateDKIMKeyStatus sets the status of a key (revoked, retired or rotated).
func (c *Client) UpdateDKIMKeyStatus(ctx context.Context, domain, selector, status string) error {
	path := dkimPath(domain) + "/" + url.PathEscape(selector)
	return c.do(ctx, http.MethodPatch, path, nil, map[string]string{"status": status}, nil)
}

// Webhooks

// ListWebhooks returns the registered webhook endpoints.
func (c *Client) ListWebhooks(ctx context.Context) ([]string, error) {
	var out []struct {
		Webhook string `json:"webhook"`
	}
	if err := c.do(ctx, http.MethodGet, "/webhook", nil, nil, &out); err != nil {
		return nil, err
	}
	endpoints := make([]string, 0, len(out))
	for _, w := range out {
		endpoints = append(endpoints, w.Webhook)
	}
	return endpoints, nil
}

// CreateWebhook registers endpoint.
func (c *Client) CreateWebhook(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodPost, "/webhook", url.Values{"endpoint": {endpoint}}, nil, nil)
}

// DeleteAllWebhooks removes every registered webhook endpoint. The API has
// no call that removes a single endpoint.
func (c *Client) DeleteAllWebhooks(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/webhook", nil, nil, nil)
}

// Sub-accounts

// SubAccount is a sub-account of the authenticated parent account.
type SubAccount struct {
	Handle      string `json:"handle"`
	CompanyName string `json:"company_name"`
	Enabled     bool   `json:"enabled"`
}

// CreateSubAccountRequest is the body of POST /sub-account.
type CreateSubAccountRequest struct {
	CompanyName string `json:"company_name"`
	Handle      string `json:"handle,omitempty"`
}

// CreateSubAccount creates a sub-account.
func (c *Client) CreateSubAccount(ctx context.Context, in CreateSubAccountRequest) (*SubAccount, error) {
	var out SubAccount
	if err := c.do(ctx, http.MethodPost, "/sub-account", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSubAccount pages through the sub-accounts and returns the one with
// handle, or nil when there is none. The API has no single-account read.
func (c *Client) GetSubAccount(ctx context.Context, handle string) (*SubAccount, error) {
	for offset := 0; ; offset += pageSize {
		q := url.Values{"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		var page []SubAccount
		if err := c.do(ctx, http.MethodGet, "/sub-account", q, nil, &page); err != nil {
			return nil, err
		}
		for i := range page {
			if page[i].Handle == handle {
				return &page[i], nil
			}
		}
		if len(page) < pageSize {
			return nil, nil
		}
	}
}

func subAccountPath(handle string) string {
	return "/sub-account/" + url.PathEscape(handle)
}

// DeleteSubAccount deletes a sub-account.
func (c *Client) DeleteSubAccount(ctx context.Context, handle string) error {
	return c.do(ctx, http.MethodDelete, subAccountPath(handle), nil, nil, nil)
}

// SetSubAccountEnabled activates or suspends a sub-account.
func (c *Client) SetSubAccountEnabled(ctx context.Context, handle string, enabled bool) error {
	action := "/suspend"
	if enabled {
		action = "/activate"
	}
	return c.do(ctx, http.MethodPost, subAccountPath(handle)+action, nil, nil, nil)
}

// GetSubAccountLimit returns the monthly send limit of a sub-account; -1
// means it inherits the parent account's limit.
func (c *Client) GetSubAccountLimit(ctx context.Context, handle string) (int64, error) {
	var out struct {
		Sends int64 `json:"sends"`
	}
	if err := c.do(ctx, http.MethodGet, subAccountPath(handle)+"/limit", nil, nil, &out); err != nil {
		return 0, err
	}
	return out.Sends, nil
}

// SetSubAccountLimit sets the monthly send limit of a sub-account.
func (c *Client) SetSubAccountLimit(ctx context.Context, handle string, sends int64) error {
	return c.do(ctx, http.MethodPut, subAccountPath(handle)+"/limit", nil, map[string]int64{"sends": sends}, nil)
}

// DeleteSubAccountLimit makes a sub-account inherit the parent's limit.
func (c *Client) DeleteSubAccountLimit(ctx context.Context, handle string) error {
	return c.do(ctx, http.MethodDelete, subAccountPath(handle)+"/limit", nil, nil, nil)
}

// Suppressions

// SuppressionSourceAPI is the source of entries created through the API.
const SuppressionSourceAPI = "api"

// Suppression is an entry in the suppression list.
type Suppression struct {
	Recipient        string   `json:"recipient"`
	SuppressionTypes []string `json:"suppression_types,omitempty"`
	Notes            *string  `json:"notes,omitempty"`
	Source           string   `json:"source,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
}

// CreateSuppression adds one entry to the suppression list.
func (c *Client) CreateSuppression(ctx context.Context, in Suppression) error {
	entry := Suppression{Recipient: in.Recipient, SuppressionTypes: in.SuppressionTypes, Notes: in.Notes}
	body := map[string]any{"suppression_entries": []Suppression{entry}}
	return c.do(ctx, http.MethodPost, "/suppression-list", nil, body, nil)
}

// GetSuppression returns the entry for recipient with the given source, or
// nil when there is none.
func (c *Client) GetSuppression(ctx context.Context, recipient, source string) (*Suppression, error) {
	q := url.Values{"recipient": {recipient}, "source": {source}}
	var out struct {
		SuppressionList []Suppression `json:"suppression_list"`
	}
	if err := c.do(ctx, http.MethodGet, "/suppression-list", q, nil, &out); err != nil {
		return nil, err
	}
	for i := range out.SuppressionList {
		e := out.SuppressionList[i]
		if strings.EqualFold(e.Recipient, recipient) && (e.Source == "" || e.Source == source) {
			return &e, nil
		}
	}
	return nil, nil
}

// DeleteSuppression removes the entry for recipient with the given source.
func (c *Client) DeleteSuppression(ctx context.Context, recipient, source string) error {
	path := "/suppression-list/recipients/" + url.PathEscape(recipient)
	return c.do(ctx, http.MethodDelete, path, url.Values{"source": {source}}, nil, nil)
}

// Check domain

// DKIMSetting selects the DKIM key check-domain verifies.
type DKIMSetting struct {
	Domain     string `json:"dkim_domain,omitempty"`
	Selector   string `json:"dkim_selector,omitempty"`
	PrivateKey string `json:"dkim_private_key,omitempty"`
}

// CheckDomainRequest is the body of POST /check-domain.
type CheckDomainRequest struct {
	Domain             string        `json:"domain"`
	DKIMSettings       []DKIMSetting `json:"dkim_settings,omitempty"`
	EnvelopeFromDomain string        `json:"envelope_from_domain,omitempty"`
	SenderID           string        `json:"sender_id,omitempty"`
}

// Verdict is a single check outcome.
type Verdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// CheckDomainResult is the response of POST /check-domain.
type CheckDomainResult struct {
	CheckResults struct {
		DKIM []struct {
			Domain    string `json:"dkim_domain"`
			Selector  string `json:"dkim_selector"`
			KeyStatus string `json:"dkim_key_status"`
			Verdict   string `json:"verdict"`
			Reason    string `json:"reason"`
		} `json:"dkim"`
		DomainLockdown Verdict `json:"domain_lockdown"`
		SenderDomain   struct {
			Verdict string  `json:"verdict"`
			A       Verdict `json:"a"`
			MX      Verdict `json:"mx"`
		} `json:"sender_domain"`
		SPF struct {
			Verdict     string `json:"verdict"`
			Reason      string `json:"reason"`
			Record      string `json:"spfRecord"`
			RecordError string `json:"spfRecordError"`
		} `json:"spf"`
	} `json:"check_results"`
	References []string `json:"references"`
}

// CheckDomain verifies a domain's SPF, DKIM, Domain Lockdown and sender
// domain records.
func (c *Client) CheckDomain(ctx context.Context, in CheckDomainRequest) (*CheckDomainResult, error) {
	var out CheckDomainResult
	if err := c.do(ctx, http.MethodPost, "/check-domain", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
