package cmd

import (
	"strings"
	"testing"
)

func TestParserApprovalPreservesFlagValues(t *testing.T) {
	const pem = "-----BEGIN CERTIFICATE-----\ncertificate-data\n-----END CERTIFICATE-----"
	for _, tc := range []struct {
		name       string
		args       []string
		parameters map[string]struct{}
		business   map[string]string
		force      bool
	}{
		{name: "long_after", args: []string{"--PublicKey", pem, "--yes"}},
		{name: "short_before", args: []string{"-y", "--PublicKey", pem}},
		{name: "long_between", args: []string{"--Name", "test", "--yes", "--PublicKey", pem}, business: map[string]string{"Name": "test"}},
		{name: "force_and_approval", args: []string{"--force", "--PublicKey", pem, "--yes"}, force: true},
		{name: "legacy_approval", args: []string{"--PublicKey", pem, "---yes"}},
		{name: "business_yes", args: []string{"--yes", "business", "--PublicKey", pem, "-y"}, parameters: map[string]struct{}{"yes": {}}, business: map[string]string{"yes": "business"}},
		{name: "literal_flag_value", args: []string{"--Description", "--yes", "--PublicKey", pem, "-y"}, business: map[string]string{"Description": "--yes"}},
		{name: "empty_value", args: []string{"--Description", "", "--PublicKey", pem, "--yes"}, business: map[string]string{"Description": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewContext()
			positional, err := NewParser(tc.args, tc.parameters).ReadArgs(c)
			if err != nil || len(positional) != 0 {
				t.Fatalf("parse: positional=%v error=%v", positional, err)
			}
			if flag := c.fixedFlags.GetByName("yes"); flag == nil || flag.GetValue() != "true" {
				t.Fatalf("approval not parsed as a system switch: %#v", flag)
			}
			if isForceEnabled(c) != tc.force {
				t.Fatalf("force=%v, want %v", isForceEnabled(c), tc.force)
			}
			if flag := c.dynamicFlags.GetByName("PublicKey"); flag == nil || flag.GetValue() != pem {
				t.Fatal("PEM value was not preserved")
			}
			for name, want := range tc.business {
				if flag := c.dynamicFlags.GetByName(name); flag == nil || flag.GetValue() != want {
					t.Fatalf("business parameter %s = %#v, want %q", name, flag, want)
				}
			}
		})
	}
}

func TestParserApprovalStillRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing_value", args: []string{"--yes", "--PublicKey"}, want: "--PublicKey must set value"},
		{name: "missing_system_value", args: []string{"-y", "--region"}, want: "--region must set value"},
		{name: "duplicate_approval", args: []string{"--yes", "-y"}, want: "duplicated --yes"},
		{name: "equals_not_approval", args: []string{"--yes=false"}, want: "must set value"},
		{name: "legacy_equals_rejected", args: []string{"---yes=false"}, want: "is not supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewParser(tc.args).ReadArgs(NewContext())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
