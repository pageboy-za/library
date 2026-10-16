// library-account provides local administrator recovery without password arguments.
package main

import (
	"context"
	"flag"
	"fmt"
	"home-library/internal/auth"
	"home-library/internal/store"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	command := ""
	flags := []string{}
	valuePending := false
	for _, arg := range args {
		if valuePending {
			flags = append(flags, arg)
			valuePending = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
			valuePending = !strings.Contains(arg, "=") && (name == "data" || name == "username" || name == "display-name")
			continue
		}
		if command == "" && (arg == "bootstrap" || arg == "reset-password" || arg == "list") {
			command = arg
		} else {
			flags = append(flags, arg)
		}
	}
	f := flag.NewFlagSet("library-account", flag.ContinueOnError)
	f.SetOutput(output)
	data := f.String("data", "data", "Existing library data directory")
	username := f.String("username", "", "Staff username")
	display := f.String("display-name", "", "Display name for first administrator")
	if err := f.Parse(flags); err != nil {
		return err
	}
	if f.NArg() != 0 || command == "" {
		return fmt.Errorf("usage: library-account -data DIR [-username NAME] [-display-name NAME] bootstrap|reset-password|list; password is read from standard input")
	}
	database := filepath.Join(*data, "library.sqlite")
	if info, err := os.Stat(database); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("existing library.sqlite required in the selected data directory")
	}
	s, err := store.Open(database)
	if err != nil {
		return err
	}
	defer s.DB.Close()
	service := auth.New(s.DB)
	if command == "list" {
		users, err := service.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			fmt.Fprintf(output, "%s\t%s\tenabled=%t\t%s\n", u.Username, u.Role, u.Enabled, u.DisplayName)
		}
		return nil
	}
	if *username == "" {
		return fmt.Errorf("-username is required")
	}
	bytes, err := io.ReadAll(io.LimitReader(input, 1026))
	if err != nil {
		return err
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(bytes), "\n"), "\r")
	if strings.ContainsAny(password, "\r\n") || len(bytes) >= 1026 {
		return fmt.Errorf("provide one password line on standard input")
	}
	switch command {
	case "bootstrap":
		if *display == "" {
			*display = *username
		}
		_, err = service.Bootstrap(ctx, *username, *display, password)
	case "reset-password":
		users, e := service.ListUsers(ctx)
		if e != nil {
			return e
		}
		var id int64
		for _, u := range users {
			if strings.EqualFold(u.Username, strings.TrimSpace(*username)) {
				id = u.ID
				break
			}
		}
		if id == 0 {
			return fmt.Errorf("staff account not found")
		}
		err = service.ResetPassword(ctx, id, password)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "Staff account updated; existing sessions for a reset account have been revoked.")
	return nil
}
func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
