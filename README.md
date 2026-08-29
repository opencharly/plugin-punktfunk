# plugin-punktfunk

The `plugin-punktfunk` candy of the [opencharly/charly](https://github.com/opencharly/charly)
plugin library, as a standalone repo (kind-prefixed naming). The candy manifest lives at
`candy/plugin-punktfunk/`; the charly resolver fetches this repo at the pinned tag,
go-builds the provider on the host, and serves it out-of-process over go-plugin gRPC.

Serves the `punktfunk:` check verb, which drives a running
[punktfunk](https://git.unom.io/unom/punktfunk) streaming host through its management
REST API — probing it (health, status, diagnostics, compositors, clients, library,
displays) and managing it (pairing, approvals, access presets, power actions).

Install the host itself with the
[`punktfunk`](https://github.com/opencharly/layer-punktfunk) candy.
