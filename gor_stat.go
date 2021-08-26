package main

import (
	"fmt"
	"runtime"
	"time"
)

type GorStat struct {
	statName  string
	startTime int64
	rateMs    int
	latest    int
	mean      float32
	max       int
	count     int
	total     int
	goroutine int
}

func NewGorStat(statName string, rateMs int) (s *GorStat) {
	s = new(GorStat)
	s.statName = statName
	s.rateMs = rateMs
	s.latest = 0
	s.mean = 0
	s.max = 0
	s.count = 0
	s.total = 0
	s.startTime = time.Now().Unix()

	if Settings.Stats {
		go s.reportStats()
	}
	return
}

func (s *GorStat) Write(latest int) {
	if Settings.Stats {
		if latest > s.max {
			s.max = latest
		}
		s.latest = latest
		s.count = s.count + 1
		s.total = s.total + latest
		// update mean
		if latest != 0 {
			elapsed := int(time.Now().Unix() - s.startTime)
			if elapsed > 0 {
				s.mean = float32(s.total / elapsed)
			}
		}
	}
}

func (s *GorStat) Reset() {
	s.latest = 0
	s.goroutine = 0
}

func (s *GorStat) reportStats() {
	for {
		// wait for output
		time.Sleep(time.Duration(s.rateMs) * time.Millisecond)

		// update goroutine num
		s.goroutine = runtime.NumGoroutine()

		Debug(0, fmt.Sprintf("%#v\n", s))
		s.Reset()
	}
}
