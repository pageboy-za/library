package main

import (
	"bytes"
	"context"
	"home-library/internal/auth"
	"home-library/internal/store"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapResetAndListWithoutPasswordArguments(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	database, err := store.Open(filepath.Join(data, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = auth.EnsureSchema(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	password := "correct horse battery staple"
	// Values named after commands must not be mistaken for positional commands.
	if err = run(ctx, []string{"-data", data, "bootstrap", "-username", "list", "-display-name", "bootstrap"}, strings.NewReader(password+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), password) {
		t.Fatal("password leaked to output")
	}
	service := auth.New(database.DB)
	user, err := service.Authenticate(ctx, "list", password)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacement := "a different long password"
	if err = run(ctx, []string{"reset-password", "-data", data, "-username", "list"}, strings.NewReader(replacement+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Session(ctx, session.Token); err == nil {
		t.Fatal("reset retained old login")
	}
	if _, err = service.Authenticate(ctx, "list", replacement); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err = run(ctx, []string{"list", "-data", data}, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "list\tadministrator\tenabled=true\tbootstrap") {
		t.Fatal(output.String())
	}
	if err = run(ctx, []string{"bootstrap", "-data", data, "-username", "again"}, strings.NewReader(password), &output); err == nil {
		t.Fatal("second bootstrap allowed")
	}
}
func TestMissingDatabaseAndPasswordFlagsRejected(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"bootstrap", "-data", t.TempDir(), "-username", "admin"}, strings.NewReader("long enough password"), &output); err == nil {
		t.Fatal("created database at mistyped data path")
	}
	if err := run(context.Background(), []string{"bootstrap", "-password", "secret"}, strings.NewReader(""), &output); err == nil {
		t.Fatal("password flag accepted")
	}
}
