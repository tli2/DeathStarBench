package tracing

import (
	"sync"
	"time"
	"github.com/rs/zerolog/log"
	"github.com/montanaflynn/stats"
)

const N_COUNT = 5000

type Counter struct {
	mu    sync.Mutex
	label string
	count int64
	vals  []int64
}

func MakeCounter(str string) *Counter {
	return &Counter{label: str, count: 0, vals: make([]int64, N_COUNT)}
}

func (c *Counter) AddOne(val int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals[c.count] = val
	c.count += 1
	if c.count == N_COUNT {
		c.printStats()
		c.count = 0
		c.vals = make([]int64, N_COUNT)
	}
}

func (c* Counter) AddTimeSince(t0 time.Time) {
	c.AddOne(time.Since(t0).Microseconds())
}

func (c* Counter) printStats() {
	fVals := make([]float64, N_COUNT)
	for i := range c.vals {
		fVals[i] = float64(c.vals[i])
	}
	mean, _ := stats.Mean(fVals)
	std, _ := stats.StandardDeviation(fVals)
	lat50P, _ := stats.Percentile(fVals, 50)
	lat75P, _ := stats.Percentile(fVals, 75)
	lat99P, _ := stats.Percentile(fVals, 99)
	min, _ := stats.Min(fVals)
	max, _ := stats.Max(fVals)
	log.Info().Msgf("Stats for %v: mean=%v; std=%v; min=%v; max=%v; 50P,75P,99P=%v,%v,%v;",
		c.label, mean, std, min, max, lat50P, lat75P, lat99P)
}
