package rand_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRand(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Rand Suite")
}
