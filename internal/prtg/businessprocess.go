package prtg

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// historyTimeout bounds a single historic-data request; a week of 1-minute
// scans for one sensor is a few MB of XML.
const historyTimeout = 90 * time.Second

// Channel is one channel of a sensor as listed by PRTG's channel table.
type Channel struct {
	ID        int
	Name      string
	LastValue string
}

// Channels lists a sensor's channels with their last value. PRTG's internal
// "Downtime" channel (negative id) is skipped.
func (c *Client) Channels(ctx context.Context, sensorID string) ([]Channel, error) {
	q := url.Values{}
	q.Set("content", "channels")
	q.Set("id", sensorID)
	q.Set("columns", "objid,name,lastvalue")
	body, err := c.get(ctx, "/api/table.json", q, 0)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Channels []struct {
			ObjID     any    `json:"objid"`
			Name      string `json:"name"`
			LastValue string `json:"lastvalue"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode PRTG channels: %w", err)
	}
	out := []Channel{}
	for _, ch := range resp.Channels {
		id, ok := toInt(ch.ObjID)
		if !ok || id < 0 {
			continue
		}
		out = append(out, Channel{ID: id, Name: ch.Name, LastValue: ch.LastValue})
	}
	return out, nil
}

// BusinessProcessChannel is one channel definition of a Business Process
// sensor: the objects (probes, groups, devices or sensors) it summarizes and
// the "percentage of objects up" thresholds that turn it Warning or Down.
type BusinessProcessChannel struct {
	Name             string
	Objects          []string
	WarningThreshold float64
	ErrorThreshold   float64
}

var bpDefinitionRe = regexp.MustCompile(`(?s)data-plugin="businessprocesschannels"\s*>\s*<!--\s*(\{.*?\})\s*-->`)

// BusinessProcessDefinition reads a Business Process sensor's channel
// configuration. PRTG's API has no endpoint for it, so it is read from the
// JSON PRTG embeds in the sensor's settings page. If a PRTG upgrade changes
// that page, this returns an error and callers keep the last known definition.
func (c *Client) BusinessProcessDefinition(ctx context.Context, sensorID string) ([]BusinessProcessChannel, error) {
	q := url.Values{}
	q.Set("id", sensorID)
	q.Set("objecttype", "sensor")
	body, err := c.get(ctx, "/controls/objectdata.htm", q, 0)
	if err != nil {
		return nil, err
	}
	m := bpDefinitionRe.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("business process definition not found in PRTG settings page")
	}
	var def struct {
		Objects []struct {
			ChannelName      string `json:"channelname"`
			Objects          []any  `json:"objects"`
			WarningThreshold any    `json:"warningthreshhold"` // PRTG's spelling
			ErrorThreshold   any    `json:"errorthreshhold"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(string(m[1]))), &def); err != nil {
		return nil, fmt.Errorf("decode business process definition: %w", err)
	}
	out := []BusinessProcessChannel{}
	for _, d := range def.Objects {
		ch := BusinessProcessChannel{Name: d.ChannelName, Objects: []string{}}
		for _, o := range d.Objects {
			if id, ok := toInt(o); ok {
				ch.Objects = append(ch.Objects, strconv.Itoa(id))
			}
		}
		ch.WarningThreshold, _ = toFloat(d.WarningThreshold)
		ch.ErrorThreshold, _ = toFloat(d.ErrorThreshold)
		out = append(out, ch)
	}
	return out, nil
}

// SensorDetails is the subset of PRTG's getsensordetails used for business
// processes. Uptime/Downtime are PRTG's cumulative figures since StatsSince.
type SensorDetails struct {
	Name         string
	ParentDevice string
	ParentGroup  string
	StatusText   string
	Message      string
	IntervalSec  int
	UptimePct    *float64
	DowntimePct  *float64
	StatsSince   *time.Time
}

func (c *Client) SensorDetails(ctx context.Context, sensorID string) (SensorDetails, error) {
	q := url.Values{}
	q.Set("id", sensorID)
	body, err := c.get(ctx, "/api/getsensordetails.json", q, 0)
	if err != nil {
		return SensorDetails{}, err
	}
	var resp struct {
		Data map[string]any `json:"sensordata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return SensorDetails{}, fmt.Errorf("decode PRTG sensor details: %w", err)
	}
	str := func(k string) string { s, _ := resp.Data[k].(string); return s }
	d := SensorDetails{
		Name:         str("name"),
		ParentDevice: str("parentdevicename"),
		ParentGroup:  str("parentgroupname"),
		StatusText:   str("statustext"),
		Message:      plainText(str("lastmessage")),
	}
	d.IntervalSec, _ = strconv.Atoi(str("interval"))
	if v, ok := parsePercent(str("uptime")); ok {
		d.UptimePct = &v
	}
	if v, ok := parsePercent(str("downtime")); ok {
		d.DowntimePct = &v
	}
	// updownsince looks like "46283.1564699190 [13 d ago]" (OLE date, UTC).
	if f := strings.Fields(str("updownsince")); len(f) > 0 {
		if v, err := strconv.ParseFloat(f[0], 64); err == nil && v > 0 {
			t := OLEToTime(v)
			d.StatsSince = &t
		}
	}
	return d, nil
}

// ObjectNames maps object id -> name/status for a PRTG table ("devices" or
// "groups"); used to label Business Process members that are not sensors.
type ObjectInfo struct{ Name, Status string }

func (c *Client) ObjectNames(ctx context.Context, content string) (map[string]ObjectInfo, error) {
	q := url.Values{}
	q.Set("content", content)
	q.Set("count", "*")
	q.Set("columns", "objid,name,status")
	body, err := c.get(ctx, "/api/table.json", q, 0)
	if err != nil {
		return nil, err
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode PRTG %s: %w", content, err)
	}
	var rows []struct {
		ObjID  any    `json:"objid"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp[content], &rows); err != nil {
		return nil, fmt.Errorf("decode PRTG %s: %w", content, err)
	}
	out := map[string]ObjectInfo{}
	for _, r := range rows {
		if id, ok := toInt(r.ObjID); ok {
			out[strconv.Itoa(id)] = ObjectInfo{Name: r.Name, Status: r.Status}
		}
	}
	return out, nil
}

var clockRe = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})\s*([AaPp][Mm])?`)

// ServerUTCOffset estimates the PRTG server's UTC offset from its clock.
// PRTG interprets historic-data sdate/edate in that local time while
// returning sample times in UTC. Only the time of day is parsed, so the
// date format (which follows PRTG's locale) does not matter.
func (c *Client) ServerUTCOffset(ctx context.Context) (time.Duration, error) {
	body, err := c.get(ctx, "/api/status.json", url.Values{}, 0)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var resp struct {
		Clock string `json:"Clock"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("decode PRTG status: %w", err)
	}
	m := clockRe.FindStringSubmatch(resp.Clock)
	if m == nil {
		return 0, fmt.Errorf("unrecognized PRTG clock %q", resp.Clock)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	switch strings.ToLower(m[4]) {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	local := time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute + time.Duration(s)*time.Second
	utc := time.Duration(now.Hour())*time.Hour + time.Duration(now.Minute())*time.Minute + time.Duration(now.Second())*time.Second
	diff := local - utc
	for diff <= -12*time.Hour {
		diff += 24 * time.Hour
	}
	for diff > 14*time.Hour {
		diff -= 24 * time.Hour
	}
	return diff.Round(15 * time.Minute), nil
}

// HistorySample is one PRTG scan: raw channel values keyed by channel id.
type HistorySample struct {
	At     time.Time
	Values map[int]string
}

// History returns raw (unaveraged) scans of a sensor between from and to
// (UTC). serverOffset is the PRTG server's UTC offset (see ServerUTCOffset);
// PRTG reads sdate/edate in its local time.
func (c *Client) History(ctx context.Context, sensorID string, from, to time.Time, serverOffset time.Duration) ([]HistorySample, error) {
	const layout = "2006-01-02-15-04-05"
	q := url.Values{}
	q.Set("id", sensorID)
	q.Set("avg", "0")
	q.Set("sdate", from.UTC().Add(serverOffset).Format(layout))
	q.Set("edate", to.UTC().Add(serverOffset).Format(layout))
	body, err := c.get(ctx, "/api/historicdata.xml", q, historyTimeout)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Items []struct {
			DatetimeRaw string `xml:"datetime_raw"`
			Values      []struct {
				ChannelID string `xml:"channelid,attr"`
				Value     string `xml:",chardata"`
			} `xml:"value_raw"`
		} `xml:"item"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode PRTG historic data: %w", err)
	}
	out := make([]HistorySample, 0, len(doc.Items))
	for _, it := range doc.Items {
		raw, err := strconv.ParseFloat(strings.TrimSpace(it.DatetimeRaw), 64)
		if err != nil || raw <= 0 {
			continue
		}
		s := HistorySample{At: OLEToTime(raw), Values: map[int]string{}}
		for _, v := range it.Values {
			if id, err := strconv.Atoi(strings.TrimSpace(v.ChannelID)); err == nil {
				s.Values[id] = strings.TrimSpace(v.Value)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// OLEToTime converts PRTG's OLE automation date (days since 1899-12-30, UTC)
// to a time.Time rounded to the second.
func OLEToTime(days float64) time.Time {
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(math.Round(days * 86400)) * time.Second)
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(x))
		return i, err == nil
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func parsePercent(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%")), 64)
	return f, err == nil
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

// plainText strips HTML and PRTG's (sometimes double) entity escaping.
func plainText(s string) string {
	s = html.UnescapeString(html.UnescapeString(s))
	return strings.TrimSpace(tagRe.ReplaceAllString(s, ""))
}
