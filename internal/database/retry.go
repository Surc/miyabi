package database

import (
	"errors"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// RetryDelay lets recoverable library jobs resume after SQLite writer contention.
// The task queue owns the retry budget and preserves each job's checkpoints.
func RetryDelay(err error) (time.Duration, bool) {
	var busy *sqlite.Error
	if errors.As(err, &busy) && busy.Code()&0xff == sqlite3.SQLITE_BUSY {
		return 0, true
	}
	return domain.RetryDelay(err)
}
