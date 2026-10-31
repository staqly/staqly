package app

import (
	"context"
	"time"
)

// Cleanup must survive a client disconnect after storage accepted a file or a database write committed.
// Keep it bounded even when the object store is unavailable; preserve request values for logging.
func mediaCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}

func (s *Service) removeStoredFile(ctx context.Context, key string) {
	cleanup, cancel := mediaCleanupContext(ctx)
	defer cancel()
	if err := s.Cfg.Blob.Delete(cleanup, key); err != nil {
		s.logger(ctx).Warn("stored file not removed", "key", key, "err", err)
	}
}
