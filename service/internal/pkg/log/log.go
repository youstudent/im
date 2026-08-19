// Package log 封装统一的 slog 日志初始化。
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

var logger *slog.Logger

// Init 初始化全局 logger。level 取值：debug/info/warn/error。
func Init(level, output string) {
	var out io.Writer = os.Stdout
	switch strings.ToLower(output) {
	case "stdout", "", "console":
		out = os.Stdout
	case "stderr":
		out = os.Stderr
	default:
		f, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			out = f
		}
	}

	lv := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn", "warning":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	}

	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: lv})
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// L 返回全局 logger。
func L() *slog.Logger { return logger }
