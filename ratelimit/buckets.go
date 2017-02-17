package ratelimit

import "time"

const (
	burstDuration     = 10 // seconds
	maxTokensPerBurst = 20
	penaltyTimeLimit  = 10 * 60 // seconds
)

type Bucket struct {
	tokens      int64
	lastDrained int64 // unix time
}

func (b *Bucket) AddToken() bool {
	return b.addToken(int64(time.Now().Unix()))
}

func (b *Bucket) DrainTime() int64 {
	return (b.tokens - maxTokensPerBurst + 1) * burstDuration / maxTokensPerBurst
}

func (b *Bucket) IsEmpty() bool {
	return b.isEmpty(int64(time.Now().Unix()))
}

func (b *Bucket) isEmpty(now int64) bool {
	b.drain(now)
	return b.tokens == 0
}

func (b *Bucket) drain(now int64) {
	drain := (now - b.lastDrained) * maxTokensPerBurst / burstDuration
	if drain > 0 {
		b.tokens = b.tokens - drain
		if b.tokens < 0 {
			b.tokens = 0
		}
		b.lastDrained = now
	}
}

func (b *Bucket) addToken(now int64) bool {
	b.drain(now)
	if b.tokens > maxTokensPerBurst {
		// Penalty tokens: progressively increase until penalty time limit is reached
		if b.tokens < maxTokensPerBurst*(penaltyTimeLimit/burstDuration) {
			b.tokens += b.tokens / 2
		}
	}

	b.tokens += 1
	return b.tokens <= maxTokensPerBurst
}
