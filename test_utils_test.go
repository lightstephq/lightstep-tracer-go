package lightstep_test

import (
	"context"
	"fmt"
	"reflect"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb/collectorpbfakes"
	. "github.com/lightstep/lightstep-tracer-go"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	opentracing "github.com/opentracing/opentracing-go"
)

func closeTestTracer(tracer opentracing.Tracer) {
	complete := make(chan struct{})
	go func() {
		Close(context.Background(), tracer)
		close(complete)
	}()
	Eventually(complete).Should(BeClosed())
}

func startNSpans(n int, tracer opentracing.Tracer) {
	for i := 0; i < n; i++ {
		tracer.StartSpan(string(i)).Finish()
	}
}

type haveKeyValuesMatcher []*collectorpb.KeyValue

func HaveKeyValues(keyValues ...*collectorpb.KeyValue) types.GomegaMatcher {
	return haveKeyValuesMatcher(keyValues)
}

func (matcher haveKeyValuesMatcher) Match(actual interface{}) (bool, error) {
	switch v := actual.(type) {
	case []*collectorpb.KeyValue:
		return matcher.MatchProtos(v)
	case *collectorpb.Log:
		return matcher.MatchProtos(v.GetFields())
	default:
		return false, fmt.Errorf("HaveKeyValues matcher expects either a []*KeyValue or a *Log/*LogRecord")
	}
}

func (matcher haveKeyValuesMatcher) MatchProtos(actualKeyValues []*collectorpb.KeyValue) (bool, error) {
	expectedKeyValues := []*collectorpb.KeyValue(matcher)
	if len(expectedKeyValues) != len(actualKeyValues) {
		return false, nil
	}

	for i := range actualKeyValues {
		if !reflect.DeepEqual(actualKeyValues[i], expectedKeyValues[i]) {
			return false, nil
		}
	}

	return true, nil
}

func (matcher haveKeyValuesMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected '%v' to have key values '%v'", actual, matcher)
}

func (matcher haveKeyValuesMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected '%v' to not have key values '%v'", actual, matcher)
}

func KeyValue(key string, value interface{}, storeAsJson ...bool) *collectorpb.KeyValue {
	tag := &collectorpb.KeyValue{Key: key}
	switch typedValue := value.(type) {
	case int:
		tag.Value = &collectorpb.KeyValue_IntValue{IntValue: int64(typedValue)}
	case string:
		if len(storeAsJson) > 0 && storeAsJson[0] {
			tag.Value = &collectorpb.KeyValue_JsonValue{JsonValue: typedValue}
		} else {
			tag.Value = &collectorpb.KeyValue_StringValue{StringValue: typedValue}
		}
	case bool:
		tag.Value = &collectorpb.KeyValue_BoolValue{BoolValue: typedValue}
	case float32:
		tag.Value = &collectorpb.KeyValue_DoubleValue{DoubleValue: float64(typedValue)}
	case float64:
		tag.Value = &collectorpb.KeyValue_DoubleValue{DoubleValue: typedValue}
	}
	return tag
}

//////////////////
// GRPC HELPERS //
//////////////////

func getReportedGRPCSpans(fakeClient *cpbfakes.FakeCollectorServiceClient) []*collectorpb.Span {
	callCount := fakeClient.ReportCallCount()
	spans := make([]*collectorpb.Span, 0)
	for i := 0; i < callCount; i++ {
		_, report, _ := fakeClient.ReportArgsForCall(i)
		spans = append(spans, report.GetSpans()...)
	}
	return spans
}

type dummyConnection struct{}

func (*dummyConnection) Close() error { return nil }

func fakeGrpcConnection(fakeClient *cpbfakes.FakeCollectorServiceClient) ConnectorFactory {
	return func() (interface{}, Connection, error) {
		return fakeClient, new(dummyConnection), nil
	}
}
