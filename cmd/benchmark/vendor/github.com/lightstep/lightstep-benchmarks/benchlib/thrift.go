package benchlib

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/lightstep/lightstep-tracer-go/thrift_0_9_2/lib/go/thrift"
)

type ThriftHTTPTransport struct {
	io.ReadCloser
	io.Writer
}

type ThriftStream interface {
	BytesReceived(num int64)
}

type ThriftFactories struct {
	thrift.TProcessorFactory
	thrift.TProtocolFactory
	ThriftStream
}

func (p *ThriftHTTPTransport) Open() error  { return nil }
func (p *ThriftHTTPTransport) IsOpen() bool { return true }
func (p *ThriftHTTPTransport) Flush() error { return nil }

// ServeThriftHTTP is boilerplate for a Thrift connection (binary)
// with additional instrumentation for benchmarking purposes.
func (t *ThriftFactories) ServeThriftHTTP(res http.ResponseWriter, req *http.Request) {
	wrbuffer := bytes.NewBuffer(nil)
	rdbuffer := bytes.NewBuffer(nil)
	rdbytes, err := rdbuffer.ReadFrom(req.Body)
	if err != nil {
		Print("Could not read body: ", err)
	}

	client := &ThriftHTTPTransport{ioutil.NopCloser(rdbuffer), wrbuffer}

	t.ThriftStream.BytesReceived(rdbytes)

	tprocessor := t.GetProcessor(client)
	tprotocol := t.GetProtocol(client)

	ok, err := tprocessor.Process(tprotocol, tprotocol)

	if err != nil {
		Print("RPC Error: ", err)
	} else if !ok {
		Print("RPC request failed")
	}

	res.Header().Set("Content-Type", "application/octet-stream")

	if _, err := io.Copy(res, wrbuffer); err != nil {
		Print("ResponseWriter.Write", err)
	}
}
