package logger

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(kv ...any) *zap.SugaredLogger {
	var log *zap.SugaredLogger

	// this file exists if we are running in k8s
	_, err := os.Stat("/var/run/secrets/kubernetes.io")
	if err != nil {
		log = zap.Must(zap.NewDevelopment(zap.AddStacktrace(zap.PanicLevel))).Sugar()
	} else {
		cfg := zap.NewProductionConfig()
		cfg.Sampling = nil // disable sampling
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
		cfg.Level.SetLevel(zapcore.InfoLevel)
		l, err := cfg.Build(zap.AddStacktrace(zap.PanicLevel))
		if err != nil {
			// this will never happen
			panic(fmt.Sprintf("failed to initialize logger: %v", err))
		}
		log = l.Sugar()
	}
	return log.With(kv...)
}
