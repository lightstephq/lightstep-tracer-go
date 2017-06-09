package lightstep

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	google_protobuf "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/lightstep/lightstep-tracer-go/basictracer"
	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
)

func makeSpanSlice(length int) []basictracer.RawSpan {
	return make([]basictracer.RawSpan, length)
}

var _ = Describe("Recorder", func() {
	const (
		arbitraryTimestampSecs = 1473442150
	)

	var (
		recorder  *Recorder
		tracer    ot.Tracer
		timestamp time.Time
		logs      []*cpb.Log
	)
	Describe("KeyValues", func() {
		Context("Of raw values", func() {
			const (
				keyString = "whomping willow"
			)

			BeforeEach(func() {
				recorder = &Recorder{}
			})

			Context("When converting weird values", func() {
				It("Does not panic", func(done Done) {
					go func() {
						defer GinkgoRecover()
						recorder.convertToKeyValue(keyString, nil)
						var nilPointer *int
						recorder.convertToKeyValue(keyString, nilPointer)

						close(done)
					}()
				})
			})

			Context("When converting a string", func() {
				Specify("GetStringValue() should return the string", func() {
					var value interface{} = "fake news"
					keyValue := recorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetStringValue()).To(Equal("fake news"))
				})
			})

			Context("When converting a boolean", func() {
				Specify("GetBoolValue() should return the bool", func() {
					var value interface{} = true
					keyValue := recorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetBoolValue()).To(Equal(true))
				})
			})

			Context("When converting an integer", func() {
				Specify("GetIntValue() should return the int", func() {
					var value interface{} = 42
					keyValue := recorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetIntValue()).To(Equal(int64(42)))
				})
			})

			Context("When converting a float", func() {
				Specify("GetDoubleValue() should return the float", func() {
					var value interface{} = 42.0
					keyValue := recorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetDoubleValue()).To(Equal(float64(42.0)))
				})
			})
		})

		Describe("LogRecords", func() {
			BeforeEach(func() {
				recorder = &Recorder{
					maxLogKeyLen:   20,
					maxLogValueLen: 40,
				}
				timestamp = time.Unix(arbitraryTimestampSecs, 0)
				otLogs := make([]ot.LogRecord, 8)
				for i := 0; i < 8; i++ {
					otLogs[i] = ot.LogRecord{
						Timestamp: timestamp,
						Fields: []log.Field{
							log.String("string", fmt.Sprintf("foo%d", i)),
							log.Object("object", []interface{}{i, i, true, "suhhh"}),
							log.String("too_long"+strings.Repeat("-", 50), strings.Repeat("-", 110)),
						},
					}
				}
				logs = recorder.translateLogs(otLogs, nil)
			})

			It("converts timestamp fields to google_protobuf.Timestamp", func() {
				for i := 0; i < 8; i++ {
					Expect(logs[i].Timestamp).To(Equal(&google_protobuf.Timestamp{arbitraryTimestampSecs, 0}))
				}
			})

			It("converts short strings to KeyValue_StringValues", func() {
				for i := 0; i < 8; i++ {
					Expect(logs[i].Keyvalues[0]).To(Equal(
						&cpb.KeyValue{
							Key:   "string",
							Value: &cpb.KeyValue_StringValue{fmt.Sprintf("foo%d", i)},
						},
					))
				}
			})

			It("converts objects to JSON", func() {
				for i := 0; i < 8; i++ {
					pl, _ := json.Marshal([]interface{}{i, i, true, "suhhh"})
					Expect(logs[i].Keyvalues[1]).To(Equal(
						&cpb.KeyValue{Key: "object", Value: &cpb.KeyValue_JsonValue{string(pl)}},
					))
				}
			})

			It("converts long strings to a truncated KeyValue_StringValues", func() {
				for i := 0; i < 8; i++ {
					Expect(logs[i].Keyvalues[2]).To(Equal(
						&cpb.KeyValue{
							Key:   "too_long-----------…",
							Value: &cpb.KeyValue_StringValue{"---------------------------------------…"},
						},
					))
				}
			})
		})
	})

	Context("When calling Close() twice", func() {
		Context("With grpc enabled", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     true,
				})
			})
			It("Should not fail", func() {
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("With thrift enabled", func() {
			BeforeEach(func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     false,
				})
			})
			It("Should not fail", func() {
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("MaxBufferedSpans", func() {
		Context("With the default max span buffer size", func() {
			BeforeEach(func() {
				recorder = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     true,
				}).(basictracer.Tracer).Options().Recorder.(*Recorder)
			})

			Context("Before any spans have been started", func() {
				It("The buffer should be empty and the capacity should be the default", func() {
					recorder.lock.Lock()
					defer recorder.lock.Unlock()
					Expect(cap(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					Expect(len(recorder.buffer.rawSpans)).To(Equal(0))
				})
			})

			Context("After spans have been started", func() {
				Context("When the buffer is at capacity", func() {
					BeforeEach(func() {
						spans := makeSpanSlice(defaultMaxSpans)
						for _, span := range spans {
							recorder.RecordSpan(span)
						}
						recorder.lock.Lock()
					})

					Specify("the buffer should be full", func() {
						Expect(len(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					})

					Specify("the buffer should have default capacity", func() {
						Expect(cap(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					})

					AfterEach(func() {
						recorder.lock.Unlock()
					})
				})

				Context("When the buffer is over capacity", func() {
					BeforeEach(func() {
						spans := makeSpanSlice(10 * defaultMaxSpans)
						for _, span := range spans {
							recorder.RecordSpan(span)
						}
						recorder.lock.Lock()
					})

					It("should drop old items from the buffer to respect the default capacity", func() {
						Expect(len(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					})

					Specify("the capacity of the buffer should remain at the default value", func() {
						Expect(cap(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					})

					AfterEach(func() {
						recorder.lock.Unlock()
					})
				})
			})
		})

		Context("With a user specified buffer size", func() {
			const (
				maxBuffer int = 10
			)

			BeforeEach(func() {
				recorder = NewTracer(Options{
					AccessToken:      "0987654321",
					MaxBufferedSpans: maxBuffer,
					UseGRPC:          true,
				}).(basictracer.Tracer).Options().Recorder.(*Recorder)
			})

			Context("Before any spans have been started", func() {
				It("The buffer should be empty with the specified capacity", func() {
					recorder.lock.Lock()
					defer recorder.lock.Unlock()
					Expect(cap(recorder.buffer.rawSpans)).To(Equal(maxBuffer))
					Expect(len(recorder.buffer.rawSpans)).To(Equal(0))
				})
			})

			Context("After many spans have been started", func() {
				BeforeEach(func() {
					spans := makeSpanSlice(100 * defaultMaxSpans)
					for _, span := range spans {
						recorder.RecordSpan(span)
					}
				})

				It("The buffer be full with the specified capacity", func() {
					recorder.lock.Lock()
					defer recorder.lock.Unlock()
					Expect(cap(recorder.buffer.rawSpans)).To(Equal(maxBuffer))
					Expect(len(recorder.buffer.rawSpans)).To(Equal(maxBuffer))
				})
			})
		})
	})
})
