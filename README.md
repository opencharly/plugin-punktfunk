# plugin-punktfunk

Streaming-host management for OpenCharly — the `punktfunk:` check verb.

The verb drives a running [punktfunk](https://git.unom.io/unom/punktfunk)
streaming host through its management REST API (bearer token over HTTPS on
47990). It both **probes** a host (health, status, diagnostics, compositors,
gpus, paired clients, pending devices, library, virtual displays, plugins, hooks,
actions) and **manages** one (arm/disarm pairing, approve/deny a pending device,
submit a PIN, rename, set an access preset, unpair, toggle a library scanner,
release virtual displays, invoke a power action, end a game).

It is served **out-of-process** — charly's loader fetches this repo, host-builds
the provider binary, and serves it over go-plugin gRPC.

It is **not** a re-skin of the generic `http:` verb. It exists for what `http:`
structurally cannot do: **token discovery** (the per-host bearer token lives at
`~/.config/punktfunk/mgmt-token` *inside* the venue and is read over the reverse
channel), **endpoint resolution** (one authored step works against a pod's
published port and a VM's forwarded one), and **domain semantics** (methods map
to endpoints and return real verdicts, with `json_path:` to assert one field).
The `events` method subscribes to the SSE lifecycle stream and waits for N
matching events — the only way to assert a *transition*.

TLS verification is **off by default** and the flag is spelled `verify_tls`
rather than `insecure` on purpose: punktfunk serves a self-signed certificate,
and a plain bool cannot distinguish "unset" from "false", so the field is
inverted to make the zero value the correct default.

**Mutating methods are refused in a `check:` step** and must be authored as
`run:` steps, so a probe can never silently unpair a device or reboot a machine.

## What it provides

| Capability | Surface |
|---|---|
| `verb:punktfunk` | the `punktfunk:` check verb — host probing and management over the management REST API |

## How to use it

Compose the plugin candy in a punktfunk-bearing bed (pod or VM) alongside the
`punktfunk` candy from `opencharly/layer-punktfunk`:

```yaml
- '@github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk:<tag>'
```

Then author the verb in a plan:

```yaml
- check: the host's management API reports itself live
  punktfunk: health
  stdout: [{contains: '"status":"ok"'}]
  eventually: 90s
  retry_interval: 5s
  context: [runtime]
```

Install the host itself with the
[`punktfunk`](https://github.com/opencharly/layer-punktfunk) candy.

## Layout

- `candy/plugin-punktfunk/` — the plugin module: `methods.go` (the host +
  client method surface), `provider.go` / `plugin.go`, `transport.go` /
  `token.go` / `parse.go`, `cli.go` (the client half), `schema/punktfunk.cue`
  (the self-contained input schema), `cmd/serve/main.go`.
- `charly.yml` — the root project manifest (`discover: candy` only); the
  `punktfunk-skill` `skill:` entity lives in the candy manifest
  `candy/plugin-punktfunk/charly.yml`.
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.

## Related

- Owning skill: `/charly-check:punktfunk` — the `punktfunk:` check verb (probe +
  manage a streaming host), authored in this candy's `skill:` entity.
- `/charly-punktfunk:punktfunk-host` — the candy that installs the host.
- `/charly-internals:plugin` — the out-of-process plugin model.
