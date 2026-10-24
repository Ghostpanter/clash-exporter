package collector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func controllerURL(cfg CollectConfig, scheme, path string) string {
	u := url.URL{Scheme: scheme, Host: cfg.ClashHost, Path: path}
	return u.String()
}

func newAPIRequest(cfg CollectConfig, path string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, controllerURL(cfg, "http", path), nil)
	if err != nil {
		return nil, err
	}
	if cfg.ClashToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ClashToken)
	}
	return req, nil
}

func dialWS(ctx context.Context, cfg CollectConfig, path string) (*websocket.Conn, *http.Response, error) {
	header := http.Header{}
	if cfg.ClashToken != "" {
		header.Set("Authorization", "Bearer "+cfg.ClashToken)
	}
	conn, resp, err := websocket.Dial(ctx, controllerURL(cfg, "ws", path), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if conn != nil {
		conn.SetReadLimit(32 * 1024 * 1024)
	}
	return conn, resp, err
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("controller returned %s", resp.Status)
	}
	return nil
}
