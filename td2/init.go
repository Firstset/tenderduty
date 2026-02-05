package tenderduty

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	dash "github.com/firstset/tenderduty/v2/td2/dashboard"
)

//go:embed static/*
var content embed.FS

func init() {
	level := slog.LevelInfo
	if envLevel := strings.TrimSpace(strings.ToLower(os.Getenv("LOG_LEVEL"))); envLevel != "" {
		switch envLevel {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
	slog.Info("logging configured", "level", level.String(), "hint", "set LOG_LEVEL to debug|info|warn|error")
	dash.Content = content

	// use a channel for logging, two reasons: several logs could hit at once (formatting,) and to broadcast
	// messages to the monitoring dashboard
	go func() {
		for msg := range logs {
			parts, ok := msg.([]any)
			if !ok || len(parts) == 0 {
				continue
			}
			level, ok := parts[0].(slog.Level)
			if !ok {
				level = slog.LevelInfo
			} else {
				parts = parts[1:]
			}
			if len(parts) == 0 {
				continue
			}
			msgStr := strings.TrimRight(strings.TrimLeft(fmt.Sprint(parts...), "["), "]")
			slog.Log(context.Background(), level, "tenderduty | "+msgStr)
			if td.EnableDash && !td.HideLogs && td.logChan != nil {
				td.logChan <- dash.LogMessage{
					MsgType: "log",
					Ts:      time.Now().UTC().Unix(),
					Msg:     msgStr,
				}
			}
		}
	}()
}

var logs = make(chan any)

func l(v ...any) {
	if len(v) == 0 {
		return
	}
	level := slog.LevelInfo
	switch t := v[0].(type) {
	case slog.Level:
		level = t
		v = v[1:]
	case slog.Leveler:
		level = t.Level()
		v = v[1:]
	}
	if len(v) == 0 {
		return
	}
	logs <- append([]any{level}, v...)
}
