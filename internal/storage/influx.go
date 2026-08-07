package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// InfluxConfig contains connection data read from external configuration.
type InfluxConfig struct {
	URL          string
	Organization string
	Bucket       string
	Token        string
}

// InfluxClient writes InfluxDB v2 line protocol through the standard library.
type InfluxClient struct {
	endpoint string
	token    string
	client   *http.Client
}

// NewInfluxClient constructs a local HTTP writer without performing network I/O.
func NewInfluxClient(cfg InfluxConfig, client *http.Client) (*InfluxClient, error) {
	if client == nil {
		return nil, errors.New("influx HTTP client is required")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("influx URL must be an HTTP or HTTPS origin")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("influx URL must not contain credentials, query, or fragment")
	}
	if cfg.Organization == "" || cfg.Bucket == "" || cfg.Token == "" {
		return nil, errors.New("influx organization, bucket, and token are required")
	}
	endpoint := strings.TrimRight(parsed.String(), "/") + "/api/v2/write?org=" + url.QueryEscape(cfg.Organization) + "&bucket=" + url.QueryEscape(cfg.Bucket) + "&precision=ns"
	return &InfluxClient{endpoint: endpoint, token: cfg.Token, client: client}, nil
}

// Write encodes and submits one validated batch.
func (c *InfluxClient) Write(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, point := range points {
		if err := validatePoint(point); err != nil {
			return err
		}
		encodePoint(&body, point)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Token "+c.token)
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("write influx batch: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("influx write returned %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

func encodePoint(buffer *bytes.Buffer, point Point) {
	buffer.WriteString(escapeMeasurement(point.Measurement))
	for _, key := range sortedKeys(point.Tags) {
		buffer.WriteByte(',')
		buffer.WriteString(escapeTag(key))
		buffer.WriteByte('=')
		buffer.WriteString(escapeTag(point.Tags[key]))
	}
	buffer.WriteByte(' ')
	for index, key := range sortedKeys(point.Fields) {
		if index > 0 {
			buffer.WriteByte(',')
		}
		buffer.WriteString(escapeFieldKey(key))
		buffer.WriteByte('=')
		field := point.Fields[key]
		switch {
		case field.Float != nil:
			buffer.WriteString(strconv.FormatFloat(*field.Float, 'g', -1, 64))
		case field.Boolean != nil:
			buffer.WriteString(strconv.FormatBool(*field.Boolean))
		case field.String != nil:
			buffer.WriteString(strconv.Quote(*field.String))
		}
	}
	buffer.WriteByte(' ')
	buffer.WriteString(strconv.FormatInt(point.Timestamp.UnixNano(), 10))
	buffer.WriteByte('\n')
}
func escapeMeasurement(value string) string {
	return strings.NewReplacer(",", "\\,", " ", "\\ ").Replace(value)
}
func escapeTag(value string) string {
	return strings.NewReplacer(",", "\\,", " ", "\\ ", "=", "\\=").Replace(value)
}
func escapeFieldKey(value string) string { return escapeTag(value) }
