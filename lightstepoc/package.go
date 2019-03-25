// Package lightstepoc provides an OpenCensus exporter for sending OpenCensus spans back to LightStep.
//
// NOTE: This package is currently experimental. Breaking changes may occur, independent of version.
//
//     func Example() {
//         exporterOptions := []lightstepoc.Option{}
//         exporter, err := lightstepoc.NewExporter(exporterOptions...)}
//         if err != nil {
//             log.Fatal(err)
//         }
//         defer exporter.Close(context.Background())
//
//         trace.RegisterExporter(nil)
//     }
package lightstepoc
