package audit

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest signing makes tampering *evident*, not impossible.
//
// Nothing stored on a node can defeat root on that node: an attacker who can
// edit a tracked file can also edit the manifest, re-sign it, and replace the
// public key. Signing is still worth having for two reasons, in order of how
// often they matter:
//
//  1. It catches the common cases for free and locally — a corrupted manifest,
//     or a tracked file edited without the manifest being re-recorded.
//  2. The signing key fingerprint is emitted to the OTel pipeline on every
//     check. A fingerprint that changes without a deployment is visible in a
//     backend the attacker on that node does not control. That off-node
//     witness, not the signature itself, is the real security property.
//
// Signing is therefore detection, not prevention. Documentation must say so.

const (
	signingKeyName = "signing.key"
	signingPubName = "signing.pub"
	sigSuffix      = ".sig"
)

// ErrNoSignature reports that a manifest has no detached signature beside it.
// Not an error in itself: manifests recorded before signing existed have none,
// and the agent must keep checking them rather than failing closed on upgrade.
var ErrNoSignature = errors.New("manifest is not signed")

// writeFileAtomic replaces path via a temporary file and a rename. Two
// reasons: the signature and public key are written read-only, so a plain
// write cannot overwrite them on the next provisioning run; and a rename is
// atomic, so a check running concurrently never reads a half-written
// signature and reports a false tamper.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// signingPaths derives the key, public key, and signature paths that belong to
// a manifest. Keys live beside the manifest so one directory holds the whole
// trust state and a single permissions mistake is visible in one place.
func signingPaths(manifestPath string) (key, pub, sig string) {
	dir := filepath.Dir(manifestPath)
	return filepath.Join(dir, signingKeyName), filepath.Join(dir, signingPubName), manifestPath + sigSuffix
}

// LoadOrCreateSigningKey returns the node's manifest signing key, generating
// one on first use. The private key is written 0400; the public key 0444 so
// that an unprivileged agent can verify what a privileged recorder signed.
func LoadOrCreateSigningKey(manifestPath string) (ed25519.PrivateKey, error) {
	keyPath, pubPath, _ := signingPaths(manifestPath)

	if seed, err := os.ReadFile(keyPath); err == nil {
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("signing key %q is %d bytes, want %d", keyPath, len(seed), ed25519.SeedSize)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read signing key %q: %w", keyPath, err)
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	if err := writeFileAtomic(keyPath, priv.Seed(), 0400); err != nil {
		return nil, fmt.Errorf("write signing key %q: %w", keyPath, err)
	}
	if err := writeFileAtomic(pubPath, pub, 0444); err != nil {
		return nil, fmt.Errorf("write public key %q: %w", pubPath, err)
	}
	return priv, nil
}

// SignManifestFile writes a detached signature over the manifest bytes.
func SignManifestFile(manifestPath string) error {
	priv, err := LoadOrCreateSigningKey(manifestPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest %q: %w", manifestPath, err)
	}
	_, _, sigPath := signingPaths(manifestPath)
	if err := writeFileAtomic(sigPath, ed25519.Sign(priv, data), 0444); err != nil {
		return fmt.Errorf("write signature %q: %w", sigPath, err)
	}
	return nil
}

// ManifestTrust is what one integrity check of the manifest itself found.
// Digest and KeyFP travel to the OTel backend on every check, where a change
// without a corresponding deployment is the tamper signal.
type ManifestTrust struct {
	Digest   string // sha256 of the manifest bytes, hex
	KeyFP    string // sha256 of the public key, hex, first 16 chars
	Signed   bool   // a detached signature was present
	Verified bool   // the signature matched the public key
}

// VerifyManifestFile checks a manifest against its detached signature and
// reports what it found. A missing signature yields ErrNoSignature with the
// digest still populated, so an unsigned manifest is reported rather than
// silently trusted or loudly failed.
func VerifyManifestFile(manifestPath string) (ManifestTrust, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return ManifestTrust{}, fmt.Errorf("read manifest %q: %w", manifestPath, err)
	}
	trust := ManifestTrust{Digest: hashHex(data)}

	_, pubPath, sigPath := signingPaths(manifestPath)
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return trust, ErrNoSignature
	}
	trust.Signed = true

	pub, err := os.ReadFile(pubPath)
	if err != nil {
		return trust, fmt.Errorf("read public key %q: %w", pubPath, err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return trust, fmt.Errorf("public key %q is %d bytes, want %d", pubPath, len(pub), ed25519.PublicKeySize)
	}
	trust.KeyFP = hashHex(pub)[:16]
	trust.Verified = ed25519.Verify(ed25519.PublicKey(pub), data, sig)
	return trust, nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
