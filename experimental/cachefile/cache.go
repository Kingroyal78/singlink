package cachefile

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/bbolt"
	bboltErrors "github.com/sagernet/bbolt/errors"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service/filemanager"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/experimental/deprecated"
	"github.com/singlink/singlink/option"
)

const (
	cacheFileMaxAsyncWrites       = 16
	cacheFileAsyncWriteRetryDelay = time.Second
)

var (
	bucketSelected = []byte("selected")
	bucketExpand   = []byte("group_expand")
	bucketMode     = []byte("clash_mode")
	bucketRuleSet  = []byte("rule_set")

	bucketNameList = []string{
		string(bucketSelected),
		string(bucketExpand),
		string(bucketMode),
		string(bucketRuleSet),
		string(bucketRDRC),
		string(bucketDNSCache),
	}

	cacheIDDefault = []byte("default")
)

var _ adapter.CacheFile = (*CacheFile)(nil)

type CacheFile struct {
	ctx                context.Context
	logger             logger.Logger
	path               string
	cacheID            []byte
	storeFakeIP        bool
	storeRDRC          bool
	storeDNS           bool
	disableExpire      bool
	rdrcTimeout        time.Duration
	optimisticTimeout  time.Duration
	DB                 *bbolt.DB
	resetAccess        sync.Mutex
	dbAccess           sync.RWMutex
	closed             atomic.Bool
	asyncAccess        sync.Mutex
	asyncWG            sync.WaitGroup
	asyncWriteTokens   chan struct{}
	cleanupAccess      sync.Mutex
	cleanupCancel      context.CancelFunc
	cleanupWG          sync.WaitGroup
	saveMetadataAccess sync.Mutex
	saveMetadataWrite  sync.Mutex
	saveMetadataTimer  *time.Timer
	saveMetadata       *adapter.FakeIPMetadata
	saveMetadataSeq    uint64
	saveFakeIPAccess   sync.RWMutex
	saveDomain         map[netip.Addr]string
	saveAddress4       map[string]netip.Addr
	saveAddress6       map[string]netip.Addr
	saveRDRCAccess     sync.RWMutex
	saveRDRC           map[saveCacheKey]bool
	saveDNSCacheAccess sync.RWMutex
	saveDNSCache       map[saveCacheKey]saveDNSCacheEntry
}

type saveCacheKey struct {
	TransportName string
	QuestionName  string
	QType         uint16
}

type saveDNSCacheEntry struct {
	rawMessage []byte
	expireAt   time.Time
	sequence   uint64
	saving     bool
}

func New(ctx context.Context, logger logger.Logger, options option.CacheFileOptions) *CacheFile {
	var path string
	if options.Path != "" {
		path = options.Path
	} else {
		path = "cache.db"
	}
	var cacheIDBytes []byte
	if options.CacheID != "" {
		cacheIDBytes = append([]byte{0}, []byte(options.CacheID)...)
	}
	if options.StoreRDRC {
		deprecated.Report(ctx, deprecated.OptionStoreRDRC)
	}
	var rdrcTimeout time.Duration
	if options.StoreRDRC {
		if options.RDRCTimeout > 0 {
			rdrcTimeout = time.Duration(options.RDRCTimeout)
		} else {
			rdrcTimeout = 7 * 24 * time.Hour
		}
	}
	return &CacheFile{
		ctx:              ctx,
		logger:           logger,
		path:             filemanager.BasePath(ctx, path),
		cacheID:          cacheIDBytes,
		storeFakeIP:      options.StoreFakeIP,
		storeRDRC:        options.StoreRDRC,
		storeDNS:         options.StoreDNS,
		rdrcTimeout:      rdrcTimeout,
		asyncWriteTokens: make(chan struct{}, cacheFileMaxAsyncWrites),
		saveDomain:       make(map[netip.Addr]string),
		saveAddress4:     make(map[string]netip.Addr),
		saveAddress6:     make(map[string]netip.Addr),
		saveRDRC:         make(map[saveCacheKey]bool),
		saveDNSCache:     make(map[saveCacheKey]saveDNSCacheEntry),
	}
}

func (c *CacheFile) Name() string {
	return "cache-file"
}

func (c *CacheFile) Dependencies() []string {
	return nil
}

func (c *CacheFile) SetOptimisticTimeout(timeout time.Duration) {
	c.optimisticTimeout = timeout
}

func (c *CacheFile) SetDisableExpire(disableExpire bool) {
	c.disableExpire = disableExpire
}

func (c *CacheFile) Start(stage adapter.StartStage) error {
	switch stage {
	case adapter.StartStateInitialize:
		return c.start()
	case adapter.StartStateStart:
		c.startCacheCleanup()
	}
	return nil
}

func (c *CacheFile) startCacheCleanup() {
	if c.storeDNS {
		c.clearRDRC()
		c.cleanupDNSCache(c.ctx)
		interval := c.optimisticTimeout / 2
		if interval <= 0 {
			interval = time.Hour
		}
		c.startCacheCleanupLoop(interval, c.cleanupDNSCache)
	} else if c.storeRDRC {
		c.cleanupRDRC(c.ctx)
		interval := c.rdrcTimeout / 2
		if interval <= 0 {
			interval = time.Hour
		}
		c.startCacheCleanupLoop(interval, c.cleanupRDRC)
	}
}

func (c *CacheFile) start() error {
	const fileMode = 0o666
	options := bbolt.Options{Timeout: time.Second}
	var (
		db  *bbolt.DB
		err error
	)
	for range 10 {
		db, err = bbolt.Open(c.path, fileMode, &options)
		if err == nil {
			break
		}
		if errors.Is(err, bboltErrors.ErrTimeout) {
			continue
		}
		if E.IsMulti(err, bboltErrors.ErrInvalid, bboltErrors.ErrChecksum, bboltErrors.ErrVersionMismatch) {
			rmErr := os.Remove(c.path)
			if rmErr != nil {
				return err
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	err = filemanager.Chown(c.ctx, c.path)
	if err != nil {
		db.Close()
		return E.Cause(err, "platform chown")
	}
	err = db.Batch(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			if name[0] == 0 {
				return b.ForEachBucket(func(k []byte) error {
					bucketName := string(k)
					if !(common.Contains(bucketNameList, bucketName)) {
						_ = b.DeleteBucket(name)
					}
					return nil
				})
			} else {
				bucketName := string(name)
				if !(common.Contains(bucketNameList, bucketName) || strings.HasPrefix(bucketName, fakeipBucketPrefix)) {
					_ = tx.DeleteBucket(name)
				}
			}
			return nil
		})
	})
	if err != nil {
		db.Close()
		return err
	}
	c.dbAccess.Lock()
	if c.closed.Load() {
		c.dbAccess.Unlock()
		db.Close()
		return os.ErrClosed
	}
	c.DB = db
	c.dbAccess.Unlock()
	return nil
}

func (c *CacheFile) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.stopCacheCleanup()
	pendingMetadata := c.stopFakeIPMetadataTimer()
	c.asyncAccess.Lock()
	c.asyncWG.Wait()
	c.asyncAccess.Unlock()
	if pendingMetadata != nil {
		c.saveMetadataWrite.Lock()
		err := c.saveFakeIPMetadataOnClose(pendingMetadata)
		c.saveMetadataWrite.Unlock()
		if err != nil && !errors.Is(err, os.ErrClosed) {
			c.logger.Warn("save FakeIP metadata: ", err)
		}
	}
	c.clearPendingAsyncState()
	c.dbAccess.Lock()
	db := c.DB
	c.DB = nil
	c.dbAccess.Unlock()
	if db == nil {
		return nil
	}
	return db.Close()
}

func (c *CacheFile) view(fn func(tx *bbolt.Tx) error) (err error) {
	db, unlock, err := c.openDB()
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			unlock()
			unlock = nil
			c.resetDB()
			err = E.New("database corrupted: ", r)
		}
		if unlock != nil {
			unlock()
		}
	}()
	return db.View(fn)
}

func (c *CacheFile) batch(fn func(tx *bbolt.Tx) error) (err error) {
	db, unlock, err := c.openDB()
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			unlock()
			unlock = nil
			c.resetDB()
			err = E.New("database corrupted: ", r)
		}
		if unlock != nil {
			unlock()
		}
	}()
	return db.Batch(fn)
}

func (c *CacheFile) update(fn func(tx *bbolt.Tx) error) (err error) {
	db, unlock, err := c.openDB()
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			unlock()
			unlock = nil
			c.resetDB()
			err = E.New("database corrupted: ", r)
		}
		if unlock != nil {
			unlock()
		}
	}()
	return db.Update(fn)
}

func (c *CacheFile) resetDB() {
	if c.closed.Load() {
		return
	}
	c.resetAccess.Lock()
	defer c.resetAccess.Unlock()
	c.dbAccess.Lock()
	defer c.dbAccess.Unlock()
	if c.DB != nil {
		c.DB.Close()
	}
	os.Remove(c.path)
	db, err := bbolt.Open(c.path, 0o666, &bbolt.Options{Timeout: time.Second})
	if err == nil {
		_ = filemanager.Chown(c.ctx, c.path)
		c.DB = db
	}
}

func (c *CacheFile) openDB() (*bbolt.DB, func(), error) {
	c.dbAccess.RLock()
	if c.closed.Load() || c.DB == nil {
		c.dbAccess.RUnlock()
		return nil, nil, os.ErrClosed
	}
	return c.DB, c.dbAccess.RUnlock, nil
}

func (c *CacheFile) startCacheCleanupLoop(interval time.Duration, cleanupFunc func(context.Context)) {
	c.cleanupAccess.Lock()
	if c.closed.Load() {
		c.cleanupAccess.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.cleanupCancel = cancel
	c.cleanupWG.Add(1)
	c.cleanupAccess.Unlock()
	go func() {
		defer c.cleanupWG.Done()
		c.loopCacheCleanup(ctx, interval, cleanupFunc)
	}()
}

func (c *CacheFile) stopCacheCleanup() {
	c.cleanupAccess.Lock()
	cancel := c.cleanupCancel
	c.cleanupCancel = nil
	c.cleanupAccess.Unlock()
	if cancel != nil {
		cancel()
	}
	c.cleanupWG.Wait()
}

func (c *CacheFile) beginAsyncWrite() bool {
	c.asyncAccess.Lock()
	defer c.asyncAccess.Unlock()
	if c.closed.Load() {
		return false
	}
	if c.asyncWriteTokens == nil {
		c.asyncWriteTokens = make(chan struct{}, cacheFileMaxAsyncWrites)
	}
	select {
	case c.asyncWriteTokens <- struct{}{}:
		c.asyncWG.Add(1)
		return true
	default:
		return false
	}
}

func (c *CacheFile) endAsyncWrite() {
	<-c.asyncWriteTokens
	c.asyncWG.Done()
}

func (c *CacheFile) stopFakeIPMetadataTimer() *adapter.FakeIPMetadata {
	c.saveMetadataAccess.Lock()
	if c.saveMetadataTimer != nil {
		c.saveMetadataTimer.Stop()
		c.saveMetadataTimer = nil
	}
	metadata := cloneFakeIPMetadata(c.saveMetadata)
	c.saveMetadata = nil
	c.saveMetadataSeq++
	c.saveMetadataAccess.Unlock()
	return metadata
}

func (c *CacheFile) clearPendingAsyncState() {
	c.saveFakeIPAccess.Lock()
	clear(c.saveDomain)
	clear(c.saveAddress4)
	clear(c.saveAddress6)
	c.saveFakeIPAccess.Unlock()
	c.saveRDRCAccess.Lock()
	clear(c.saveRDRC)
	c.saveRDRCAccess.Unlock()
	c.saveDNSCacheAccess.Lock()
	clear(c.saveDNSCache)
	c.saveDNSCacheAccess.Unlock()
}

func (c *CacheFile) StoreFakeIP() bool {
	return c.storeFakeIP
}

func (c *CacheFile) LoadMode() string {
	var mode string
	c.view(func(t *bbolt.Tx) error {
		bucket := t.Bucket(bucketMode)
		if bucket == nil {
			return nil
		}
		var modeBytes []byte
		if len(c.cacheID) > 0 {
			modeBytes = bucket.Get(c.cacheID)
		} else {
			modeBytes = bucket.Get(cacheIDDefault)
		}
		mode = string(modeBytes)
		return nil
	})
	return mode
}

func (c *CacheFile) StoreMode(mode string) error {
	return c.batch(func(t *bbolt.Tx) error {
		bucket, err := t.CreateBucketIfNotExists(bucketMode)
		if err != nil {
			return err
		}
		if len(c.cacheID) > 0 {
			return bucket.Put(c.cacheID, []byte(mode))
		} else {
			return bucket.Put(cacheIDDefault, []byte(mode))
		}
	})
}

func (c *CacheFile) bucket(t *bbolt.Tx, key []byte) *bbolt.Bucket {
	if c.cacheID == nil {
		return t.Bucket(key)
	}
	bucket := t.Bucket(c.cacheID)
	if bucket == nil {
		return nil
	}
	return bucket.Bucket(key)
}

func (c *CacheFile) createBucket(t *bbolt.Tx, key []byte) (*bbolt.Bucket, error) {
	if c.cacheID == nil {
		return t.CreateBucketIfNotExists(key)
	}
	bucket, err := t.CreateBucketIfNotExists(c.cacheID)
	if bucket == nil {
		return nil, err
	}
	return bucket.CreateBucketIfNotExists(key)
}

func (c *CacheFile) LoadSelected(group string) string {
	var selected string
	c.view(func(t *bbolt.Tx) error {
		bucket := c.bucket(t, bucketSelected)
		if bucket == nil {
			return nil
		}
		selectedBytes := bucket.Get([]byte(group))
		if len(selectedBytes) > 0 {
			selected = string(selectedBytes)
		}
		return nil
	})
	return selected
}

func (c *CacheFile) StoreSelected(group, selected string) error {
	return c.batch(func(t *bbolt.Tx) error {
		bucket, err := c.createBucket(t, bucketSelected)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(group), []byte(selected))
	})
}

func (c *CacheFile) LoadGroupExpand(group string) (isExpand bool, loaded bool) {
	c.view(func(t *bbolt.Tx) error {
		bucket := c.bucket(t, bucketExpand)
		if bucket == nil {
			return nil
		}
		expandBytes := bucket.Get([]byte(group))
		if len(expandBytes) == 1 {
			isExpand = expandBytes[0] == 1
			loaded = true
		}
		return nil
	})
	return
}

func (c *CacheFile) StoreGroupExpand(group string, isExpand bool) error {
	return c.batch(func(t *bbolt.Tx) error {
		bucket, err := c.createBucket(t, bucketExpand)
		if err != nil {
			return err
		}
		if isExpand {
			return bucket.Put([]byte(group), []byte{1})
		} else {
			return bucket.Put([]byte(group), []byte{0})
		}
	})
}

func (c *CacheFile) LoadRuleSet(tag string) *adapter.SavedBinary {
	var savedSet adapter.SavedBinary
	err := c.view(func(t *bbolt.Tx) error {
		bucket := c.bucket(t, bucketRuleSet)
		if bucket == nil {
			return os.ErrNotExist
		}
		setBinary := bucket.Get([]byte(tag))
		if len(setBinary) == 0 {
			return os.ErrInvalid
		}
		return savedSet.UnmarshalBinary(setBinary)
	})
	if err != nil {
		return nil
	}
	return &savedSet
}

func (c *CacheFile) SaveRuleSet(tag string, set *adapter.SavedBinary) error {
	return c.batch(func(t *bbolt.Tx) error {
		bucket, err := c.createBucket(t, bucketRuleSet)
		if err != nil {
			return err
		}
		setBinary, err := set.MarshalBinary()
		if err != nil {
			return err
		}
		return bucket.Put([]byte(tag), setBinary)
	})
}
