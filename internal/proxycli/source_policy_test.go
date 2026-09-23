package proxycli

import (
	"net/url"
	"strings"
	"testing"
)

func TestTrustedArtifactPathTraversal(t *testing.T) {
	const prefix = "https://cloudfs.tos-cn-beijing.volces.com/sdk/prod/"
	for _, path := range []string{
		"../outside", "./artifact.zip", "%2e%2e/outside", "%2E./outside",
		"%252e%252e/outside", "%25252e%25252e/outside",
		"child%2f..%2f..%2foutside", "child%252f%252e%252e%252f%252e%252e%252foutside",
		"child\\..\\..\\outside", "child%5c..%5c..%5coutside",
		"child%255c%252e%252e%255coutside",
		"bad%25escape/%252e%252e/%252e%252e/outside",
	} {
		t.Run(path, func(t *testing.T) {
			raw := prefix + path
			if trustedArtifactURL(raw) {
				t.Fatalf("accepted ambiguous/traversing path %q", raw)
			}
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			original, _ := url.Parse(prefix + "artifact.zip")
			if trustedArtifactRedirect(original, u) {
				t.Fatalf("accepted redirect to ambiguous/traversing path %q", raw)
			}
		})
	}
}

func TestTrustedArtifactEncodedOfficialPaths(t *testing.T) {
	for _, raw := range []string{
		"https://registry.npmjs.org/@volcengine/vefaas-cli/-/vefaas-cli-0.3.2.tgz",
		"https://registry.npmjs.org/@volcengine%2fvefaas-cli/-/vefaas-cli-0.3.2.tgz",
		"https://registry.npmjs.org/%40volcengine%2Fvefaas-cli/-/vefaas-cli-0.3.2.tgz",
		"https://cloudfs.tos-cn-beijing.volces.com/sdk/prod/release%20notes.zip",
		"https://cloudfs.tos-cn-beijing.volces.com/sdk/prod/release..zip",
		"https://cloudcache.volccdn.com/ve/proxycli/arkcli/v1.0.33/arkcli",
	} {
		if !trustedArtifactURL(raw) {
			t.Errorf("rejected valid official path %q", raw)
		}
	}
}

func TestTrustedArtifactAuditedPrefixesRemainAvailable(t *testing.T) {
	for _, prefix := range trustedArtifactPrefixes {
		raw := prefix
		if strings.HasSuffix(prefix, "/") {
			raw += "release-1.2.3.zip"
		}
		if !trustedArtifactURL(raw) {
			t.Errorf("audited prefix no longer accepted: %s", raw)
		}
	}
	original, _ := url.Parse("https://github.com/volcengine/milvus-cli/releases/download/v1.0.1/cli.zip")
	valid, _ := url.Parse("https://release-assets.githubusercontent.com/github-production-release-asset/123/cli.zip?sig=a%2Fb%3D")
	if !trustedArtifactRedirect(original, valid) {
		t.Fatal("signed GitHub release redirect rejected")
	}
	ambiguous, _ := url.Parse("https://release-assets.githubusercontent.com/asset/%252e%252e/outside")
	if trustedArtifactRedirect(original, ambiguous) {
		t.Fatal("ambiguous GitHub release redirect accepted")
	}
}

func TestURLParsePreservesDotSegments(t *testing.T) {
	for _, rawPath := range []string{"/sdk/prod/../outside", "/sdk/prod/%2e%2e/outside"} {
		u, err := url.Parse("https://cloudfs.tos-cn-beijing.volces.com" + rawPath)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/sdk/prod/../outside" {
			t.Fatalf("url.Parse changed dot segments: Path=%q RawPath=%q", u.Path, u.RawPath)
		}
	}
}
