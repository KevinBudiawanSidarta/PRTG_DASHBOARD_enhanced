package prtg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Sensor struct {
	ID        string `json:"objid"`
	Device    string `json:"device"`
	Sensor    string `json:"sensor"`
	Status    string `json:"status"`
	LastCheck any    `json:"lastcheck"`
}
type tableResponse struct {
	Sensors []Sensor `json:"sensordata"`
}

type Client struct {
	BaseURL, Username, Password, Passhash string
	HTTP                                  *http.Client
}

func NewClient() *Client {
	sec := 20
	if v, _ := strconv.Atoi(os.Getenv("PRTG_TIMEOUT_SEC")); v > 0 {
		sec = v
	}
	base := firstEnv("PRTG_SERVER", "PRTG_BASE_URL")
	user := firstEnv("PRTG_USER", "PRTG_USERNAME")
	pass := firstEnv("PRTG_PASS", "PRTG_PASSWORD")
	return &Client{BaseURL: strings.TrimRight(base, "/"), Username: user, Password: pass, Passhash: os.Getenv("PRTG_PASSHASH"), HTTP: &http.Client{Timeout: time.Duration(sec) * time.Second}}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) Sensors(ctx context.Context) ([]Sensor, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("PRTG_SERVER/PRTG_BASE_URL is required")
	}
	u, err := url.Parse(c.BaseURL + "/api/table.json")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("content", "sensors")
	q.Set("output", "json")
	q.Set("count", "*")
	q.Set("columns", "objid,device,sensor,status,lastcheck")
	if c.Username != "" {
		q.Set("username", c.Username)
	}
	if c.Passhash != "" {
		q.Set("passhash", c.Passhash)
	} else if c.Password != "" {
		q.Set("password", c.Password)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("PRTG status %s", resp.Status)
	}
	var raw json.RawMessage
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	var table tableResponse
	if err = json.Unmarshal(raw, &table); err == nil && table.Sensors != nil {
		return table.Sensors, nil
	}
	var sensors []Sensor
	if err = json.Unmarshal(raw, &sensors); err != nil {
		return nil, fmt.Errorf("decode PRTG payload: %w", err)
	}
	return sensors, nil
}

func NormalizeState(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	// PRTG %status may contain both old and new states (for example "Down -> Up").
	if i := strings.LastIndexAny(s, ">-:"); i >= 0 && i+1 < len(s) {
		tail := strings.TrimSpace(strings.TrimLeft(s[i+1:], ">-: "))
		if strings.Contains(tail, "down") {
			return "down"
		}
		if strings.Contains(tail, "warn") {
			return "warning"
		}
		if strings.Contains(tail, "up") || strings.Contains(tail, "ok") {
			return "up"
		}
	}
	switch {
	case strings.Contains(s, "down"), strings.Contains(s, "error"):
		return "down"
	case strings.Contains(s, "warn"):
		return "warning"
	case strings.Contains(s, "up"), strings.Contains(s, "ok"):
		return "up"
	default:
		return "unknown"
	}
}
