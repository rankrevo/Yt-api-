package metrics

import (
	"sync/atomic"
	"time"
)

type Registry struct {
	ActiveJobs     atomic.Int64
	QueuedJobs     atomic.Int64
	CompletedJobs  atomic.Int64
	FailedJobs     atomic.Int64
	Workers        atomic.Int64
	QueueCapacity  atomic.Int64
	RateLimit      atomic.Int64
	UptimeStart    time.Time
	SuccessCount   atomic.Int64
	ErrorCount     atomic.Int64
	SessionsActive atomic.Int64
}

func NewRegistry() *Registry {
	r := &Registry{UptimeStart: time.Now()}
	return r
}

func (r *Registry) SuccessRate() float64 {
	s := r.SuccessCount.Load()
	e := r.ErrorCount.Load()
	t := s + e
	if t == 0 {
		return 1.0
	}
	return float64(s) / float64(t)
}

func (r *Registry) UptimeSeconds() int64 {
	return int64(time.Since(r.UptimeStart).Seconds())
}
