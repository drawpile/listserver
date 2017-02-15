package ratelimit

import (
	"sync"
	"time"
)

const cleanupInterval = 10 * 60 // seconds

type BucketMap struct {
	buckets map[string]*Bucket
	nextCleanup int64
	mutex sync.Mutex
}

func NewBucketMap() *BucketMap {
	return &BucketMap{
		make(map[string]*Bucket),
		0,
		sync.Mutex{},
	}
}

func (bm *BucketMap) AddToken(key string) bool {
	return bm.addToken(key, int64(time.Now().Unix()))
}

func (bm *BucketMap) addToken(key string, now int64) bool {
	bm.mutex.Lock()
	if bm.buckets[key] == nil {
		bm.buckets[key] = &Bucket{}
	}
	ok := bm.buckets[key].addToken(now)
	if bm.nextCleanup < now {
		bm.nextCleanup = now + cleanupInterval
		bm.cleanup(now)
	}
	bm.mutex.Unlock()
	return ok
}

func (bm *BucketMap) DrainTime(key string) int64 {
	bm.mutex.Lock()
	var time int64
	if bucket := bm.buckets[key]; bucket != nil {
		time = bucket.DrainTime()
	}
	bm.mutex.Unlock()
	return time
}

func (bm *BucketMap) cleanup(now int64) {
	for k, b := range bm.buckets {
		if b.isEmpty(now) {
			delete(bm.buckets, k)
		}
	}
}

