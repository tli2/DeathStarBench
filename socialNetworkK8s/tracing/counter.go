package tracing

import (
	"sync"
	"time"
	"math"
	"github.com/rs/zerolog/log"
)

type Counter struct {
	mu    sync.Mutex
	count int64
	sum    int64
	ssum  int64
	max   int64
	min   int64
	label string
}

func MakeCounter(str string) *Counter {
	return &Counter{label: str, count: 0, sum: 0, ssum: 0, min: 10000000, max: 0}
}

func (c *Counter) AddOne(val int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count += 1
	c.sum += val
	c.ssum += val*val
	if val > c.max {
		c.max = val
	}
	if val < c.min {
		c.min = val
	}
	if c.count == 1000 {
		avg := c.sum / c.count
		std := math.Sqrt(float64(c.ssum/c.count - avg*avg))
		log.Info().Msgf(
			"Stats for %v: max = %v; min=%v, avg=%v, std=%v", c.label, c.max, c.min, avg, std)
		c.count = 0
		c.sum = 0
		c.ssum = 0
		c.max = 0
		c.min = 10000000
	}
}

func (c* Counter) AddTimeSince(t0 time.Time) {
	c.AddOne(time.Since(t0).Microseconds())
}
