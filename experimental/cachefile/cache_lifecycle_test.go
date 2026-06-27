package cachefile

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sagernet/bbolt"
	"github.com/sagernet/sing/common/logger"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/option"

	"github.com/stretchr/testify/require"
)

func TestFakeIPMetadataAsyncFlushesLatestMetadata(t *testing.T) {
	t.Parallel()

	cacheFile := newStartedTestCacheFile(t, option.CacheFileOptions{StoreFakeIP: true})

	oldMetadata := testFakeIPMetadata("198.18.0.1")
	newMetadata := testFakeIPMetadata("198.18.0.2")
	cacheFile.FakeIPSaveMetadataAsync(oldMetadata)
	cacheFile.FakeIPSaveMetadataAsync(newMetadata)
	cacheFile.flushFakeIPMetadata()

	require.Equal(t, newMetadata, cacheFile.FakeIPMetadata())
}

func TestFakeIPMetadataSyncSaveSupersedesPendingTimer(t *testing.T) {
	t.Parallel()

	cacheFile := newStartedTestCacheFile(t, option.CacheFileOptions{StoreFakeIP: true})

	oldMetadata := testFakeIPMetadata("198.18.0.1")
	newMetadata := testFakeIPMetadata("198.18.0.2")
	cacheFile.FakeIPSaveMetadataAsync(oldMetadata)
	require.NoError(t, cacheFile.FakeIPSaveMetadata(newMetadata))
	cacheFile.flushFakeIPMetadata()

	require.Equal(t, newMetadata, cacheFile.FakeIPMetadata())
}

func TestFakeIPMetadataClosePersistsPendingMetadataAfterAsyncBackpressure(t *testing.T) {
	t.Parallel()

	options := option.CacheFileOptions{StoreFakeIP: true}
	cacheFile := newStartedTestCacheFile(t, options)

	for range cacheFileMaxAsyncWrites {
		require.True(t, cacheFile.beginAsyncWrite())
	}

	metadata := testFakeIPMetadata("198.18.0.9")
	cacheFile.FakeIPSaveMetadataAsync(metadata)
	cacheFile.flushFakeIPMetadata()

	for range cacheFileMaxAsyncWrites {
		cacheFile.endAsyncWrite()
	}

	path := cacheFile.path
	require.NoError(t, cacheFile.Close())

	reopened := New(context.Background(), logger.NOP(), option.CacheFileOptions{
		Path:        path,
		StoreFakeIP: true,
	})
	require.NoError(t, reopened.start())
	t.Cleanup(func() {
		_ = reopened.Close()
	})

	require.Equal(t, metadata, reopened.FakeIPMetadata())
}

func TestCleanupDNSCacheHonorsCancelledContext(t *testing.T) {
	t.Parallel()

	cacheFile := newStartedTestCacheFile(t, option.CacheFileOptions{StoreDNS: true})
	expireAt := time.Now().Add(-time.Hour)
	require.NoError(t, cacheFile.SaveDNSCache("local", "example.com.", 1, []byte{1, 2, 3}, expireAt))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cacheFile.cleanupDNSCache(ctx)

	rawMessage, _, loaded := cacheFile.LoadDNSCache("local", "example.com.", 1)
	require.True(t, loaded)
	require.Equal(t, []byte{1, 2, 3}, rawMessage)
}

func TestCleanupRDRCHonorsCancelledContext(t *testing.T) {
	t.Parallel()

	cacheFile := newStartedTestCacheFile(t, option.CacheFileOptions{StoreRDRC: true})
	require.NoError(t, cacheFile.batch(func(tx *bbolt.Tx) error {
		bucket, err := cacheFile.createBucket(tx, bucketRDRC)
		if err != nil {
			return err
		}
		bucket, err = bucket.CreateBucketIfNotExists([]byte("local"))
		if err != nil {
			return err
		}
		return bucket.Put([]byte{0, 1, 'e'}, []byte{0})
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cacheFile.cleanupRDRC(ctx)

	var exists bool
	require.NoError(t, cacheFile.view(func(tx *bbolt.Tx) error {
		bucket := cacheFile.bucket(tx, bucketRDRC)
		if bucket == nil {
			return nil
		}
		bucket = bucket.Bucket([]byte("local"))
		if bucket == nil {
			return nil
		}
		exists = bucket.Get([]byte{0, 1, 'e'}) != nil
		return nil
	}))
	require.True(t, exists)
}

func TestCacheFileCloseRejectsLaterWritesAndAsyncSaves(t *testing.T) {
	t.Parallel()

	cacheFile := newStartedTestCacheFile(t, option.CacheFileOptions{
		StoreFakeIP: true,
		StoreRDRC:   true,
		StoreDNS:    true,
	})
	require.NoError(t, cacheFile.Close())

	cacheFile.FakeIPSaveMetadataAsync(testFakeIPMetadata("198.18.0.1"))
	cacheFile.SaveRDRCAsync("local", "example.com.", 1, logger.NOP())
	cacheFile.SaveDNSCacheAsync("local", "example.com.", 1, []byte{1, 2, 3}, time.Now().Add(time.Minute), logger.NOP())

	err := cacheFile.SaveDNSCache("local", "example.com.", 1, []byte{1}, time.Now().Add(time.Minute))
	require.True(t, errors.Is(err, os.ErrClosed), "expected os.ErrClosed, got %v", err)
}

func newStartedTestCacheFile(t *testing.T, options option.CacheFileOptions) *CacheFile {
	t.Helper()

	options.Path = filepath.Join(t.TempDir(), "cache.db")
	cacheFile := New(context.Background(), logger.NOP(), options)
	require.NoError(t, cacheFile.start())
	t.Cleanup(func() {
		_ = cacheFile.Close()
	})
	return cacheFile
}

func testFakeIPMetadata(current string) *adapter.FakeIPMetadata {
	return &adapter.FakeIPMetadata{
		Inet4Range:   netip.MustParsePrefix("198.18.0.0/15"),
		Inet6Range:   netip.MustParsePrefix("fc00::/18"),
		Inet4Current: netip.MustParseAddr(current),
		Inet6Current: netip.MustParseAddr("fc00::1"),
	}
}
