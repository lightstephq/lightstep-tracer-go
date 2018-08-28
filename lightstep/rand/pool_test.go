package rand

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Nearest Power of 2 uint64", func() {
	Context("With different uint64 inputs", func() {
		It("should return uint64 that are of 2^x", func() {
			Expect(nextNearestPow2uint64(2)).To(Equal(uint64(2)))
			Expect(nextNearestPow2uint64(3)).To(Equal(uint64(4)))
			Expect(nextNearestPow2uint64(5)).To(Equal(uint64(8)))
			Expect(nextNearestPow2uint64(10)).To(Equal(uint64(16)))
		})
	})
})

var _ = Describe("Fast modulus", func() {
	Context("With different integers in the power of 2", func() {
		It("should return the correct modulus", func() {
			scenarios := []struct {
				number   int
				divisor  int
				expected int
			}{
				{
					number:   13,
					divisor:  4,
					expected: 1,
				},
				{
					number:   13,
					divisor:  8,
					expected: 5,
				},
				{
					number:   16,
					divisor:  8,
					expected: 0,
				},
			}
			for _, sc := range scenarios {
				ans := sc.number & (sc.divisor - 1)
				Expect(ans).To(Equal(sc.expected), "should be equal")
			}
		})
	})
})

var _ = Describe("Pool", func() {
	Context("With any input size", func() {
		It("should return a pool of random number generators of length in the power 2", func() {
			pool := NewPool(1, 3)
			Expect(len(pool.sources)).To(Equal(4))
		})
	})
})
