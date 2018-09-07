package lightstep

import (
	gorand "math/rand"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/lightstep/lightstep-tracer-go/lightstep/rand"
)

var _ = Describe("GenSeededGUID", func() {
	Context("With a many calls on genSeededGUID", func() {
		It("should not generate any duplicates", func() {
			randompool = rand.NewPool(time.Now().UnixNano(), 10)
			uniques := 1000000
			ids := map[uint64]bool{}
			for i := 0; i < uniques; i++ {
				id := genSeededGUID()
				ids[id] = true
			}
			Expect(len(ids)).To(Equal(uniques), "should have no duplicates")
		})
	})

	Context("With a many calls on genSeededGUID2", func() {
		It("should not generate any duplicates", func() {
			randompool = rand.NewPool(time.Now().UnixNano(), 10)
			uniques := 1000000
			ids := map[uint64]bool{}
			for i := 0; i < uniques; i++ {
				id1, id2 := genSeededGUID2()
				ids[id1] = true
				ids[id2] = true
			}
			Expect(len(ids)).To(Equal(uniques*2), "should have no duplicates")
		})
	})
})

var _ = Measure("Single Source GenSeededGUID should handle concurrency badly", func(b Benchmarker) {
	goroutines := 100
	calls := 50000
	b.Time("runtime", func() {
		barrier := make(chan struct{})
		group := &sync.WaitGroup{}
		f := func(repeat int) {
			<-barrier // block all goroutines
			for i := 0; i < repeat; i++ {
				_ = singleSourceGenSeededGUID()
			}
			group.Done()
		}

		for g := 0; g < goroutines; g++ {
			group.Add(1)
			go f(calls)
		}
		close(barrier) // lift the barrier
		group.Wait()
	})
}, 10)

var _ = Measure("Random Pool GenSeededGUID should handle concurrency efficiently", func(b Benchmarker) {
	goroutines := 100
	calls := 50000
	randompool = rand.NewPool(time.Now().UnixNano(), 16)
	b.Time("runtime", func() {
		barrier := make(chan struct{})
		group := &sync.WaitGroup{}
		f := func(repeat int) {
			<-barrier // block all goroutines
			for i := 0; i < repeat; i++ {
				_ = genSeededGUID()
			}
			group.Done()
		}

		for g := 0; g < goroutines; g++ {
			group.Add(1)
			go f(calls)
		}
		close(barrier) // lift the barrier
		group.Wait()
	})
}, 10)

var (
	seededGUIDGen     *gorand.Rand
	seededGUIDGenOnce sync.Once
	seededGUIDLock    sync.Mutex
)

// implementation using single random generator
func singleSourceGenSeededGUID() uint64 {
	// Golang does not seed the rng for us. Make sure it happens.
	seededGUIDGenOnce.Do(func() {
		seededGUIDGen = gorand.New(gorand.NewSource(time.Now().UnixNano()))
	})

	// The golang random generators are *not* intrinsically thread-safe.
	seededGUIDLock.Lock()
	defer seededGUIDLock.Unlock()
	return uint64(seededGUIDGen.Int63())
}

func BenchmarkSingleSourceGenSeededGUID(b *testing.B) {
	// run with 100000 goroutines
	b.SetParallelism(100000)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			singleSourceGenSeededGUID()
		}
	})
}

func BenchmarkGenSeededRandomID(b *testing.B) {
	// run with 100000 goroutines
	b.SetParallelism(100000)
	randompool = rand.NewPool(time.Now().UnixNano(), 16) // 16 random generators
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			genSeededGUID()
		}
	})
}
