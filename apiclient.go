package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type coreAPIClient struct {
	cfg    clashAPIConfig
	logger *logger
	client *http.Client
}

func newCoreAPIClient(cfg clashAPIConfig, l *logger) *coreAPIClient {
	return &coreAPIClient{
		cfg:    cfg,
		logger: l,
		client: &http.Client{Transport: &http.Transport{Proxy: nil}},
	}
}

func (c *coreAPIClient) isConfigured() bool {
	return c.cfg.IsConfigured()
}

func (c *coreAPIClient) checkReady() bool {
	if !c.isConfigured() {
		return false
	}

	c.log("API check...")
	if _, err := c.request("GET", "/version", "", time.Second); err != nil {
		c.log(strings.TrimSpace("API check: failed. " + err.Error()))
		return false
	}
	c.log("API check: successful.")
	return true
}

func (c *coreAPIClient) request(method, path, data string, timeout time.Duration) (string, error) {
	if !c.isConfigured() {
		return "", errors.New("Clash API is not configured.")
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var body io.Reader
	if data != "" {
		body = strings.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method,
		"http://"+c.cfg.ConnectAddress()+path, body)
	if err != nil {
		return "", fmt.Errorf("%s %s%s after %d ms. %v", method, path, requestDataSuffix(data), elapsedMs(start), err)
	}
	if c.cfg.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Secret)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s%s after %d ms. %v", method, path, requestDataSuffix(data), elapsedMs(start), err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%s %s%s after %d ms. %v", method, path, requestDataSuffix(data), elapsedMs(start), err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s %s%s after %d ms. HTTP %d.", method, path, requestDataSuffix(data), elapsedMs(start), resp.StatusCode)
	}

	return string(payload), nil
}

func (c *coreAPIClient) log(message string) {
	c.logger.log("API", message)
}

func requestDataSuffix(data string) string {
	if data == "" {
		return ""
	}
	return " " + data
}

func elapsedMs(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
