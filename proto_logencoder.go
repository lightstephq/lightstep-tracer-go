package lightstep

import (
	"encoding/json"
	"fmt"

	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	"github.com/opentracing/opentracing-go/log"
)

const (
	ellipsis = "…"
)

// An implementation of the log.Encoder interface
type grpcLogFieldEncoder struct {
	converter       *protoConverter
	buffer          *reportBuffer
	currentKeyValue *collectorpb.KeyValue
}

func marshalFields(
	converter *protoConverter,
	protoLog *collectorpb.Log,
	fields []log.Field,
	buffer *reportBuffer,
) {
	logFieldEncoder := grpcLogFieldEncoder{
		converter: converter,
		buffer:    buffer,
	}
	protoLog.Fields = make([]*collectorpb.KeyValue, len(fields))
	for i, field := range fields {
		logFieldEncoder.currentKeyValue = &collectorpb.KeyValue{}
		field.Marshal(&logFieldEncoder)
		protoLog.Fields[i] = logFieldEncoder.currentKeyValue
	}
}

func (lfe *grpcLogFieldEncoder) EmitString(key, value string) {
	lfe.emitSafeKey(key)
	lfe.emitSafeString(value)
}
func (lfe *grpcLogFieldEncoder) EmitBool(key string, value bool) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_BoolValue{BoolValue: value}
}
func (lfe *grpcLogFieldEncoder) EmitInt(key string, value int) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_IntValue{IntValue: int64(value)}
}
func (lfe *grpcLogFieldEncoder) EmitInt32(key string, value int32) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_IntValue{IntValue: int64(value)}
}
func (lfe *grpcLogFieldEncoder) EmitInt64(key string, value int64) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_IntValue{IntValue: value}
}

// N.B. We are using a string encoding for 32- and 64-bit unsigned
// integers because it will require a protocol change to treat this
// properly. Revisit this after the OC/OT merger.  LS-1175
//
// We could safely continue using the int64 value to represent uint32
// without breaking the stringified representation, but for
// consistency with uint64, we're encoding all unsigned integers as
// strings.
func (lfe *grpcLogFieldEncoder) EmitUint32(key string, value uint32) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_StringValue{StringValue: fmt.Sprint(value)}
}
func (lfe *grpcLogFieldEncoder) EmitUint64(key string, value uint64) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_StringValue{StringValue: fmt.Sprint(value)}
}
func (lfe *grpcLogFieldEncoder) EmitFloat32(key string, value float32) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_DoubleValue{DoubleValue: float64(value)}
}
func (lfe *grpcLogFieldEncoder) EmitFloat64(key string, value float64) {
	lfe.emitSafeKey(key)
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_DoubleValue{DoubleValue: value}
}
func (lfe *grpcLogFieldEncoder) EmitObject(key string, value interface{}) {
	lfe.emitSafeKey(key)
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		emitEvent(newEventUnsupportedValue(key, value, err))
		lfe.buffer.logEncoderErrorCount++
		lfe.emitSafeString("<json.Marshal error>")
		return
	}
	lfe.emitSafeJSON(string(jsonBytes))
}
func (lfe *grpcLogFieldEncoder) EmitLazyLogger(value log.LazyLogger) {
	// Delegate to `value` to do the late-bound encoding.
	value(lfe)
}

func (lfe *grpcLogFieldEncoder) emitSafeKey(key string) {
	if lfe.converter.maxLogKeyLen > 0 && len(key) > lfe.converter.maxLogKeyLen {
		key = key[:(lfe.converter.maxLogKeyLen-1)] + ellipsis
	}
	lfe.currentKeyValue.Key = key
}
func (lfe *grpcLogFieldEncoder) emitSafeString(str string) {
	if lfe.converter.maxLogValueLen > 0 && len(str) > lfe.converter.maxLogValueLen {
		str = str[:(lfe.converter.maxLogValueLen-1)] + ellipsis
	}
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_StringValue{StringValue: str}
}
func (lfe *grpcLogFieldEncoder) emitSafeJSON(json string) {
	if lfe.converter.maxLogValueLen > 0 && len(json) > lfe.converter.maxLogValueLen {
		str := json[:(lfe.converter.maxLogValueLen-1)] + ellipsis
		lfe.currentKeyValue.Value = &collectorpb.KeyValue_StringValue{StringValue: str}
		return
	}
	lfe.currentKeyValue.Value = &collectorpb.KeyValue_JsonValue{JsonValue: json}
}
