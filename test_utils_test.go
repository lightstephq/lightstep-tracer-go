package lightstep_test

import (
	"fmt"
	"reflect"
	"time"

	. "github.com/lightstep/lightstep-tracer-go"
	ot "github.com/opentracing/opentracing-go"
	"golang.org/x/net/context"

	cpb "github.com/lightstep/lightstep-tracer-go/collectorpb"
	cpbfakes "github.com/lightstep/lightstep-tracer-go/collectorpb/collectorpbfakes"
	"google.golang.org/grpc"

	"github.com/lightstep/lightstep-tracer-go/lightstep_thrift"
	thriftfakes "github.com/lightstep/lightstep-tracer-go/lightstep_thrift/lightstep_thriftfakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func closeTestTracer(tracer ot.Tracer) {
	errChan := make(chan error)
	go func() { errChan <- CloseTracer(tracer) }()
	Eventually(errChan).Should(Receive(BeNil()))
}

func startNSpans(n int, tracer ot.Tracer) {
	for i := 0; i < n; i++ {
		tracer.StartSpan(string(i)).Finish()
	}
}

//////////////////
// GRPC HELPERS //
//////////////////
type haveKeyValuesMatcher []*cpb.KeyValue

func HaveKeyValues(keyValues ...*cpb.KeyValue) types.GomegaMatcher {
	return haveKeyValuesMatcher(keyValues)
}

func (matcher haveKeyValuesMatcher) Match(actual interface{}) (bool, error) {
	var actualKeyValues []*cpb.KeyValue

	switch v := actual.(type) {
	case []*cpb.KeyValue:
		actualKeyValues = v
	case *cpb.Log:
		actualKeyValues = v.GetKeyvalues()
	default:
		return false, fmt.Errorf("HaveKeyValues matcher expects either a []*KeyValue or a *Log")
	}

	expectedKeyValues := []*cpb.KeyValue(matcher)
	if len(expectedKeyValues) != len(actualKeyValues) {
		return false, nil
	}

	for i, _ := range actualKeyValues {
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

func KeyValue(key string, value interface{}, storeAsJson ...bool) *cpb.KeyValue {
	tag := &cpb.KeyValue{Key: key}
	switch typedValue := value.(type) {
	case int:
		tag.Value = &cpb.KeyValue_IntValue{int64(typedValue)}
	case string:
		if len(storeAsJson) > 0 && storeAsJson[0] {
			tag.Value = &cpb.KeyValue_JsonValue{typedValue}
		} else {
			tag.Value = &cpb.KeyValue_StringValue{typedValue}
		}
	case bool:
		tag.Value = &cpb.KeyValue_BoolValue{typedValue}
	case float32:
		tag.Value = &cpb.KeyValue_DoubleValue{float64(typedValue)}
	case float64:
		tag.Value = &cpb.KeyValue_DoubleValue{typedValue}
	}
	return tag
}

func attachGrpcSpanListener(fakeClient *cpbfakes.FakeCollectorServiceClient) func() []*cpb.Span {
	reportChan := make(chan *cpb.ReportRequest)
	fakeClient.ReportStub = func(context context.Context, reportResponse *cpb.ReportRequest, options ...grpc.CallOption) (*cpb.ReportResponse, error) {
		select {
		case reportChan <- reportResponse:
		case <-time.After(1 * time.Second):
		}
		return &cpb.ReportResponse{}, nil
	}

	return func() []*cpb.Span {
		timeout := time.After(5 * time.Second)
		for {
			select {
			case report := <-reportChan:
				if len(report.GetSpans()) > 0 {
					return report.GetSpans()
				}
			case <-timeout:
				Fail("timed out trying to get spans")
			}
		}
	}
}

type dummyConnection struct{}

func (*dummyConnection) Close() error { return nil }

func fakeGrpcConnection(fakeClient *cpbfakes.FakeCollectorServiceClient) ConnectorFactory {
	return func() (interface{}, Connection, error) {
		return fakeClient, new(dummyConnection), nil
	}
}

////////////////////
// THRIFT HELPERS //
////////////////////
type haveThriftKeyValuesMatcher []*lightstep_thrift.KeyValue

func HaveThriftKeyValues(keyValues ...*lightstep_thrift.KeyValue) types.GomegaMatcher {
	return haveThriftKeyValuesMatcher(keyValues)
}

func (matcher haveThriftKeyValuesMatcher) Match(actual interface{}) (bool, error) {
	var actualKeyValues []*lightstep_thrift.KeyValue

	switch v := actual.(type) {
	case []*lightstep_thrift.KeyValue:
		actualKeyValues = v
	case *lightstep_thrift.LogRecord:
		actualKeyValues = v.GetFields()
	default:
		return false, fmt.Errorf("HaveKeyValues matcher expects either a []*KeyValue or a *Log")
	}

	expectedKeyValues := []*lightstep_thrift.KeyValue(matcher)
	if len(expectedKeyValues) != len(actualKeyValues) {
		return false, nil
	}

	for i, _ := range actualKeyValues {
		if !reflect.DeepEqual(actualKeyValues[i], expectedKeyValues[i]) {
			return false, nil
		}
	}

	return true, nil
}

func (matcher haveThriftKeyValuesMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected '%v' to have key values '%v'", actual, matcher)
}

func (matcher haveThriftKeyValuesMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected '%v' to not have key values '%v'", actual, matcher)
}

func ThriftKeyValue(key, value string) *lightstep_thrift.KeyValue {
	return &lightstep_thrift.KeyValue{Key: key, Value: value}
}

func attachThriftSpanListener(fakeClient *thriftfakes.FakeReportingService) func() []*lightstep_thrift.SpanRecord {
	reportChan := make(chan *lightstep_thrift.ReportRequest)
	fakeClient.ReportStub = func(auth *lightstep_thrift.Auth, request *lightstep_thrift.ReportRequest) (*lightstep_thrift.ReportResponse, error) {
		select {
		case reportChan <- request:
		case <-time.After(1 * time.Second):
		}
		return &lightstep_thrift.ReportResponse{}, nil
	}

	return func() []*lightstep_thrift.SpanRecord {
		timeout := time.After(5 * time.Second)
		for {
			select {
			case report := <-reportChan:
				if len(report.GetSpanRecords()) > 0 {
					return report.GetSpanRecords()
				}
			case <-timeout:
				Fail("timed out trying to get spans")
			}
		}
	}
}

func fakeThriftConnectionFactory(fakeClient lightstep_thrift.ReportingService) ConnectorFactory {
	return func() (interface{}, Connection, error) {
		return fakeClient, new(dummyConnection), nil
	}
}
