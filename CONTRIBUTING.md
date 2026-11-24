# Contributing to redfish-inventory-exporter

Thank you for helping. By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to contribute

- **Report a bug or a wrong metric.** Open an issue. Say which vendor, model and firmware, and attach the relevant Redfish resource when you can. Do not paste credentials, serial numbers, MAC addresses or unsanitized captures.
- **Report a vulnerability.** Do not open an issue. Follow [SECURITY.md](SECURITY.md).
- **Send a change.** Fix a collector, add a metric, improve the docs, or add a test.

For a large change, open an issue first so a maintainer can say whether it fits.

## Set up

You need Go 1.26 or newer, as in `go.mod`.

```bash
git clone <repository-url>
cd redfish-inventory-exporter
go build -o build/redfish-inventory-exporter .
```

## Check your change

```bash
go vet ./...
go test ./...
```

## Where the code goes

- This repository collects what a BMC reports and maps it to metrics. A rule that exists only to work around one vendor's or model's payload quirk does not belong here. In a deployment behind Redpool, fix the payload in Redpool's normalizers instead, so every client sees the same data.
- A change that alters a metric, label or `# HELP` text changes the golden `.prom` files in the [redpool-fixtures](https://github.com/asama-ai-labs/redpool-fixtures) repository. Say so in the pull request, and regenerate those files with the scripts in the redpool repository (`REDPOOL_FIXTURES` points them at a fixtures checkout). Do not edit a golden file by hand.
- Do not invent a value the BMC did not send. Emit an empty label or omit the series.

## Commits and pull requests

- Write the subject in the imperative, under about 72 characters, and say what the change does.
- Keep a pull request to one concern, and explain why in the description.
- Add or update a test with the change.
- New source files start with the SPDX header the neighboring files use.

## Sign your commits (DCO)

Every commit needs a `Signed-off-by` line. It says you wrote the change, or have the right to submit it, under the project's Apache-2.0 license. Add it with:

```bash
git commit -s
```

The line must use your real name and an email you can be reached at:

```text
Signed-off-by: Your Name <you@example.com>
```

The full text you certify is the [Developer Certificate of Origin 1.1](https://developercertificate.org/).

The DCO workflow fails a pull request when any commit lacks the line. To fix a missing one, run `git commit --amend -s`, or `git rebase --signoff` for several commits.

## Review

A maintainer reviews every pull request. A change merges when a maintainer approves it and CI passes.

## License

redfish-inventory-exporter is licensed under the [Apache License 2.0](LICENSE). A contribution is made under the same license.
