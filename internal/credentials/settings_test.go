// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials/onepassword"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v3"

	_ "github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials/config"
	_ "github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials/hashicorp"
)

func TestResolveDefaults(t *testing.T) {
	sel, err := credentials.Resolve(parseCredentials(t, "hosts:\n  default:\n    username: admin\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Name != credentials.NameConfig || sel.Timeout != 5*time.Second {
		t.Fatalf("selection %+v", sel)
	}
	settings := sel.Settings(credentials.NameOnePassword).(*onepassword.Settings)
	if settings.Vault != "" || settings.ItemTemplate != "{{ip}}" {
		t.Fatalf("onepassword defaults: %+v", settings)
	}
	if settings.Fields.Username != "username" || settings.Fields.Password != "password" ||
		settings.Fields.PrivPassword != "priv_password" || settings.Fields.Community != "community" {
		t.Fatalf("fields %+v", settings.Fields)
	}
}

func TestPrecedence(t *testing.T) {
	node := parseCredentials(t, `
credentials:
  provider: onepassword
  timeout: 9s
  onepassword:
    vault: from-yaml
    item_template: "yaml-{{ip}}"
    fields:
      username: yaml-user
      password: yaml-pass
      priv_password: yaml-priv
      community: yaml-community
`)
	yamlOnly, err := credentials.Resolve(node, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := yamlOnly.Settings(credentials.NameOnePassword).(*onepassword.Settings)
	if yamlOnly.Name != credentials.NameOnePassword || yamlOnly.Timeout != 9*time.Second ||
		settings.Vault != "from-yaml" || settings.ItemTemplate != "yaml-{{ip}}" ||
		settings.Fields.Username != "yaml-user" || settings.Fields.Password != "yaml-pass" ||
		settings.Fields.PrivPassword != "yaml-priv" || settings.Fields.Community != "yaml-community" {
		t.Fatalf("yaml overlay: %+v %+v", yamlOnly, settings)
	}

	flags := parseFlags(t,
		"--credentials.provider=config",
		"--credentials.timeout=2s",
		"--credentials.onepassword.vault=from-cli",
		"--credentials.onepassword.item-template=cli-{{ip}}",
		"--credentials.onepassword.fields.username=cli-user",
		"--credentials.onepassword.fields.password=cli-pass",
		"--credentials.onepassword.fields.priv-password=cli-priv",
		"--credentials.onepassword.fields.community=cli-community",
	)
	sel, err := credentials.Resolve(node, flags)
	if err != nil {
		t.Fatal(err)
	}
	settings = sel.Settings(credentials.NameOnePassword).(*onepassword.Settings)
	if sel.Name != credentials.NameConfig || sel.Timeout != 2*time.Second ||
		settings.Vault != "from-cli" || settings.ItemTemplate != "cli-{{ip}}" ||
		settings.Fields.Username != "cli-user" || settings.Fields.Password != "cli-pass" ||
		settings.Fields.PrivPassword != "cli-priv" || settings.Fields.Community != "cli-community" {
		t.Fatalf("cli overlay: name %s timeout %s settings %+v", sel.Name, sel.Timeout, settings)
	}
}

func TestOnePasswordFieldLabel(t *testing.T) {
	node := parseCredentials(t, `
credentials:
  provider: onepassword
  onepassword:
    vault: bmc
    fields:
      username:
        onepassword: login
      password: secret
`)
	sel, err := credentials.Resolve(node, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := sel.Settings(credentials.NameOnePassword).(*onepassword.Settings)
	if settings.Fields.Username != "login" || settings.Fields.Password != "secret" {
		t.Fatalf("fields %+v", settings.Fields)
	}
	node = parseCredentials(t, "credentials:\n  onepassword:\n    fields:\n      username:\n        label: login\n")
	if _, err := credentials.Resolve(node, nil); err == nil {
		t.Fatal("expected unknown field key to fail")
	}
}

func TestOnePasswordValidation(t *testing.T) {
	node := parseCredentials(t, "credentials:\n  provider: onepassword\n  onepassword:\n    vault: bmc\n")
	if _, err := credentials.Resolve(node, nil); err != nil {
		t.Fatal(err)
	}
	node = parseCredentials(t, "credentials:\n  provider: onepassword\n  onepassword: {}\n")
	if _, err := credentials.Resolve(node, nil); err == nil {
		t.Fatal("expected missing vault to fail")
	}
	node = parseCredentials(t, "credentials:\n  provider: onepassword\n  onepassword:\n    vault: bmc\n    item_template: \"{{host}}\"\n")
	if _, err := credentials.Resolve(node, nil); err == nil {
		t.Fatal("expected unsupported placeholder to fail")
	}
	node = parseCredentials(t, "credentials:\n  provider: nope\n")
	if _, err := credentials.Resolve(node, nil); err == nil {
		t.Fatal("expected invalid provider to fail")
	}
	flags := parseFlags(t, "--credentials.provider=config")
	if _, err := credentials.Resolve(node, flags); err != nil {
		t.Fatal(err)
	}
}

func TestRejectTokenAndUnknownFields(t *testing.T) {
	const token = "super-secret-token-value"
	cases := []string{
		"credentials:\n  provider: onepassword\n  token: " + token + "\n",
		"credentials:\n  onepassword:\n    service_account_token: " + token + "\n",
		"credentials:\n  OP_SERVICE_ACCOUNT_TOKEN: " + token + "\n",
	}
	for _, raw := range cases {
		_, err := document(t, raw)
		if err == nil {
			t.Fatalf("expected token rejection for %q", raw)
		}
		if strings.Contains(err.Error(), token) {
			t.Fatalf("token leaked in error: %v", err)
		}
	}
	_, err := document(t, "credentials:\n  onepassword:\n    extra: kept-secret\n")
	if err == nil || strings.Contains(err.Error(), "kept-secret") {
		t.Fatalf("unknown field: %v", err)
	}
}

func TestCLIRejectsTokenAndBadProvider(t *testing.T) {
	app := kingpin.New("redfish-inventory-exporter", "")
	credentials.AddFlags(app)
	const token = "cli-secret-token"
	_, err := app.Parse([]string{"--credentials.onepassword.token=" + token})
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("token flag: %v", err)
	}
	app = kingpin.New("redfish-inventory-exporter", "")
	credentials.AddFlags(app)
	_, err = app.Parse([]string{"--credentials.provider=vault"})
	if err == nil || strings.Contains(err.Error(), "vault") {
		t.Fatalf("provider flag: %v", err)
	}
}

func TestHashicorpConflictAndActivation(t *testing.T) {
	ext := credentials.External{Modes: map[string]bool{credentials.NameHashicorp: true}}
	node := parseCredentials(t, "credentials:\n  provider: onepassword\n  onepassword:\n    vault: bmc\n")
	sel, err := credentials.Resolve(node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sel.Active(ext); err == nil {
		t.Fatal("expected conflict")
	}
	if _, err := sel.Active(credentials.External{}); err != nil {
		t.Fatal(err)
	}
	sel, err = credentials.Resolve(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	name, err := sel.Active(ext)
	if err != nil || name != credentials.NameHashicorp {
		t.Fatalf("name %s err %v", name, err)
	}
}

func TestThrowawayProviderNeedsNoCoreEdits(t *testing.T) {
	const name = "example"
	credentials.Register(credentials.Spec{
		Name:       name,
		Selectable: true,
		Defaults: func() any {
			return &exampleSettings{Bucket: "default-bucket"}
		},
		MergeYAML: func(settings any, node *yaml.Node) error {
			if err := credentials.RejectKeys(node, "bucket"); err != nil {
				return err
			}
			return node.Decode(settings)
		},
		CheckKeys: func(node *yaml.Node) error {
			return credentials.RejectKeys(node, "bucket")
		},
		RegisterFlags: func(app *kingpin.Application) credentials.Overlay {
			flags := &exampleFlags{}
			app.Flag("credentials.example.bucket", "Example bucket.").SetValue(&flags.bucket)
			return flags
		},
		Validate: func(settings any, _ time.Duration) error {
			if settings.(*exampleSettings).Bucket == "" {
				return errExample
			}
			return nil
		},
		Open: func(context.Context, any, credentials.Deps) (credentials.Provider, error) {
			return exampleProvider{}, nil
		},
	})
	node := parseCredentials(t, "credentials:\n  provider: example\n  example:\n    bucket: from-yaml\n")
	sel, err := credentials.Resolve(node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Name != name || sel.Settings(name).(*exampleSettings).Bucket != "from-yaml" {
		t.Fatalf("yaml settings %+v", sel.Settings(name))
	}
	flags := parseFlags(t, "--credentials.provider=example", "--credentials.example.bucket=from-cli")
	sel, err = credentials.Resolve(node, flags)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Settings(name).(*exampleSettings).Bucket != "from-cli" {
		t.Fatalf("cli settings %+v", sel.Settings(name))
	}
	provider, err := credentials.Open(context.Background(), name, sel, credentials.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
}

func TestTargetNormalization(t *testing.T) {
	got, err := credentials.NormalizeTargetIP("https://user:secret@10.1.2.3:8443/redfish")
	if err != nil || got != "10.1.2.3" {
		t.Fatalf("got %s err %v", got, err)
	}
	if _, err := credentials.NormalizeTargetIP("bmc.example"); !errors.Is(err, credentials.ErrInvalidTarget) {
		t.Fatal(err)
	}
	item, err := credentials.RenderItemName("bmc-{{ip}}", credentials.Name{IP: "2001:db8::2"})
	if err != nil || item != "bmc-2001:db8::2" {
		t.Fatalf("item %q err %v", item, err)
	}
	item, err = credentials.RenderItemName("{{hostname}}-{{serialnumber}}", credentials.Name{
		Hostname: "node-a", SerialNumber: "SN1",
	})
	if err != nil || item != "node-a-SN1" {
		t.Fatalf("item %q err %v", item, err)
	}
	item, err = credentials.RenderDefaultItemName("pdu-{{ip}}")
	if err != nil || item != "pdu-default" {
		t.Fatalf("default item %q err %v", item, err)
	}
}

func TestOpenRunsStartup(t *testing.T) {
	const name = "checked"
	opened := false
	credentials.Register(credentials.Spec{
		Name:       name,
		Selectable: true,
		Startup: func(any, credentials.Deps) error {
			return errExample
		},
		Open: func(context.Context, any, credentials.Deps) (credentials.Provider, error) {
			opened = true
			return exampleProvider{}, nil
		},
	})
	sel, err := credentials.Resolve(parseCredentials(t, "credentials:\n  provider: checked\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = credentials.Open(context.Background(), name, sel, credentials.Deps{})
	if !errors.Is(err, errExample) {
		t.Fatalf("err %v", err)
	}
	if opened {
		t.Fatal("open ran after startup failure")
	}
}

func TestOnePasswordStartupRequiresToken(t *testing.T) {
	node := parseCredentials(t, "credentials:\n  provider: onepassword\n  onepassword:\n    vault: bmc\n")
	for _, token := range []string{"", "   "} {
		t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", token)
		sel, err := credentials.Resolve(node, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = credentials.Open(context.Background(), sel.Name, sel, credentials.Deps{})
		if err == nil || !strings.Contains(err.Error(), "OP_SERVICE_ACCOUNT_TOKEN") {
			t.Fatalf("token %q err %v", token, err)
		}
	}
}

func TestConfigStartupRequiresLookup(t *testing.T) {
	sel, err := credentials.Resolve(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = credentials.Open(context.Background(), credentials.NameConfig, sel, credentials.Deps{})
	if err == nil || !strings.Contains(err.Error(), "config credential lookup is required") {
		t.Fatalf("err %v", err)
	}
}

func TestConfigProviderUsesLookup(t *testing.T) {
	sel, err := credentials.Resolve(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := credentials.Open(context.Background(), credentials.NameConfig, sel, credentials.Deps{
		Lookup: func(target string) credentials.Login {
			return credentials.Login{Username: "yaml-user", Password: target}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
	if err != nil || got.Username != "yaml-user" || got.Password != "10.0.0.8" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestPublicFailureRedacts(t *testing.T) {
	status, msg := credentials.PublicFailure(errExample)
	if status != 500 || msg != "failed to resolve credentials" {
		t.Fatalf("status %d message %q", status, msg)
	}
	status, msg = credentials.PublicFailure(credentials.ErrInvalidTarget)
	if status != 400 || msg != "target must be an IP address" {
		t.Fatalf("status %d message %q", status, msg)
	}
}

type exampleSettings struct {
	Bucket string `yaml:"bucket"`
}

type exampleFlags struct {
	bucket flagValue
}

func (f *exampleFlags) Apply(settings any) {
	if f.bucket.set {
		settings.(*exampleSettings).Bucket = f.bucket.value
	}
}

type flagValue struct {
	value string
	set   bool
}

func (f *flagValue) Set(v string) error {
	f.value = v
	f.set = true
	return nil
}

func (f *flagValue) String() string { return f.value }

type exampleProvider struct{}

func (exampleProvider) Credentials(context.Context, credentials.Subject) (credentials.Login, error) {
	return credentials.Login{Username: "example"}, nil
}

var errExample = errString("example")

type errString string

func (e errString) Error() string { return string(e) }

func parseCredentials(t *testing.T, raw string) *yaml.Node {
	t.Helper()
	node, err := document(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func document(t *testing.T, raw string) (*yaml.Node, error) {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	creds, ok := credentials.Child(root, "credentials")
	if !ok {
		return nil, nil
	}
	if err := credentials.ValidateDocument(creds); err != nil {
		return nil, err
	}
	return creds, nil
}

func parseFlags(t *testing.T, args ...string) *credentials.Flags {
	t.Helper()
	app := kingpin.New("redfish-inventory-exporter", "")
	flags := credentials.AddFlags(app)
	if _, err := app.Parse(args); err != nil {
		t.Fatal(err)
	}
	return flags
}
