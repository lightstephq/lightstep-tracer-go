package benchlib

import (
	"math"

	"github.com/GaryBoone/GoStats/stats"
)

type Stats struct {
	stats.Stats
}

type DerivedStats struct {
	n              int
	variance, mean float64
}

func (s Stats) Sub(o Stats) DerivedStats {
	if s.Size() != o.Size() {
		panic("n mismatch")
	}
	return DerivedStats{s.Size(), s.PopulationVariance() + o.PopulationVariance(), s.Mean() - o.Mean()}
}

func (s DerivedStats) Div(f float64) DerivedStats {
	return DerivedStats{s.n, s.variance / (f * f), s.mean / f}
}

// Int64Mean returns the mean of an integer array as a float
func Int64Mean(nums []int64) (mean float64) {
	if len(nums) == 0 {
		return 0.0
	}
	for _, n := range nums {
		mean += float64(n)
	}
	return mean / float64(len(nums))
}

// Int64StandardDeviation returns the standard deviation of the slice
// as a float
func Int64StandardDeviation(nums []int64) (dev float64) {
	if len(nums) == 0 {
		return 0.0
	}

	m := Int64Mean(nums)
	for _, n := range nums {
		dev += (float64(n) - m) * (float64(n) - m)
	}
	dev = math.Pow(dev/float64(len(nums)), 0.5)
	return dev
}

// Int64NormalConfidenceInterval returns the 95% confidence interval for the mean
// as two float values, the lower and the upper bounds and assuming a normal
// distribution
func Int64NormalConfidenceInterval(nums []int64) (lower float64, upper float64) {
	conf := 1.95996 // 95% confidence for the mean, http://bit.ly/Mm05eZ
	mean := Int64Mean(nums)
	dev := Int64StandardDeviation(nums) / math.Sqrt(float64(len(nums)))
	return mean - dev*conf, mean + dev*conf
}

func (f DerivedStats) Count() int {
	return int(f.n)
}

func (f DerivedStats) StandardDeviation() float64 {
	return math.Sqrt(f.variance)
}

func (f DerivedStats) NormalConfidenceInterval() (lower, upper float64) {
	conf := 1.95996 // 95% confidence for the mean, http://bit.ly/Mm05eZ
	dev := f.StandardDeviation() / math.Sqrt(float64(f.n))
	return f.mean - dev*conf, f.mean + dev*conf
}

func (f DerivedStats) Mean() float64 {
	return f.mean
}

func (s Stats) NormalConfidenceInterval() (low, high float64) {
	conf := 1.95996 // 95% confidence for the mean, http://bit.ly/Mm05eZ
	mean := s.Mean()
	dev := s.PopulationStandardDeviation() / math.Sqrt(float64(s.Count()))
	return mean - dev*conf, mean + dev*conf
}
