// Adapted from github.com/jiayaoqijia/altcode/gateway/errors.go
// Copyright (c) 2026 Ottie contributors — MIT License

package gateway

import "errors"

var (
	// ErrNotRunning indicates the channel is not running.
	ErrNotRunning = errors.New("channel not running")

	// ErrRateLimit indicates the platform returned a rate-limit response.
	ErrRateLimit = errors.New("rate limited")

	// ErrTemporary indicates a transient failure (e.g. network timeout, 5xx).
	ErrTemporary = errors.New("temporary failure")

	// ErrSendFailed indicates a permanent failure (e.g. invalid chat ID).
	ErrSendFailed = errors.New("send failed")
)
