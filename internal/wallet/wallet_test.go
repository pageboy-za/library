package wallet

import (
	"archive/zip"
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"github.com/smallstep/pkcs7"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, *rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	dir := t.TempDir()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	caKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable WWDR test intermediate"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, e := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	ca, e = x509.ParseCertificate(caDER)
	if e != nil {
		t.Fatal(e)
	}
	pass := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Disposable pass test certificate", OrganizationalUnit: []string{"TESTTEAM"}, ExtraNames: []pkix.AttributeTypeAndValue{{Type: asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 1}, Value: "pass.example.library"}}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	passDER, e := x509.CreateCertificate(rand.Reader, pass, ca, &key.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	write := func(name string, raw []byte) {
		if e := os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("pass.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: passDER}))
	write("wwdr.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	write("key.pem", keyPEM)
	account, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "fixture@example.iam.gserviceaccount.com", "private_key": string(keyPEM), "private_key_id": "fixture-key"})
	write("account.json", account)
	cfg := Config{Apple: &AppleConfig{"pass.example.library", "TESTTEAM", "Fixture Library", filepath.Join(dir, "pass.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "wwdr.pem")}, Google: &GoogleConfig{"123456", "123456.library", filepath.Join(dir, "account.json"), "Fixture Library", []string{"https://library.local"}}}
	raw, _ := json.Marshal(cfg)
	write("config.json", raw)
	return filepath.Join(dir, "config.json"), key, ca
}
func TestNativeAppleAndGoogleSignatures(t *testing.T) {
	path, key, ca := fixture(t)
	s, e := NewFromFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if !s.Status().Apple || !s.Status().Google {
		t.Fatal(s.Status())
	}
	card := Card{"mbr_abcdefghijklmnopqrstuvwxyz123456", "Fixture Member"}
	data, e := s.Apple(card)
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		files[f.Name], e = io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	var manifest map[string]string
	if e = json.Unmarshal(files["manifest.json"], &manifest); e != nil {
		t.Fatal(e)
	}
	if len(manifest) != 4 {
		t.Fatal(manifest)
	}
	for n, want := range manifest {
		h := sha1.Sum(files[n])
		if hex.EncodeToString(h[:]) != want {
			t.Fatal("incorrect manifest", n)
		}
	}
	sig, e := pkcs7.Parse(files["signature"])
	if e != nil {
		t.Fatal(e)
	}
	if len(sig.Content) != 0 {
		t.Fatal("signature must be detached")
	}
	sig.Content = files["manifest.json"]
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if e = sig.VerifyWithChain(roots); e != nil {
		t.Fatal(e)
	}
	sig.Content = append([]byte("tamper"), sig.Content...)
	if sig.Verify() == nil {
		t.Fatal("tampered manifest signature accepted")
	}
	var pass map[string]any
	if e = json.Unmarshal(files["pass.json"], &pass); e != nil {
		t.Fatal(e)
	}
	if pass["serialNumber"] != card.Code || pass["webServiceURL"] != nil || pass["authenticationToken"] != nil {
		t.Fatal("unexpected pass identity or update token", pass)
	}
	link, e := s.Google(card)
	if e != nil {
		t.Fatal(e)
	}
	parts := strings.Split(strings.TrimPrefix(link, "https://pay.google.com/gp/v/save/"), ".")
	if len(parts) != 3 {
		t.Fatal("malformed JWT")
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if e = rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, h[:], signature); e != nil {
		t.Fatal(e)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if e = json.Unmarshal(raw, &claims); e != nil {
		t.Fatal(e)
	}
	if claims["aud"] != "google" || claims["typ"] != "savetowallet" {
		t.Fatal(claims)
	}
	objects := claims["payload"].(map[string]any)["genericObjects"].([]any)
	object := objects[0].(map[string]any)
	if object["id"] != "123456."+card.Code || object["barcode"].(map[string]any)["value"] != card.Code {
		t.Fatal(object)
	}
	for _, secret := range []string{"private_key", "client_email", "receipt", "loans", "email", "phone"} {
		if bytes.Contains(files["pass.json"], []byte(secret)) || bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("unexpected personal/credential field %s", secret)
		}
	}
}
func TestConfigurationAndProviderFailures(t *testing.T) {
	s, e := NewFromFile("")
	if e != nil || s.Status() != (Status{}) {
		t.Fatal(s, e)
	}
	if _, e = s.Apple(Card{}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = s.Google(Card{}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	path, _, _ := fixture(t)
	s, e = NewFromFile(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range []Card{{"https://private.invalid/token", "Fixture"}, {"mbr_abcdefghijklmnopqrstuvwxyz123456", ""}, {"mbr_abcdefghijklmnopqrstuvwxyz123456", "bad\nname"}} {
		if _, e = s.Apple(c); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
		if _, e = s.Google(c); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = NewFromFile(path); e == nil {
		t.Fatal("public config accepted")
	}
	os.Chmod(path, 0600)
	raw, _ := os.ReadFile(path)
	var cfg Config
	json.Unmarshal(raw, &cfg)
	cfg.Apple.TeamID = "MISMATCH"
	raw, _ = json.Marshal(cfg)
	os.WriteFile(path, raw, 0600)
	if _, e = NewFromFile(path); e == nil {
		t.Fatal("mismatched Apple team accepted")
	}
	cfg.Apple = nil
	cfg.Google.Origins = []string{"http://library.local"}
	raw, _ = json.Marshal(cfg)
	os.WriteFile(path, raw, 0600)
	if _, e = NewFromFile(path); e == nil {
		t.Fatal("HTTP Google origin accepted")
	}
	cfg.Google.Origins = []string{"https://library.local"}
	os.Chmod(cfg.Google.ServiceAccount, 0644)
	raw, _ = json.Marshal(cfg)
	os.WriteFile(path, raw, 0600)
	if _, e = NewFromFile(path); e == nil {
		t.Fatal("public signing key accepted")
	}
}

func TestAppleRejectsExpiredAndMismatchedSigningMaterial(t *testing.T) {
	path, _, _ := fixture(t)
	s, err := NewFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	card := Card{"mbr_abcdefghijklmnopqrstuvwxyz123456", "Fixture Member"}
	s.apple.certificate.NotAfter = time.Now().Add(-time.Minute)
	if _, err = s.Apple(card); err == nil {
		t.Fatal("expired leaf signed a pass")
	}
	s, err = NewFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.apple.chain[0].NotAfter = time.Now().Add(-time.Minute)
	if _, err = s.Apple(card); err == nil {
		t.Fatal("expired intermediate signed a pass")
	}
	raw, _ := os.ReadFile(path)
	var cfg Config
	json.Unmarshal(raw, &cfg)
	cfg.Apple.WWDRCertificates = cfg.Apple.Certificate
	raw, _ = json.Marshal(cfg)
	os.WriteFile(path, raw, 0600)
	if _, err = NewFromFile(path); err == nil {
		t.Fatal("non-CA intermediate accepted")
	}
	cfg.Apple.WWDRCertificates = filepath.Join(filepath.Dir(path), "wwdr.pem")
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(cfg.Apple.PrivateKey, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(other)}), 0600)
	raw, _ = json.Marshal(cfg)
	os.WriteFile(path, raw, 0600)
	if _, err = NewFromFile(path); err == nil {
		t.Fatal("mismatched signer private key accepted")
	}
}

func TestCredentialLocationsRejectServedAndBackedUpPaths(t *testing.T) {
	path, _, _ := fixture(t)
	s, err := NewFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	safe := t.TempDir()
	if err = s.ValidateLocations(filepath.Join(safe, "web"), filepath.Join(safe, "data-not-created")); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateLocations(filepath.Dir(path)); err == nil {
		t.Fatal("configuration in served directory accepted")
	}
	// A parent symlink can hide credential material physically inside the web
	// root even when the configured path has a completely different prefix.
	parent := t.TempDir()
	alias := filepath.Join(parent, "external")
	if err = os.Symlink(filepath.Dir(path), alias); err != nil {
		t.Fatal(err)
	}
	linked, err := NewFromFile(filepath.Join(alias, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = linked.ValidateLocations(filepath.Dir(path)); err == nil {
		t.Fatal("symlink parent bypassed forbidden location")
	}
	// Every credential, not only the config, is checked.
	s.files = []string{s.apple.config.Certificate, s.apple.config.PrivateKey, s.apple.config.WWDRCertificates, s.google.config.ServiceAccount}
	for _, file := range s.files {
		single := &Service{files: []string{file}}
		if err = single.ValidateLocations(filepath.Dir(file)); err == nil {
			t.Fatal("credential inside protected root accepted")
		}
	}
	disabled, _ := NewFromFile("")
	if err = disabled.ValidateLocations(filepath.Join(safe, "missing")); err != nil {
		t.Fatal(err)
	}
}
