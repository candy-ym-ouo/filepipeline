package service

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

type RetryPolicy struct {
	Base   time.Duration
	Max    time.Duration
	Jitter float64
	mu     sync.Mutex
	random *rand.Rand
}

func NewRetryPolicy() *RetryPolicy {
	return &RetryPolicy{Base: time.Second, Max: 30 * time.Second, Jitter: 0.20,
		random: rand.New(rand.NewSource(time.Now().UnixNano()))}
}
func (p *RetryPolicy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := p.Base
	if base <= 0 {
		base = time.Second
	}
	maximum := p.Max
	if maximum <= 0 {
		maximum = 30 * time.Second
	}
	exponent := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(base) * exponent)
	if delay > maximum || delay < 0 {
		delay = maximum
	}
	jitter := p.Jitter
	if jitter <= 0 {
		return delay
	}
	p.mu.Lock()
	factor := 1 - jitter + p.random.Float64()*(2*jitter)
	p.mu.Unlock()
	return time.Duration(float64(delay) * factor)
}
func (p *RetryPolicy) SetSource(source rand.Source) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.random = rand.New(source)
}
