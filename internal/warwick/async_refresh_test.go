package warwick

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
)

func TestAsyncRefreshDisabledDoesNotSpawnDetachedWork(t *testing.T) {
	client := NewClassroomClient(nil, cache.New())
	client.SetAsyncRefreshEnabled(false)
	called := make(chan struct{}, 1)

	started := client.tryRefresh("key", func() { called <- struct{}{} })

	require.False(t, started)
	select {
	case <-called:
		t.Fatal("disabled async refresh must not start detached work")
	case <-time.After(20 * time.Millisecond):
	}
}
