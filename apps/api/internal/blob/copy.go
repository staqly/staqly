package blob

import (
	"context"
	"errors"
	"fmt"
)

// CopyReport counts what Copy did.
type CopyReport struct {
	Copied, Skipped int
	Bytes           int64
}

// Copy puts every file of src into dst under the same key. A key dst already holds with the same size is
// skipped, so a run that stopped half way picks up where it left off and a second run copies nothing.
// With dryRun nothing is written; the report says what would be copied. Files stay in src.
func Copy(ctx context.Context, src, dst Store, dryRun bool, progress func(key string, size int64)) (CopyReport, error) {
	var rep CopyReport
	err := src.Walk(ctx, "", func(key string, size int64) error {
		have, err := dst.Stat(ctx, key)
		switch {
		case err == nil && have == size:
			rep.Skipped++
			return nil
		case err != nil && !errors.Is(err, ErrNotExist):
			return fmt.Errorf("%s: %w", key, err)
		}
		if progress != nil {
			progress(key, size)
		}
		if !dryRun {
			f, err := src.Open(ctx, key)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			err = dst.Put(ctx, key, f, size, "")
			f.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		rep.Copied++
		rep.Bytes += size
		return nil
	})
	return rep, err
}
