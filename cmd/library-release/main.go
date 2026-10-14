// library-release signs release manifests and verifies their publisher.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	action := flag.String("action", "verify", "generate, sign or verify")
	key := flag.String("key", "", "PEM key file")
	input := flag.String("manifest", "", "manifest file")
	signature := flag.String("signature", "", "detached signature file")
	public := flag.String("public", "", "public key output when generating")
	flag.Parse()
	fail := func(e error) {
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
	}
	if *action == "generate" {
		pub, priv, e := ed25519.GenerateKey(rand.Reader)
		fail(e)
		a, e := x509.MarshalPKCS8PrivateKey(priv)
		fail(e)
		b, e := x509.MarshalPKIXPublicKey(pub)
		fail(e)
		write := func(path string, data []byte, mode os.FileMode) {
			f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			fail(e)
			_, e = f.Write(data)
			fail(e)
			fail(f.Sync())
			fail(f.Close())
		}
		write(*key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: a}), 0600)
		write(*public, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: b}), 0644)
		return
	}
	data, e := os.ReadFile(*input)
	fail(e)
	if len(data) > 16<<20 {
		fail(fmt.Errorf("manifest too large"))
	}
	raw, e := os.ReadFile(*key)
	fail(e)
	block, _ := pem.Decode(raw)
	if block == nil {
		fail(fmt.Errorf("invalid PEM key"))
	}
	switch *action {
	case "sign":
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		fail(e)
		priv, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			fail(fmt.Errorf("Ed25519 key required"))
		}
		fail(os.WriteFile(*signature, []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))+"\n"), 0644))
	case "verify":
		parsed, e := x509.ParsePKIXPublicKey(block.Bytes)
		fail(e)
		pub, ok := parsed.(ed25519.PublicKey)
		if !ok {
			fail(fmt.Errorf("Ed25519 key required"))
		}
		sig, e := os.ReadFile(*signature)
		fail(e)
		decoded, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
		fail(e)
		if !ed25519.Verify(pub, data, decoded) {
			fail(fmt.Errorf("release signature rejected"))
		}
	default:
		fail(fmt.Errorf("unknown action"))
	}
}
