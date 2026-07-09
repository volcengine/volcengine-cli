package upgrade

import "testing"

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.0.49", "1.0.49"},
		{"v1.0.49", "1.0.49"},
		{"V1.0.49", "1.0.49"},
		{"  v1.0.0  ", "1.0.0"},
	}
	for _, c := range cases {
		if got := NormalizeVersion(c.in); got != c.want {
			t.Errorf("NormalizeVersion(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"1.0.48", "1.0.49", true},
		{"1.0.49", "1.0.49", false},
		{"1.0.50", "1.0.49", false},
		{"v1.0.0", "1.1.0", true},
		{"2.0.0", "1.9.9", false},
		{"1.0.0-beta", "1.0.0", true},
		{"1.0.0", "1.0.0-beta", false},
		{"1.0.0-alpha", "1.0.0-beta", true},
	}
	for _, tt := range tests {
		if got := IsNewer(tt.current, tt.latest); got != tt.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestIsNewer_NonSemver(t *testing.T) {
	if !IsNewer("custom-1", "custom-2") {
		t.Fatal("expected non-semver inequality to report newer")
	}
	if IsNewer("same", "same") {
		t.Fatal("expected same opaque versions to not be newer")
	}
}

func TestSameVersion(t *testing.T) {
	if !SameVersion("v1.0.1", "1.0.1") {
		t.Fatal("expected same")
	}
	if SameVersion("1.0.1", "1.0.2") {
		t.Fatal("expected different")
	}
}

func TestValidateVersion(t *testing.T) {
	if err := ValidateVersion("1.0.49"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateVersion("v1.0.49-rc.1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateVersion("../evil"); err == nil {
		t.Fatal("expected path rejection")
	}
	if err := ValidateVersion("1.0/../../x"); err == nil {
		t.Fatal("expected path rejection")
	}
	if err := ValidateVersion(""); err == nil {
		t.Fatal("expected empty rejection")
	}
}
