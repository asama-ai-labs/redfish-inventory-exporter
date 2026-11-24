# Security

Report a vulnerability by opening a private security advisory on this repository (Security tab, "Report a vulnerability"). Do not file a public issue.

The project is pre-1.0. Fixes land on `main` and in the next release; older releases are not patched.

## What the exporter does and does not protect

redfish-inventory-exporter logs in to BMCs and turns what they report into Prometheus metrics. Read these before you deploy it.

- **The scrape endpoint has no authentication.** `GET /redfish?target=<host>` makes the exporter connect to that host, and it sends the credentials the credential provider returns for it. A target with no entry of its own gets the `hosts.default` credentials. Anyone who can reach the listener can therefore make the exporter send those credentials to any host it can reach. The listener defaults to `127.0.0.1` and `--port` (9611). Set `--web.listen-address` to bind another address, including `0.0.0.0:9611` inside a container whose port is published. When a password is in the config file, or a provider other than `config` is selected, an empty `--target-allowlist` makes the process exit. The allowlist is IPs, CIDR ranges or exact host names, and a host name is never resolved. With no password configured the exporter still starts and warns that any caller can name any host. A target must be `host` or `host:port`; a path, query or userinfo is refused with 400.
- **BMC TLS certificates are not verified by default.** Without options the Redfish client skips certificate verification, so a connection to a BMC can be read or altered by anyone on the path. `--tls.ca-file` verifies against the CA that signed your BMC certificates, and `--tls.verify` verifies against the system roots. The exporter warns at startup when it is not verifying. `REDFISH_SCHEME=http` makes it use plaintext HTTP.
- **Load is bounded, not authenticated.** The server sets read, write and idle timeouts, every BMC request has a timeout (`--bmc-request-timeout`), and `--max-concurrent-scrapes` caps scrapes in flight (more get 503). Idle BMC sessions are dropped from memory when their last scrape ends.
- **Proxy settings come from the environment.** The Redfish client honors the standard proxy variables.
- **Credentials sit in a config file unless you use a provider.** `redfish.yml` holds usernames and passwords in plain text. The repository ships only `redfish.yml.example`, with placeholder passwords, and git ignores a local `redfish.yml`. Keep the real file out of version control and out of images; the Docker build context excludes it. The `onepassword` provider reads its service-account token from `OP_SERVICE_ACCOUNT_TOKEN` in the environment, never from the file.
- **Log export is opt-in.** Nothing is exported, and no checkpoint file is written, unless you pass `--enable-otel-logs`. `--otel-log-insecure` sends OTLP over plaintext gRPC and `--otel-log-insecure-skip-verify` skips certificate checks for OTLP. Leave both off unless the collector is on a trusted network.
