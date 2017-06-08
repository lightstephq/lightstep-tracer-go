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
		recorder     *Recorder
		tracer       ot.Tracer
		fakeRecorder Recorder
		timestamp    time.Time
		logs         []*cpb.Log
	)
	Describe("Regarding KeyValue conversion", func() {
		Context("Of raw values", func() {
			const (
				keyString = "whomping willow"
			)

			type fakeString string
			type fakeBool bool
			type fakeInt64 int64
			type fakeFloat64 float64

			BeforeEach(func() {
				fakeRecorder = Recorder{}
			})

			Context("When converting weird values", func() {
				It("Does not panic", func() {
					go func() {
						defer GinkgoRecover()
						fakeRecorder.convertToKeyValue(keyString, nil)
						var nilPointer *int
						fakeRecorder.convertToKeyValue(keyString, nilPointer)
					}()
				})
			})

			Context("When converting a string", func() {
				Specify("GetStringValue() should return the string", func() {
					var value fakeString = "fake news"
					keyValue := fakeRecorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetStringValue()).To(Equal("fake news"))
				})
			})

			Context("When converting a boolean", func() {
				Specify("GetBoolValue() should return the bool", func() {
					var value fakeBool = true
					keyValue := fakeRecorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetBoolValue()).To(Equal(true))
				})
			})

			Context("When converting an integer", func() {
				Specify("GetIntValue() should return the int", func() {
					var value fakeInt64 = 42
					keyValue := fakeRecorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetIntValue()).To(Equal(int64(42)))
				})
			})

			Context("When converting a float", func() {
				Specify("GetDoubleValue() should return the float", func() {
					var value fakeFloat64 = 42.0
					keyValue := fakeRecorder.convertToKeyValue(keyString, value)
					Expect(keyValue.GetDoubleValue()).To(Equal(float64(42.0)))
				})
			})
		})

		Context("Of log translation", func() {
			BeforeEach(func() {
				fakeRecorder = Recorder{
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
				logs = fakeRecorder.translateLogs(otLogs, nil)
			})

			Context("When a timestamp is translated", func() {
				It("is translated with google_protobuf", func() {
					for i := 0; i < 8; i++ {
						Expect(logs[i].Timestamp).To(Equal(&google_protobuf.Timestamp{arbitraryTimestampSecs, 0}))
					}
				})
			})

			Context("When a short string field is translated", func() {
				It("is translated to String Key Value pair", func() {
					for i := 0; i < 8; i++ {
						Expect(logs[i].Keyvalues[0]).To(Equal(
							&cpb.KeyValue{
								Key:   "string",
								Value: &cpb.KeyValue_StringValue{fmt.Sprintf("foo%d", i)},
							},
						))
					}
				})
			})

			Context("When an object is translated", func() {
				It("Is marshled into JSON", func() {
					for i := 0; i < 8; i++ {
						pl, _ := json.Marshal([]interface{}{i, i, true, "suhhh"})
						Expect(logs[i].Keyvalues[1]).To(Equal(
							&cpb.KeyValue{Key: "object", Value: &cpb.KeyValue_JsonValue{string(pl)}},
						))
					}
				})
			})

			Context("When a long string field is translated", func() {
				It("Is translated to a truncated String Key Value pair", func() {
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
	})

	Context("When calling Close() twice", func() {
		Context("With grpc enabled", func() {
			It("Should not fail", func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     true,
				})
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("With thrift enabled", func() {
			It("Should not fail", func() {
				tracer = NewTracer(Options{
					AccessToken: "0987654321",
					UseGRPC:     false,
				})
				err := CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())

				err = CloseTracer(tracer)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("Regarding the span buffer", func() {
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
				It("The buffer should respond appropriately", func() {
					By("Having a full buffer when enough spans have been started")
					spans := makeSpanSlice(defaultMaxSpans)
					for _, span := range spans {
						recorder.RecordSpan(span)
					}
					recorder.lock.Lock()
					Expect(len(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					recorder.lock.Unlock()

					By("Having the buffer at default capacity")
					recorder.lock.Lock()
					Expect(cap(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					recorder.lock.Unlock()

					By("Having the capacity and length remain the same when more spans are started")
					spans = append(spans, makeSpanSlice(defaultMaxSpans)...)
					for _, span := range spans {
						recorder.RecordSpan(span)
					}
					recorder.lock.Lock()
					Expect(cap(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					Expect(len(recorder.buffer.rawSpans)).To(Equal(defaultMaxSpans))
					recorder.lock.Unlock()
				})
			})
		})

		Context("With a user specied buffer size", func() {
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
