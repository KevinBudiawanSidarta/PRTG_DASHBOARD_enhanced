// Package jetdata is a small client for the JETData.AI V3 API (records and
// admin form configuration) used to mirror dashboard data into JETData.
package jetdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Host, Project, Username, Password string
	HTTP                              *http.Client

	mu  sync.Mutex
	key string
}

// NewClient reads JET_HOST, JET_PROJECT, JET_USERNAME and JET_PASSWORD.
func NewClient() (*Client, error) {
	c := &Client{
		Host:     strings.TrimRight(strings.TrimSpace(os.Getenv("JET_HOST")), "/"),
		Project:  strings.TrimSpace(os.Getenv("JET_PROJECT")),
		Username: os.Getenv("JET_USERNAME"),
		Password: os.Getenv("JET_PASSWORD"),
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
	if c.Host == "" || c.Project == "" || c.Username == "" || c.Password == "" {
		return nil, errors.New("JET_HOST, JET_PROJECT, JET_USERNAME and JET_PASSWORD are required")
	}
	return c, nil
}

func (c *Client) endpoint() string { return c.Host + "/api_v3/api.php" }

// raw posts a multipart request and returns the decoded JSON object.
func (c *Client) raw(ctx context.Context, params map[string]string) (map[string]any, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range params {
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("JET %s: unexpected response (HTTP %d): %.200s", params["method"], resp.StatusCode, string(b))
	}
	return out, nil
}

func (c *Client) authenticate(ctx context.Context) (string, error) {
	out, err := c.raw(ctx, map[string]string{"method": "authenticate", "project": c.Project, "username": c.Username, "password": c.Password})
	if err != nil {
		return "", err
	}
	key, _ := out["session_key"].(string)
	if key == "" {
		return "", fmt.Errorf("JET authenticate failed: %v", out["message"])
	}
	return key, nil
}

func (c *Client) session(ctx context.Context, renew bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != "" && !renew {
		return c.key, nil
	}
	key, err := c.authenticate(ctx)
	if err != nil {
		return "", err
	}
	c.key = key
	return key, nil
}

// Call runs a V3 method with the current session, re-authenticating once if
// the session expired. A non-success status is returned as an error.
func (c *Client) Call(ctx context.Context, method string, params map[string]string) (map[string]any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		key, err := c.session(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		p := map[string]string{"method": method, "project": c.Project, "session_key": key}
		for k, v := range params {
			p[k] = v
		}
		out, err := c.raw(ctx, p)
		if err != nil {
			return nil, err
		}
		if status, _ := out["status"].(string); status != "success" {
			msg := fmt.Sprint(out["message"])
			if attempt == 0 && strings.Contains(strings.ToLower(msg), "session") {
				continue
			}
			return out, fmt.Errorf("JET %s: %s", method, msg)
		}
		return out, nil
	}
	return nil, fmt.Errorf("JET %s: session could not be renewed", method)
}

// Form is one entry of getForms.
type Form struct {
	ID        int
	Name      string
	Type      string
	GroupName string
}

func (c *Client) Forms(ctx context.Context) ([]Form, error) {
	out, err := c.Call(ctx, "getForms", nil)
	if err != nil {
		return nil, err
	}
	list, _ := out["forms"].([]any)
	forms := make([]Form, 0, len(list))
	for _, x := range list {
		m, _ := x.(map[string]any)
		id, _ := strconv.Atoi(fmt.Sprint(m["id_form"]))
		forms = append(forms, Form{ID: id, Name: fmt.Sprint(m["form_name"]), Type: fmt.Sprint(m["form_type"]), GroupName: fmt.Sprint(m["group_name"])})
	}
	return forms, nil
}

// FieldNames lists a form's field_name values.
func (c *Client) FieldNames(ctx context.Context, formID int) (map[string]bool, error) {
	out, err := c.Call(ctx, "getFieldMappings", map[string]string{"id_form": strconv.Itoa(formID)})
	if err != nil {
		return nil, err
	}
	list, _ := out["fields"].([]any)
	names := map[string]bool{}
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			names[fmt.Sprint(m["field_name"])] = true
		}
	}
	return names, nil
}

func (c *Client) CreateForm(ctx context.Context, name, formType, group string) (int, error) {
	out, err := c.Call(ctx, "createForm", map[string]string{"form_name": name, "form_type": formType, "group_name": group, "access_level": "public"})
	if err != nil {
		return 0, err
	}
	return idFrom(out["id_form"])
}

func (c *Client) CreateCustomForm(ctx context.Context, name, group, html string) (int, error) {
	out, err := c.Call(ctx, "createCustomForm", map[string]string{"form_name": name, "group_name": group, "custom_UI": html})
	if err != nil {
		return 0, err
	}
	return idFrom(out["id_form"])
}

func (c *Client) UpdateCustomForm(ctx context.Context, formID int, html string) error {
	_, err := c.Call(ctx, "updateCustomForm", map[string]string{"id_form": strconv.Itoa(formID), "custom_UI": html})
	return err
}

// CustomFormHTML reads custom_UI back (admin only) for deploy verification.
func (c *Client) CustomFormHTML(ctx context.Context, formID int) (string, error) {
	out, err := c.Call(ctx, "getCustomForm", map[string]string{"id_form": strconv.Itoa(formID)})
	if err != nil {
		return "", err
	}
	data, _ := out["data"].(map[string]any)
	html, _ := data["custom_UI"].(string)
	return html, nil
}

// Field describes a field to create with createFieldMapping.
type Field struct {
	Name, Label, Type string
	Order             int
}

func (c *Client) CreateField(ctx context.Context, formID int, f Field) error {
	_, err := c.Call(ctx, "createFieldMapping", map[string]string{
		"id_form": strconv.Itoa(formID), "field_name": f.Name, "field_label": f.Label,
		"field_type": f.Type, "field_order": strconv.Itoa(f.Order),
	})
	return err
}

// Record is one JET row: its id_row plus field values as strings.
type Record struct {
	ID     int
	Values map[string]string
}

// AllRecords pages through a form with getRecords (max 1000 per page).
func (c *Client) AllRecords(ctx context.Context, formID int) ([]Record, error) {
	var out []Record
	for offset := 0; ; offset += 1000 {
		res, err := c.Call(ctx, "getRecords", map[string]string{"id_form": strconv.Itoa(formID), "limit": "1000", "offset": strconv.Itoa(offset)})
		if err != nil {
			return nil, err
		}
		rows, _ := res["data"].([]any)
		for _, x := range rows {
			m, _ := x.(map[string]any)
			r := Record{Values: map[string]string{}}
			for k, v := range m {
				if v == nil {
					r.Values[k] = ""
				} else {
					r.Values[k] = fmt.Sprint(v)
				}
			}
			r.ID, _ = strconv.Atoi(r.Values["id_row"])
			out = append(out, r)
		}
		if truncated, _ := res["truncated"].(bool); !truncated || len(rows) == 0 {
			return out, nil
		}
	}
}

// CreateBatch inserts records with createRecordsBatch (query params + JSON
// body) and reports partial failures as an error.
func (c *Client) CreateBatch(ctx context.Context, formID int, records []map[string]string) error {
	if len(records) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"records": records})
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		key, err := c.session(ctx, attempt > 0)
		if err != nil {
			return err
		}
		q := url.Values{}
		q.Set("method", "createRecordsBatch")
		q.Set("project", c.Project)
		q.Set("session_key", key)
		q.Set("id_form", strconv.Itoa(formID))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint()+"?"+q.Encode(), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Summary struct {
				Failed int `json:"failed"`
			} `json:"summary"`
			Errors []any `json:"errors"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			return fmt.Errorf("JET createRecordsBatch: unexpected response: %.200s", string(b))
		}
		if out.Status != "success" {
			if attempt == 0 && strings.Contains(strings.ToLower(out.Message), "session") {
				continue
			}
			return fmt.Errorf("JET createRecordsBatch: %s", out.Message)
		}
		if out.Summary.Failed > 0 {
			return fmt.Errorf("JET createRecordsBatch: %d of %d records failed: %v", out.Summary.Failed, len(records), out.Errors)
		}
		return nil
	}
	return errors.New("JET createRecordsBatch: session could not be renewed")
}

func (c *Client) UpdateRecord(ctx context.Context, formID, rowID int, values map[string]string) error {
	p := map[string]string{"id_form": strconv.Itoa(formID), "id_row": strconv.Itoa(rowID)}
	for k, v := range values {
		p[k] = v
	}
	_, err := c.Call(ctx, "updateRecord", p)
	return err
}

func (c *Client) DeleteRecord(ctx context.Context, formID, rowID int) error {
	_, err := c.Call(ctx, "deleteRecord", map[string]string{"id_form": strconv.Itoa(formID), "id_row": strconv.Itoa(rowID)})
	return err
}

func idFrom(v any) (int, error) {
	id, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("JET returned no id_form (%v)", v)
	}
	return id, nil
}
