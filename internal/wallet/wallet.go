// Package wallet signs optional member passes locally, without network calls.
package wallet

import (
	"archive/zip"
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" // Apple pass manifests require SHA-1, not a password/security hash.
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/smallstep/pkcs7"
)

var ErrUnavailable = errors.New("Wallet provider is not configured")
var ErrInvalid = errors.New("Invalid wallet card")

type Card struct{ Code, Name string }
type Status struct {
	Apple  bool `json:"apple"`
	Google bool `json:"google"`
}
type AppleConfig struct {
	PassTypeID       string `json:"pass_type_id"`
	TeamID           string `json:"team_id"`
	Organization     string `json:"organization"`
	Certificate      string `json:"certificate"`
	PrivateKey       string `json:"private_key"`
	WWDRCertificates string `json:"wwdr_certificates"`
}
type GoogleConfig struct {
	IssuerID       string   `json:"issuer_id"`
	ClassID        string   `json:"class_id"`
	ServiceAccount string   `json:"service_account"`
	Organization   string   `json:"organization"`
	Origins        []string `json:"origins"`
}
type Config struct {
	Apple  *AppleConfig  `json:"apple,omitempty"`
	Google *GoogleConfig `json:"google,omitempty"`
}
type appleSigner struct {
	config      AppleConfig
	certificate *x509.Certificate
	key         *rsa.PrivateKey
	chain       []*x509.Certificate
}
type googleSigner struct {
	config       GoogleConfig
	email, keyID string
	key          *rsa.PrivateKey
}
type Service struct {
	apple  *appleSigner
	google *googleSigner
	files  []string
}

func (s *Service) Status() Status {
	return Status{s != nil && s.apple != nil, s != nil && s.google != nil}
}

var codePattern = regexp.MustCompile(`^mbr_[A-Za-z0-9_-]{32}$`)
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validCard(c Card) bool {
	return codePattern.MatchString(c.Code) && strings.TrimSpace(c.Name) != "" && utf8.ValidString(c.Name) && utf8.RuneCountInString(c.Name) <= 200 && !strings.ContainsAny(c.Name, "\r\n\x00")
}
func readFile(path string, private bool) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("wallet file paths must be absolute")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open wallet configuration or credential")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return nil, errors.New("invalid wallet configuration or credential file")
	}
	// Keep issuer keys out of group/world-readable files. Symlinks are rejected
	// so a privileged deployment cannot unexpectedly follow a changed target.
	lst, err := os.Lstat(path)
	if err != nil || lst.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("wallet files must not be symbolic links")
	}
	if private && st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("wallet configuration and private keys require mode 0600")
	}
	return io.ReadAll(io.LimitReader(f, 1<<20))
}
func decodeJSON(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return errors.New("invalid wallet configuration JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid trailing wallet configuration")
	}
	return nil
}
func NewFromFile(path string) (*Service, error) {
	s := &Service{}
	if path == "" {
		return s, nil
	}
	raw, err := readFile(path, true)
	if err != nil {
		return nil, err
	}
	s.files = append(s.files, path)
	var c Config
	if err = decodeJSON(raw, &c); err != nil {
		return nil, err
	}
	if c.Apple != nil {
		s.files = append(s.files, c.Apple.Certificate, c.Apple.PrivateKey, c.Apple.WWDRCertificates)
		s.apple, err = loadApple(*c.Apple)
		if err != nil {
			return nil, fmt.Errorf("Apple Wallet: %w", err)
		}
	}
	if c.Google != nil {
		s.files = append(s.files, c.Google.ServiceAccount)
		s.google, err = loadGoogle(*c.Google)
		if err != nil {
			return nil, fmt.Errorf("Google Wallet: %w", err)
		}
	}
	return s, nil
}
func certificates(path string) ([]*x509.Certificate, error) {
	raw, e := readFile(path, false)
	if e != nil {
		return nil, e
	}
	var certs []*x509.Certificate
	for len(bytes.TrimSpace(raw)) > 0 {
		b, rest := pem.Decode(raw)
		if b == nil || b.Type != "CERTIFICATE" {
			return nil, errors.New("expected PEM certificates")
		}
		c, e := x509.ParseCertificate(b.Bytes)
		if e != nil {
			return nil, errors.New("invalid certificate")
		}
		certs = append(certs, c)
		raw = rest
	}
	if len(certs) == 0 {
		return nil, errors.New("missing certificate")
	}
	return certs, nil
}
func parseKey(raw []byte) (*rsa.PrivateKey, error) {
	b, rest := pem.Decode(raw)
	if b == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("expected one unencrypted RSA PEM private key")
	}
	var key *rsa.PrivateKey
	switch b.Type {
	case "RSA PRIVATE KEY":
		k, e := x509.ParsePKCS1PrivateKey(b.Bytes)
		if e != nil {
			return nil, errors.New("invalid RSA private key")
		}
		key = k
	case "PRIVATE KEY":
		k, e := x509.ParsePKCS8PrivateKey(b.Bytes)
		if e != nil {
			return nil, errors.New("invalid private key")
		}
		key, _ = k.(*rsa.PrivateKey)
	default:
		return nil, errors.New("expected unencrypted RSA private key")
	}
	if key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, errors.New("RSA key must be valid and at least 2048 bits")
	}
	return key, nil
}
func currentCert(c *x509.Certificate) bool {
	now := time.Now()
	return !now.Before(c.NotBefore) && now.Before(c.NotAfter)
}
func loadApple(c AppleConfig) (*appleSigner, error) {
	if !strings.HasPrefix(c.PassTypeID, "pass.") || !identifierPattern.MatchString(c.PassTypeID) || c.TeamID == "" || c.Organization == "" {
		return nil, errors.New("pass type, team and organization are required")
	}
	certs, e := certificates(c.Certificate)
	if e != nil {
		return nil, e
	}
	if len(certs) != 1 {
		return nil, errors.New("one pass signing certificate is required")
	}
	cert := certs[0]
	raw, e := readFile(c.PrivateKey, true)
	if e != nil {
		return nil, e
	}
	key, e := parseKey(raw)
	if e != nil {
		return nil, e
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || pub.E != key.E || pub.N.Cmp(key.N) != 0 {
		return nil, errors.New("certificate and private key do not match")
	}
	matchingID := false
	for _, a := range cert.Subject.Names {
		if a.Type.String() == "0.9.2342.19200300.100.1.1" && a.Value == c.PassTypeID {
			matchingID = true
		}
	}
	matchingTeam := false
	for _, ou := range cert.Subject.OrganizationalUnit {
		if ou == c.TeamID {
			matchingTeam = true
		}
	}
	if !matchingID || !matchingTeam || !currentCert(cert) || cert.IsCA || cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return nil, errors.New("pass certificate identity, usage or validity is incorrect")
	}
	chain, e := certificates(c.WWDRCertificates)
	if e != nil {
		return nil, e
	}
	child := cert
	for _, parent := range chain {
		if !currentCert(parent) || !parent.IsCA || child.CheckSignatureFrom(parent) != nil {
			return nil, errors.New("invalid or expired WWDR certificate chain")
		}
		child = parent
	}
	return &appleSigner{c, cert, key, chain}, nil
}
func loadGoogle(c GoogleConfig) (*googleSigner, error) {
	if c.IssuerID == "" || !regexp.MustCompile(`^[0-9]+$`).MatchString(c.IssuerID) || !strings.HasPrefix(c.ClassID, c.IssuerID+".") || len(c.ClassID) <= len(c.IssuerID)+1 || !identifierPattern.MatchString(c.ClassID) || c.Organization == "" || len(c.Origins) == 0 {
		return nil, errors.New("issuer, class, organization and HTTPS origins are required")
	}
	for _, origin := range c.Origins {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("origins must be HTTPS origins without a path")
		}
	}
	raw, e := readFile(c.ServiceAccount, true)
	if e != nil {
		return nil, e
	}
	var a struct {
		Type       string `json:"type"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
		KeyID      string `json:"private_key_id"`
	}
	// Google exports contain additional metadata; retain only signing credentials.
	if json.Unmarshal(raw, &a) != nil || a.Type != "service_account" || !strings.HasSuffix(a.Email, ".gserviceaccount.com") {
		return nil, errors.New("invalid Google service account")
	}
	address, e := mail.ParseAddress(a.Email)
	if e != nil || address.Address != a.Email {
		return nil, errors.New("invalid Google service account email")
	}
	key, e := parseKey([]byte(a.PrivateKey))
	if e != nil {
		return nil, e
	}
	return &googleSigner{c, a.Email, a.KeyID, key}, nil
}
func (s *Service) Apple(c Card) ([]byte, error) {
	if s == nil || s.apple == nil {
		return nil, ErrUnavailable
	}
	if !validCard(c) {
		return nil, ErrInvalid
	}
	a := s.apple
	if !currentCert(a.certificate) {
		return nil, errors.New("Apple pass signing certificate expired")
	}
	for _, cert := range a.chain {
		if !currentCert(cert) {
			return nil, errors.New("Apple WWDR certificate expired")
		}
	}
	pass := map[string]any{"formatVersion": 1, "passTypeIdentifier": a.config.PassTypeID, "serialNumber": c.Code, "teamIdentifier": a.config.TeamID, "organizationName": a.config.Organization, "description": "Library member card", "logoText": a.config.Organization, "foregroundColor": "rgb(255, 255, 255)", "backgroundColor": "rgb(40, 85, 66)", "labelColor": "rgb(255, 255, 255)", "generic": map[string]any{"primaryFields": []any{map[string]string{"key": "member", "label": "MEMBER", "value": c.Name}}}, "barcodes": []any{map[string]string{"format": "PKBarcodeFormatQR", "message": c.Code, "messageEncoding": "iso-8859-1", "altText": c.Code}}}
	raw, e := json.Marshal(pass)
	if e != nil {
		return nil, e
	}
	files := map[string][]byte{"pass.json": raw, "icon.png": icon(29), "icon@2x.png": icon(58), "icon@3x.png": icon(87)}
	manifest := map[string]string{}
	for name, data := range files {
		digest := sha1.Sum(data)
		manifest[name] = hex.EncodeToString(digest[:])
	}
	raw, e = json.Marshal(manifest)
	if e != nil {
		return nil, e
	}
	files["manifest.json"] = raw
	signed, e := pkcs7.NewSignedData(raw)
	if e != nil {
		return nil, e
	}
	signed.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	if e = signed.AddSignerChain(a.certificate, a.key, a.chain, pkcs7.SignerInfoConfig{}); e != nil {
		return nil, e
	}
	signed.Detach()
	files["signature"], e = signed.Finish()
	if e != nil {
		return nil, e
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, name := range []string{"pass.json", "icon.png", "icon@2x.png", "icon@3x.png", "manifest.json", "signature"} {
		f, e := z.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = f.Write(files[name]); e != nil {
			return nil, e
		}
	}
	if e = z.Close(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
func icon(size int) []byte {
	im := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := color.RGBA{40, 85, 66, 255}
			if x > size/4 && x < 3*size/4 && y > size/5 && y < 4*size/5 {
				v = color.RGBA{255, 255, 255, 255}
			}
			im.SetRGBA(x, y, v)
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, im)
	return out.Bytes()
}
func localized(text string) map[string]any {
	return map[string]any{"defaultValue": map[string]string{"language": "en-GB", "value": text}}
}
func (s *Service) Google(c Card) (string, error) {
	if s == nil || s.google == nil {
		return "", ErrUnavailable
	}
	if !validCard(c) {
		return "", ErrInvalid
	}
	g := s.google
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if g.keyID != "" {
		header["kid"] = g.keyID
	}
	claims := map[string]any{"iss": g.email, "aud": "google", "typ": "savetowallet", "iat": time.Now().Unix(), "origins": g.config.Origins, "payload": map[string]any{"genericObjects": []any{map[string]any{"id": g.config.IssuerID + "." + c.Code, "classId": g.config.ClassID, "state": "ACTIVE", "cardTitle": localized(g.config.Organization), "header": localized("Library card"), "subheader": localized(c.Name), "hexBackgroundColor": "#285542", "barcode": map[string]string{"type": "QR_CODE", "value": c.Code, "alternateText": c.Code}}}}}
	encode := func(v any) string { raw, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(raw) }
	unsigned := encode(header) + "." + encode(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, e := rsa.SignPKCS1v15(rand.Reader, g.key, crypto.SHA256, digest[:])
	if e != nil {
		return "", e
	}
	return "https://pay.google.com/gp/v/save/" + unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// ValidateLocations keeps issuer material out of served files and library
// snapshots. Resolve both lexical and physical locations, including a symlink
// in a parent directory, and support data directories not created yet.
func (s *Service) ValidateLocations(forbidden ...string) error {
	if s == nil {
		return nil
	}
	for _, file := range s.files {
		lexical, e := filepath.Abs(file)
		if e != nil {
			return errors.New("invalid wallet file location")
		}
		physical, e := filepath.EvalSymlinks(lexical)
		if e != nil {
			return errors.New("cannot resolve wallet file location")
		}
		for _, root := range forbidden {
			if root == "" {
				continue
			}
			absolute, e := filepath.Abs(root)
			if e != nil {
				return errors.New("invalid protected directory")
			}
			resolved, e := resolveDirectory(absolute)
			if e != nil {
				return errors.New("cannot resolve protected directory")
			}
			for _, pair := range [][2]string{{absolute, lexical}, {resolved, physical}} {
				relative, e := filepath.Rel(pair[0], pair[1])
				if e == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					return errors.New("wallet configuration and credentials must be outside web and library data directories")
				}
			}
		}
	}
	return nil
}
func resolveDirectory(path string) (string, error) {
	resolved, e := filepath.EvalSymlinks(path)
	if e == nil {
		return resolved, nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", e
	}
	resolved, e = resolveDirectory(parent)
	if e != nil {
		return "", e
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}
