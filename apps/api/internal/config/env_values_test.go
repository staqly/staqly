package config

import (
	"strings"
	"testing"
	"time"
)

// A typo in a setting refuses to start, naming the variable and the value: TRUST_PROXY=yes silently
// meant false and SESSION_TTL=30d silently meant the default. Negative or zero durations are refused
// where they mean nothing (PROCESSING_TIMEOUT=0 is "never").
func TestInvalidEnvValuesRefuseToStart(t *testing.T) {
	for k, v := range map[string]string{
		"TRUST_PROXY": "yes", "COOKIE_SECURE": "on", "AUTH_DEV_SIGNIN": "maybe", "S3_CREATE_BUCKET": "y",
		"SESSION_TTL": "30d", "WEBHOOK_RETRY_BASE": "-1s", "WEBHOOK_RETRY_MAX": "0s", "WEBHOOK_LEASE": "60s", "PROCESSING_TIMEOUT": "-5m",
		"MAX_UPLOAD_MB": "lots", "RATE_LIMIT_PER_MINUTE": "-3", "JOB_CONCURRENCY": "four", "RATE_LIMIT_DISABLED": "nope",
		"NOTIFY_RETRY_BASE": "soon", "MAIL_SMTP_PORT": "0",
	} {
		t.Run(k, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(k, v)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), k) || !strings.Contains(err.Error(), v) {
				t.Fatalf("%s=%s: %v, want an error naming both", k, v, err)
			}
		})
	}
	clearEnv(t)
	for k, v := range map[string]string{"TRUST_PROXY": "1", "COOKIE_SECURE": "FALSE", "SESSION_TTL": "720h",
		"PROCESSING_TIMEOUT": "0", "RATE_LIMIT_DISABLED": "true", "MAX_UPLOAD_MB": "200"} {
		t.Setenv(k, v)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.TrustProxy || c.CookieSecure || c.App.SessionTTL != 720*time.Hour || !c.App.ProcessingTimeoutNever || c.App.MaxUploadBytes != 200<<20 {
		t.Fatalf("valid values misread: %+v", c)
	}
}
