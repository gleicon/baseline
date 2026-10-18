package audit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeSignedManifest(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"generated_at":1}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := SignManifestFile(path); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return dir, path
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	_, path := writeSignedManifest(t)

	trust, err := VerifyManifestFile(path)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !trust.Signed || !trust.Verified {
		t.Fatalf("a freshly signed manifest must verify: %+v", trust)
	}
	if trust.Digest == "" || len(trust.KeyFP) != 16 {
		t.Errorf("digest and key fingerprint must travel off-node: %+v", trust)
	}
}

// The case the whole feature exists for: a tracked file is tampered with and
// the manifest edited to match, but the attacker does not re-sign.
func TestVerifyDetectsEditedManifest(t *testing.T) {
	_, path := writeSignedManifest(t)

	if err := os.WriteFile(path, []byte(`{"schema_version":1,"generated_at":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	trust, err := VerifyManifestFile(path)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !trust.Signed {
		t.Fatal("signature file is still present; Signed must stay true")
	}
	if trust.Verified {
		t.Fatal("an edited manifest must not verify against the old signature")
	}
}

// A key swap is what a thorough attacker does next. It verifies locally, by
// design — the fingerprint change is what the backend sees.
func TestKeyFingerprintChangesOnKeySwap(t *testing.T) {
	dir, path := writeSignedManifest(t)
	first, err := VerifyManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{signingKeyName, signingPubName} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	if err := SignManifestFile(path); err != nil {
		t.Fatalf("re-sign with a new key: %v", err)
	}
	second, err := VerifyManifestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Verified {
		t.Fatal("re-signed manifest should verify locally — that is the point of the off-node witness")
	}
	if first.KeyFP == second.KeyFP {
		t.Fatal("a new signing key must produce a different fingerprint, or tampering is invisible off-node")
	}
}

// Manifests recorded before signing existed have no signature. Failing closed
// on upgrade would silence a whole fleet, so this must stay a soft signal.
func TestUnsignedManifestIsReportedNotFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	trust, err := VerifyManifestFile(path)
	if !errors.Is(err, ErrNoSignature) {
		t.Fatalf("want ErrNoSignature, got %v", err)
	}
	if trust.Signed {
		t.Error("Signed must be false when no signature exists")
	}
	if trust.Digest == "" {
		t.Error("the digest must still be reported so the backend can watch it")
	}
}
