package lightstep

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type handler1 struct {
	foo int
}

func (h handler1) OnEvent(event Event) {
}

type handler2 struct {
	bar string
}

func (h handler2) OnEvent(event Event) {}

var _ = Describe("OnEvent", func() {
	var h1 handler1
	var h2 handler2
	BeforeEach(func() {
		h1 = handler1{foo: 1}
		h2 = handler2{bar: "bar"}
	})

	It("can be updated with different types of event handlers", func() {
		Expect(func() { OnEvent(h1.OnEvent) }).NotTo(Panic())
		Expect(func() { OnEvent(h2.OnEvent) }).NotTo(Panic())
	})

	It("does not race when setting and calling the handler", func() {
		for i := 0; i < 100; i++ {
			go func() {
				OnEvent(h1.OnEvent)
			}()
			go func() {
				onEvent(newEventStartError(nil))
			}()
			go func() {
				OnEvent(h2.OnEvent)
			}()
			go func() {
				onEvent(newEventStartError(nil))
			}()
		}
		time.Sleep(time.Millisecond)
	})
})
