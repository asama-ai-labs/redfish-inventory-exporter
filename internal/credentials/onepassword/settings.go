// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package onepassword

import (
	"errors"
	"strings"
	"time"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v3"
)

// Settings selects the vault, item title, and field names.
type Settings struct {
	Vault        string
	ItemTemplate string
	DefaultItem  string
	Fields       Fields
	Timeout      time.Duration
}

// Fields holds the 1Password item field label for each of our credentials.
// Username is our Redfish username. Its value is the label on the 1Password item.
type Fields struct {
	Username     string
	Password     string
	PrivPassword string
	Community    string
}

func defaults() *Settings {
	return &Settings{
		ItemTemplate: "{{ip}}",
		Fields: Fields{
			Username:     "username",
			Password:     "password",
			PrivPassword: "priv_password",
			Community:    "community",
		},
	}
}

func (s *Settings) validate() error {
	if s == nil {
		return errors.New("1password vault is invalid")
	}
	if err := credentials.ValidateSegment(s.Vault); err != nil {
		return errors.New("1password vault is invalid")
	}
	if err := credentials.ValidateItemTemplate(s.ItemTemplate); err != nil {
		return err
	}
	if s.DefaultItem != "" {
		if err := credentials.ValidateItemTitle(s.DefaultItem); err != nil || strings.Contains(s.DefaultItem, ":") {
			return errors.New("1password default item is invalid")
		}
	}
	if err := credentials.ValidateSegment(s.Fields.Username); err != nil {
		return errors.New("1password username field is invalid")
	}
	if err := credentials.ValidateSegment(s.Fields.Password); err != nil {
		return errors.New("1password password field is invalid")
	}
	if s.Timeout <= 0 {
		return errors.New("1password timeout must be positive")
	}
	return nil
}

type yamlSettings struct {
	Vault        *string     `yaml:"vault"`
	ItemTemplate *string     `yaml:"item_template"`
	DefaultItem  *string     `yaml:"default_item"`
	Fields       *yamlFields `yaml:"fields"`
}

type yamlFields struct {
	Username     *yamlField `yaml:"username"`
	Password     *yamlField `yaml:"password"`
	PrivPassword *yamlField `yaml:"priv_password"`
	Community    *yamlField `yaml:"community"`
}

// yamlField is one of our credentials. A string is the 1Password label.
// A mapping uses onepassword for that label.
type yamlField struct {
	label *string
}

func (f *yamlField) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var label string
		if err := node.Decode(&label); err != nil {
			return err
		}
		f.label = &label
		return nil
	case yaml.MappingNode:
		if err := credentials.RejectKeys(node, "onepassword"); err != nil {
			return err
		}
		var raw struct {
			OnePassword *string `yaml:"onepassword"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		if raw.OnePassword == nil {
			return errors.New("1password field label is missing")
		}
		f.label = raw.OnePassword
		return nil
	default:
		return errors.New("credentials configuration is invalid")
	}
}

func mergeYAML(settings any, node *yaml.Node) error {
	dst, _ := settings.(*Settings)
	if dst == nil || node == nil {
		return errors.New("credentials configuration is invalid")
	}
	var raw yamlSettings
	if err := node.Decode(&raw); err != nil {
		return errors.New("credentials configuration is invalid")
	}
	if raw.Vault != nil {
		dst.Vault = *raw.Vault
	}
	if raw.ItemTemplate != nil {
		dst.ItemTemplate = *raw.ItemTemplate
	}
	if raw.DefaultItem != nil {
		dst.DefaultItem = strings.TrimSpace(*raw.DefaultItem)
	}
	if raw.Fields == nil {
		return nil
	}
	applyField(&dst.Fields.Username, raw.Fields.Username)
	applyField(&dst.Fields.Password, raw.Fields.Password)
	applyField(&dst.Fields.PrivPassword, raw.Fields.PrivPassword)
	applyField(&dst.Fields.Community, raw.Fields.Community)
	return nil
}

func applyField(dst *string, field *yamlField) {
	if field != nil && field.label != nil {
		*dst = *field.label
	}
}

func checkKeys(node *yaml.Node) error {
	if err := credentials.RejectKeys(node, "vault", "item_template", "default_item", "fields"); err != nil {
		return err
	}
	fields, ok := credentials.Child(node, "fields")
	if !ok {
		return nil
	}
	return credentials.RejectKeys(fields, "username", "password", "priv_password", "community")
}

type flagOverlay struct {
	vault        optional
	itemTemplate optional
	defaultItem  optional
	username     optional
	password     optional
	privPassword optional
	community    optional
}

type optional struct {
	value string
	set   bool
}

func (o *optional) Set(v string) error {
	o.value = v
	o.set = true
	return nil
}

func (o *optional) String() string { return o.value }

func registerFlags(app *kingpin.Application) credentials.Overlay {
	flags := &flagOverlay{}
	app.Flag("credentials.onepassword.vault", "1Password vault that holds one item per BMC IP.").SetValue(&flags.vault)
	app.Flag("credentials.onepassword.item-template", "1Password item title template. Replaces {{ip}}, {{hostname}}, and {{serialnumber}}.").SetValue(&flags.itemTemplate)
	app.Flag("credentials.onepassword.default-item", "1Password item title used when the device item is missing. Example: \"iDRAC admin credentials\".").SetValue(&flags.defaultItem)
	app.Flag("credentials.onepassword.fields.username", "1Password item field label for our username.").SetValue(&flags.username)
	app.Flag("credentials.onepassword.fields.password", "1Password item field label for our password.").SetValue(&flags.password)
	app.Flag("credentials.onepassword.fields.priv-password", "1Password item field label for our priv_password. Unused by Redfish.").SetValue(&flags.privPassword)
	app.Flag("credentials.onepassword.fields.community", "1Password item field label for our community. Unused by Redfish.").SetValue(&flags.community)
	return flags
}

func (f *flagOverlay) Apply(settings any) {
	dst, _ := settings.(*Settings)
	if dst == nil || f == nil {
		return
	}
	if f.vault.set {
		dst.Vault = f.vault.value
	}
	if f.itemTemplate.set {
		dst.ItemTemplate = f.itemTemplate.value
	}
	if f.defaultItem.set {
		dst.DefaultItem = strings.TrimSpace(f.defaultItem.value)
	}
	if f.username.set {
		dst.Fields.Username = f.username.value
	}
	if f.password.set {
		dst.Fields.Password = f.password.value
	}
	if f.privPassword.set {
		dst.Fields.PrivPassword = f.privPassword.value
	}
	if f.community.set {
		dst.Fields.Community = f.community.value
	}
}
