package cachefile

import (
	"net/netip"
	"os"
	"time"

	"github.com/sagernet/bbolt"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/singlink/singlink/adapter"
	C "github.com/singlink/singlink/constant"
)

const fakeipBucketPrefix = "fakeip_"

var (
	bucketFakeIP        = []byte(fakeipBucketPrefix + "address")
	bucketFakeIPDomain4 = []byte(fakeipBucketPrefix + "domain4")
	bucketFakeIPDomain6 = []byte(fakeipBucketPrefix + "domain6")
	keyMetadata         = []byte(fakeipBucketPrefix + "metadata")
)

func (c *CacheFile) FakeIPMetadata() *adapter.FakeIPMetadata {
	var metadata adapter.FakeIPMetadata
	err := c.batch(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketFakeIP)
		if bucket == nil {
			return os.ErrNotExist
		}
		metadataBinary := bucket.Get(keyMetadata)
		if len(metadataBinary) == 0 {
			return os.ErrInvalid
		}
		err := bucket.Delete(keyMetadata)
		if err != nil {
			return err
		}
		return metadata.UnmarshalBinary(metadataBinary)
	})
	if err != nil {
		return nil
	}
	return &metadata
}

func (c *CacheFile) FakeIPSaveMetadata(metadata *adapter.FakeIPMetadata) error {
	metadata = cloneFakeIPMetadata(metadata)
	if metadata == nil {
		return nil
	}
	c.saveMetadataAccess.Lock()
	c.saveMetadataSeq++
	if c.saveMetadataTimer != nil {
		c.saveMetadataTimer.Stop()
		c.saveMetadataTimer = nil
	}
	c.saveMetadata = nil
	c.saveMetadataAccess.Unlock()
	c.saveMetadataWrite.Lock()
	defer c.saveMetadataWrite.Unlock()
	return c.saveFakeIPMetadata(metadata)
}

func (c *CacheFile) saveFakeIPMetadata(metadata *adapter.FakeIPMetadata) error {
	return c.batch(func(tx *bbolt.Tx) error {
		return saveFakeIPMetadataInTx(tx, metadata)
	})
}

func saveFakeIPMetadataInTx(tx *bbolt.Tx, metadata *adapter.FakeIPMetadata) error {
	bucket, err := tx.CreateBucketIfNotExists(bucketFakeIP)
	if err != nil {
		return err
	}
	metadataBinary, err := metadata.MarshalBinary()
	if err != nil {
		return err
	}
	return bucket.Put(keyMetadata, metadataBinary)
}

func (c *CacheFile) FakeIPSaveMetadataAsync(metadata *adapter.FakeIPMetadata) {
	metadata = cloneFakeIPMetadata(metadata)
	if metadata == nil || c.closed.Load() {
		return
	}
	c.saveMetadataAccess.Lock()
	if c.closed.Load() {
		c.saveMetadataAccess.Unlock()
		return
	}
	c.saveMetadata = metadata
	c.saveMetadataSeq++
	if c.saveMetadataTimer == nil {
		c.scheduleFakeIPMetadataFlushLocked(C.FakeIPMetadataSaveInterval)
	} else {
		c.saveMetadataTimer.Reset(C.FakeIPMetadataSaveInterval)
	}
	c.saveMetadataAccess.Unlock()
}

func (c *CacheFile) scheduleFakeIPMetadataFlushLocked(delay time.Duration) {
	if c.saveMetadataTimer == nil {
		c.saveMetadataTimer = time.AfterFunc(delay, func() {
			c.flushFakeIPMetadata()
		})
		return
	}
	c.saveMetadataTimer.Reset(delay)
}

func (c *CacheFile) flushFakeIPMetadata() {
	if !c.beginAsyncWrite() {
		c.saveMetadataAccess.Lock()
		if !c.closed.Load() && c.saveMetadata != nil {
			c.scheduleFakeIPMetadataFlushLocked(cacheFileAsyncWriteRetryDelay)
		}
		c.saveMetadataAccess.Unlock()
		return
	}
	defer c.endAsyncWrite()
	c.saveMetadataAccess.Lock()
	metadata := cloneFakeIPMetadata(c.saveMetadata)
	sequence := c.saveMetadataSeq
	c.saveMetadataAccess.Unlock()
	if metadata == nil {
		return
	}
	c.saveMetadataWrite.Lock()
	defer c.saveMetadataWrite.Unlock()
	c.saveMetadataAccess.Lock()
	if c.closed.Load() || sequence != c.saveMetadataSeq {
		c.saveMetadataAccess.Unlock()
		return
	}
	c.saveMetadataAccess.Unlock()
	_ = c.saveFakeIPMetadata(metadata)
	c.saveMetadataAccess.Lock()
	if sequence == c.saveMetadataSeq {
		c.saveMetadata = nil
	}
	c.saveMetadataAccess.Unlock()
}

func (c *CacheFile) saveFakeIPMetadataOnClose(metadata *adapter.FakeIPMetadata) (err error) {
	c.dbAccess.RLock()
	db := c.DB
	if db == nil {
		c.dbAccess.RUnlock()
		return os.ErrClosed
	}
	defer c.dbAccess.RUnlock()
	defer func() {
		if r := recover(); r != nil {
			err = E.New("database corrupted: ", r)
		}
	}()
	return db.Batch(func(tx *bbolt.Tx) error {
		return saveFakeIPMetadataInTx(tx, metadata)
	})
}

func (c *CacheFile) FakeIPStore(address netip.Addr, domain string) error {
	return c.batch(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(bucketFakeIP)
		if err != nil {
			return err
		}
		oldDomain := bucket.Get(address.AsSlice())
		err = bucket.Put(address.AsSlice(), []byte(domain))
		if err != nil {
			return err
		}
		if address.Is4() {
			bucket, err = tx.CreateBucketIfNotExists(bucketFakeIPDomain4)
		} else {
			bucket, err = tx.CreateBucketIfNotExists(bucketFakeIPDomain6)
		}
		if err != nil {
			return err
		}
		if oldDomain != nil {
			if err := bucket.Delete(oldDomain); err != nil {
				return err
			}
		}
		return bucket.Put([]byte(domain), address.AsSlice())
	})
}

func (c *CacheFile) FakeIPStoreAsync(address netip.Addr, domain string, logger logger.Logger) {
	if !c.beginAsyncWrite() {
		return
	}
	c.saveFakeIPAccess.Lock()
	if oldDomain, loaded := c.saveDomain[address]; loaded {
		if address.Is4() {
			delete(c.saveAddress4, oldDomain)
		} else {
			delete(c.saveAddress6, oldDomain)
		}
	}
	c.saveDomain[address] = domain
	if address.Is4() {
		c.saveAddress4[domain] = address
	} else {
		c.saveAddress6[domain] = address
	}
	c.saveFakeIPAccess.Unlock()
	go func() {
		defer c.endAsyncWrite()
		err := c.FakeIPStore(address, domain)
		if err != nil && !c.closed.Load() {
			logger.Warn("save FakeIP cache: ", err)
		}
		c.saveFakeIPAccess.Lock()
		if currentDomain, loaded := c.saveDomain[address]; loaded && currentDomain == domain {
			delete(c.saveDomain, address)
		}
		if address.Is4() {
			if currentAddress, loaded := c.saveAddress4[domain]; loaded && currentAddress == address {
				delete(c.saveAddress4, domain)
			}
		} else {
			if currentAddress, loaded := c.saveAddress6[domain]; loaded && currentAddress == address {
				delete(c.saveAddress6, domain)
			}
		}
		c.saveFakeIPAccess.Unlock()
	}()
}

func (c *CacheFile) FakeIPLoad(address netip.Addr) (string, bool) {
	c.saveFakeIPAccess.RLock()
	cachedDomain, cached := c.saveDomain[address]
	c.saveFakeIPAccess.RUnlock()
	if cached {
		return cachedDomain, true
	}
	var domain string
	_ = c.view(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketFakeIP)
		if bucket == nil {
			return nil
		}
		domain = string(bucket.Get(address.AsSlice()))
		return nil
	})
	return domain, domain != ""
}

func (c *CacheFile) FakeIPLoadDomain(domain string, isIPv6 bool) (netip.Addr, bool) {
	var (
		cachedAddress netip.Addr
		cached        bool
	)
	c.saveFakeIPAccess.RLock()
	if !isIPv6 {
		cachedAddress, cached = c.saveAddress4[domain]
	} else {
		cachedAddress, cached = c.saveAddress6[domain]
	}
	c.saveFakeIPAccess.RUnlock()
	if cached {
		return cachedAddress, true
	}
	var address netip.Addr
	_ = c.view(func(tx *bbolt.Tx) error {
		var bucket *bbolt.Bucket
		if isIPv6 {
			bucket = tx.Bucket(bucketFakeIPDomain6)
		} else {
			bucket = tx.Bucket(bucketFakeIPDomain4)
		}
		if bucket == nil {
			return nil
		}
		address = M.AddrFromIP(bucket.Get([]byte(domain)))
		return nil
	})
	return address, address.IsValid()
}

func (c *CacheFile) FakeIPReset() error {
	return c.batch(func(tx *bbolt.Tx) error {
		err := tx.DeleteBucket(bucketFakeIP)
		if err != nil {
			return err
		}
		err = tx.DeleteBucket(bucketFakeIPDomain4)
		if err != nil {
			return err
		}
		return tx.DeleteBucket(bucketFakeIPDomain6)
	})
}

func cloneFakeIPMetadata(metadata *adapter.FakeIPMetadata) *adapter.FakeIPMetadata {
	if metadata == nil {
		return nil
	}
	metadataCopy := *metadata
	return &metadataCopy
}
