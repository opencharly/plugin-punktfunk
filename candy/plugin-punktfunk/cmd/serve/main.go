// Command serve is the OUT-OF-PROCESS entrypoint for the punktfunk verb plugin: a
// thin shim serving the importable provider over go-plugin gRPC via sdk.Serve. The
// SAME NewProvider()/NewMeta() compile INTO charly in-process when listed in
// compiled_plugins; this binary is host-built and connected only when they are not —
// placement is invisible above the registry.
package main

import (
	punktfunk "github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk"
	"github.com/opencharly/sdk"
)

func main() { sdk.Serve(punktfunk.NewProvider(), punktfunk.NewMeta()) }
