package lightstep

import (
	"runtime"
	"time"
)

var (
	// create a random pool with size equal to number of CPU Cores
	randompool = NewRandPool(time.Now().UnixNano(), uint64(runtime.NumCPU()))
)

func genSeededGUID() uint64 {
	return uint64(randompool.Pick().Int63())
}

func genSeededGUID2() (uint64, uint64) {
	n1, n2 := randompool.Pick().TwoInt63()
	return uint64(n1), uint64(n2)
}
