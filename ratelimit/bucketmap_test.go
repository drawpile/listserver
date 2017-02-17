package ratelimit

import "testing"

func TestBucketMap(t *testing.T) {
	bm := NewBucketMap()

	for i := 0; i < maxTokensPerBurst; i += 1 {
		if bm.addToken("1", 0) == false {
			t.Error("First bucket filled up early!")
			return
		}
	}

	if bm.addToken("1", 0) {
		t.Error("First bucket should be full!")
		return
	}

	for i := 0; i < maxTokensPerBurst; i += 1 {
		if bm.addToken("2", 0) == false {
			t.Error("First bucket filled up early!")
		}
	}

	if bm.addToken("2", 0) {
		t.Error("Second bucket should be full!")
		return
	}

	if bm.addToken("2", cleanupInterval+1) == false {
		t.Error("Second bucket didn't drain!")
		return
	}

	// Since we passed cleanupInterval previously, the first bucket should
	// have been removed by now
	if bm.nextCleanup != cleanupInterval*2+1 {
		t.Error("Next cleanup wasn't scheduled!")
		return
	}

	if len(bm.buckets) != 1 {
		t.Error("BucketMap cleanup failed! There are", len(bm.buckets), "buckets in memory, expected just one!")
		return
	}
}
