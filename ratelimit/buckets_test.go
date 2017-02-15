package ratelimit

import "testing"

func TestBucket(t *testing.T) {
	b := Bucket{}

	// Bucket should start out empty
	if !b.isEmpty(0) {
		t.Error("Bucket didn't start empty!")
		return
	}

	// Fill up the bucket
	for i := 0; i<maxTokensPerBurst; i+=1 {
		if(b.addToken(1) == false) {
			t.Error("Bucket filled up early!")
			return
		}
	}

	// Next one should overflow
	if(b.addToken(0) == true) {
		t.Error("Bucket should have filled up by now!")
	}

	drainTime := b.DrainTime()

	btest := b
	if btest.addToken(drainTime-1) {
		t.Error("Bucket drained faster than DrainTime said!")
		return
	}

	btest = b
	if !btest.addToken(drainTime+1) {
		t.Error("Bucket drained slower than DrainTime said!")
		return
	}
}

