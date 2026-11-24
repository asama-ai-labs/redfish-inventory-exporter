# redfish-inventory-exporter

Prometheus-style Redfish metrics exporter with optional async Redfish SEL/event log export over OTLP.

Licensed under the Apache License, Version 2.0. Copyright 2026 Asama Inc.

## Scrape URL

`GET /redfish` requires three query parameters:

```text
/redfish?target=<bmc_host_or_ip>&hostname=<host_name>&serialnumber=<serial>
```

Example vmagent relabel (adjust label names to match your service discovery):

```yaml
relabel_configs:
  - source_labels: [__address__]
    target_label: __param_target
  - source_labels: [hostname]
    target_label: __param_hostname
  - source_labels: [serialnumber]
    target_label: __param_serialnumber
  - target_label: __address__
    replacement: redfish-inventory-exporter:9611
  - target_label: __metrics_path__
    replacement: /redfish
```

## Credentials

Redfish usernames and passwords come from one of three sources. Only one is active at a time.

The repository ships no `redfish.yml`, because it would hold passwords. Copy [`redfish.yml.example`](redfish.yml.example) to `redfish.yml` (git ignores it) and replace the placeholder passwords. The exporter reads `redfish.yml` from its working directory unless `--config` says otherwise. In the container the working directory is `/data`, so mount your file at `/data/redfish.yml` and a volume at `/data` to keep the log checkpoint file across restarts.

| Source | Select it with | When the source has no password |
| ------ | -------------- | ------------------------------- |
| Config file | The default. `credentials.provider: config` | Not applicable |
| [1Password](#1password) | `credentials.provider: onepassword` in the config file, or `--credentials.provider=onepassword` | Uses the config file. If 1Password itself fails and nothing was cached, the scrape fails |
| [HashiCorp Vault](#hashicorp-vault) | The flag `--vault.type=hashicorp`. There is no YAML block, and `--credentials.provider=vault` is not valid | Fails the scrape. With `--hashicorp.config-fallback` it uses the config file |

`--vault.type=hashicorp` takes over from the default `config` provider. Combining it with `provider: onepassword` stops the exporter at startup.

### Config file

With `credentials.provider` left at its default, `config`, usernames and passwords come from the config file. The lookup order is `hosts.<target>`, then `hosts.default`, then the top-level `default`. The same lookup is the fallback for the other two sources.

### 1Password

Set the provider to `onepassword` to load the username and password from 1Password instead. Create a service account that can read the vault, and export its token only as `OP_SERVICE_ACCOUNT_TOKEN`. The token is not accepted in the config file or as a command-line flag.

Each scrape target is reduced to a bare IP: the scheme, userinfo, brackets, path, and port are removed. A DNS name is rejected. The item title is `item_template` with `{{ip}}` replaced by that IP, `{{hostname}}` replaced by the `hostname` query parameter, and `{{serialnumber}}` replaced by the `serialnumber` query parameter. A template may use any of those placeholders. The exporter resolves `op://<vault>/<item>/<username field>` and `op://<vault>/<item>/<password field>` in one lookup, bounded by `credentials.timeout`. A successful lookup is reused for 5 minutes. Hostname and serial number cannot contain spaces or `/`.

If that item has no password, the exporter reads `default_item` when it is set (`iDRAC admin credentials`, or any other title). When `default_item` is empty, the same template is tried again with `default` in place of every placeholder (`default`, or `pdu-default` when the template is `pdu-{{ip}}`, or `default-default` when the template is `{{hostname}}-{{serialnumber}}`). If that item has no password either, the scrape uses the YAML host entry, then `hosts.default`, then the top-level `default`. A failed lookup is retried. If 1Password is still unavailable and that item was read successfully before, those secrets are reused. Otherwise the scrape fails and YAML passwords are not used. A template that needs hostname or serial number fails the scrape when that query parameter is empty, and does not use the YAML passwords. `provider: onepassword` cannot be combined with `--vault.type`.

Command-line flags override the config file. The config file overrides the built-in defaults below.

```yaml
credentials:
  provider: config
  timeout: 5s
  onepassword:
    vault: ""
    item_template: "{{ip}}"
    default_item: ""
    fields:
      username:
        onepassword: username
      password:
        onepassword: password
      priv_password:
        onepassword: priv_password
      community:
        onepassword: community
```

The key under `fields` is our credential: `username` and `password` for Redfish, plus `priv_password` and `community` so the same block can be shared with SNMP. `onepassword` is the field label on the 1Password item. A plain string is the same label: `username: login`. Vault names and 1Password field labels cannot contain spaces or `/`. `default_item` may contain spaces. A slash or a colon is still rejected.

```yaml
credentials:
  provider: onepassword
  timeout: 5s
  onepassword:
    vault: bmc
    item_template: "{{ip}}"
    default_item: "iDRAC admin credentials"
    fields:
      username:
        onepassword: username
      password:
        onepassword: password
```

```bash
export OP_SERVICE_ACCOUNT_TOKEN=...
go run . --config redfish.yml --credentials.provider=onepassword --credentials.onepassword.vault=bmc
```

| Flag                                             | Default         | Description                                             |
| ------------------------------------------------ | --------------- | ------------------------------------------------------- |
| `--credentials.provider`                         | `config`        | `config` or `onepassword`. Vault is selected with `--vault.type` |
| `--credentials.timeout`                          | `5s`            | 1Password lookup timeout. Does not apply to Vault       |
| `--credentials.onepassword.vault`                | empty           | Vault name. Required when the provider is `onepassword` |
| `--credentials.onepassword.item-template`        | `{{ip}}`        | Item title. `{{ip}}`, `{{hostname}}`, and `{{serialnumber}}` are replaced |
| `--credentials.onepassword.default-item`         | empty           | Shared item title used when the device item is missing. Spaces are allowed |
| `--credentials.onepassword.fields.username`      | `username`      | 1Password field label for our `username`                |
| `--credentials.onepassword.fields.password`      | `password`      | 1Password field label for our `password`                |
| `--credentials.onepassword.fields.priv-password` | `priv_password` | 1Password field label for our `priv_password`; unused by Redfish |
| `--credentials.onepassword.fields.community`     | `community`     | 1Password field label for our `community`; unused by Redfish |

Create one 1Password item per BMC, titled with that BMC's IP (or whatever `item_template` renders), and store the Redfish username and password on it.

### HashiCorp Vault

Pass `--vault.type=hashicorp` to read each BMC's username and password from a HashiCorp Vault KV secret. Vault is chosen by flag only, and `credentials.provider` stays `config`.

```bash
export VAULT_ADDR=https://vault.example.com:8200
export VAULT_TOKEN=...
go run . --config redfish.yml --vault.type=hashicorp --hashicorp.kv.mount=redfish
```

Store one secret per BMC in the KV mount. The secret's path is the `target` query value exactly as Prometheus sends it, an IP or `host:port`. It is not reduced to a bare IP, and `hostname` and `serialnumber` play no part. The secret needs two string keys, `username` and `password`:

```text
redfish/192.0.2.10    username=readonly  password=...
redfish/default       username=readonly  password=...
```

The exporter tries KV v2 first, then v1, and keeps the version that worked. For each scrape it uses the first login that has a password:

1. The secret named after the target.
2. The secret named `default` in the same mount.

If neither gives a password, the scrape fails with HTTP 502 and logs an error. That covers a missing secret, a key that is absent or not a string, a permission error, an unreachable Vault, and a lookup that takes longer than 5 seconds. The exporter cannot tell these apart, because the Vault client reports them all as one failure. The config file is not consulted, so a Vault outage never sends the config file's passwords to your BMCs.

To use the config file as a last step instead (`hosts.<target>`, then `hosts.default`, then the top-level `default`), start the exporter with `--hashicorp.config-fallback`. It then logs a warning for each fall-back and at startup. Only use it when the config file holds credentials you want sent to every BMC that Vault cannot answer for. Secrets are read on every scrape. There is no cache, and the 5-second limit is fixed (`--credentials.timeout` does not apply).

At startup the exporter checks the token against Vault. A missing token (`vault token is required`) or a failed check stops the exporter before it serves any scrape.

| Flag                                  | Environment variable | Default   | Description                                                           |
| ------------------------------------- | -------------------- | --------- | --------------------------------------------------------------------- |
| `--vault.type`                        |                      | empty     | `hashicorp` (any letter case). Empty means Vault is off               |
| `--hashicorp.address`                 | `VAULT_ADDR`         | `https://127.0.0.1:8200` | Vault address                                          |
| `--hashicorp.token`                   | `VAULT_TOKEN`        | empty     | Vault token. Required, and read only from `VAULT_TOKEN`. The flag is refused at startup, because command-line arguments show up in process listings |
| `--hashicorp.config-fallback`         |                      | false     | Use the config file login when Vault has no usable credentials, instead of failing the scrape |
| `--hashicorp.kv.mount`                | `VAULT_MOUNT_PATH`   | `redfish` | KV mount that holds the secrets                                       |
| `--hashicorp.insecure-skip-tls-verify` | `VAULT_SKIP_VERIFY` | false     | Skip TLS verification of Vault. For testing only                      |

### Adding a credential source

Each credential source is a provider registered by name. `/redfish` asks the selected provider for a username and password and does not branch on the backend. A new integration is a package under `internal/credentials` that registers its YAML block, flags, validation, startup checks, and client. The credentials core runs those startup checks when the provider opens. The core config does not list that provider's fields.

## OTLP log export

Log export is off by default. With `--enable-otel-logs`, on each metrics scrape, after the Prometheus response is flushed, the exporter asynchronously collects new Redfish log entries and sends them to an OpenTelemetry Collector via OTLP.

Sidecar example:

```bash
go run . \
  --config redfish.yml \
  --enable-otel-logs \
  --otel-log-endpoint=http://127.0.0.1:4318/v1/logs \
  --otel-log-checkpoint-file=/var/lib/redfish-inventory-exporter/redfish-log-checkpoints.json
```

### Flags

| Flag                                | Default                        | Description                                                  |
| ----------------------------------- | ------------------------------ | ------------------------------------------------------------ |
| `--enable-otel-logs`                | false                          | Turn on async log export. Off unless set                     |
| `--otel-log-protocol`               | `http`                         | `http` or `grpc`                                             |
| `--otel-log-endpoint`               | protocol default               | HTTP: `http://host:4318/v1/logs`; gRPC: `host:4317`          |
| `--otel-log-timeout`                | `30s`                          | Collect and export timeout                                   |
| `--otel-log-insecure`               | false                          | gRPC plaintext only                                          |
| `--otel-log-insecure-skip-verify`   | false                          | Skip TLS verify for HTTPS OTLP or TLS gRPC                   |
| `--otel-log-checkpoint-file`        | `redfish-log-checkpoints.json` | Checkpoint JSON path                                         |
| `--otel-log-initial-last-n`         | `500`                          | On first run or reset, emit newest N entries per log service |
| `--otel-log-reset-clock-skew`       | `5m`                           | Window for detecting BMC log clears                          |
| `--redfish-max-concurrent-requests` | `2`                            | Max concurrent HTTP requests per BMC session                 |

### Server, TLS and target flags

| Flag                      | Default | Description                                                                                                   |
| ------------------------- | ------- | ------------------------------------------------------------------------------------------------------------- |
| `--web.listen-address`    | `127.0.0.1` and `--port` | `host:port` to listen on. Set `0.0.0.0:9611` to listen on every interface |
| `--target-allowlist`      | empty   | Comma-separated IPs, CIDR ranges or exact host names a scrape may target. Required when a password or an external credential provider is configured |
| `--tls.ca-file`           | empty   | PEM CA for the BMC certificates. Implies verification against this CA only                                    |
| `--tls.verify`            | false   | Verify BMC certificates against the system roots. Without either TLS flag, certificates are not verified      |
| `--bmc-request-timeout`   | `30s`   | Timeout for each HTTP request to a BMC, including the login                                                   |
| `--scrape-timeout`        | `60s`   | Budget for one scrape: cancels the credential lookup and sets the server write timeout. Keep it below the scraper's timeout |
| `--max-concurrent-scrapes`| `32`    | Scrapes in flight across all targets; more get HTTP 503 with `Retry-After`. `0` means no limit                |

Default endpoints when `--otel-log-endpoint` is empty:

- HTTP: `http://localhost:4318/v1/logs`
- gRPC: `localhost:4317`

## Checkpoints

Checkpoints are stored as a single JSON object keyed by:

```text
redfishlog/<target>/<log_service_odata_id>
```

Each value:

```json
{ "created": "...", "entry_id": "...", "last_entry_count": 123 }
```

This matches the shape used by `redfishlogreceiver` in OpenTelemetry Collector Contrib. Checkpoints are updated **only after** a successful OTLP export (at-least-once delivery; duplicates possible on retry).

OTLP resource attributes include `bmc.endpoint`, `bmc.target`, `bmc.credential_key` (same as target), `host.name`, and `host.serial_number`.

## Architecture

1. Metrics scrape runs synchronously; response is flushed before log work starts.
2. Log collection runs in a background goroutine using the same Redfish session.
3. **Metrics always win:** a new scrape cancels any in-flight log export for that BMC target.
4. New log entries are exported via OTLP protobuf (HTTP or gRPC).
5. Checkpoint file is written after export succeeds.

## Operational notes

- **Metrics priority:** log export never blocks the HTTP scrape response. If a scrape arrives while log export is running, log collection is cancelled for that target.
- **Overlapping log jobs:** if a log export is still running when the next scrape arrives, it is cancelled (not queued).
- **Partial failures:** if some members of a collection cannot be read (for example one DIMM answers 404), the members that were read are still exported and the error is logged. `redfish_inventory_exporter_collector_success{target,collector}` is `0` for that subsystem during the scrape and `1` when every request succeeded. Alert on it to catch a subsystem that is silently incomplete.
- **Credential rotation**: When username/password change for a target, the session manager reconnects on the next `Acquire`.
- **Checkpoint file scale**: Each successful export rewrites the entire checkpoint JSON atomically. This is durable and simple; suitable for low hundreds of BMCs. Writes are serialized by a mutex.

## Build and test

```bash
go test ./...
go run . --config redfish.yml.example
```

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md) first; commits need a `Signed-off-by` line (DCO). Report vulnerabilities as described in [SECURITY.md](SECURITY.md), and follow the [Code of Conduct](CODE_OF_CONDUCT.md).
