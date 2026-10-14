package main

import (
	"context"
	"flag"
	"fmt"
	"home-library/internal/backup"
	"os"
	"os/signal"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	f := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	var err error
	switch os.Args[1] {
	case "create":
		source := f.String("data", "data", "Source library data directory")
		dest := f.String("out", "", "New backup directory (must not exist)")
		f.Parse(os.Args[2:])
		if *dest == "" {
			usage()
		}
		err = backup.Create(ctx, *source, *dest)
	case "verify":
		source := f.String("backup", "", "Backup directory")
		f.Parse(os.Args[2:])
		if *source == "" {
			usage()
		}
		err = backup.Verify(ctx, *source)
	case "restore":
		source := f.String("backup", "", "Backup directory")
		dest := f.String("out", "", "New data directory (must not exist)")
		f.Parse(os.Args[2:])
		if *source == "" || *dest == "" {
			usage()
		}
		err = backup.Restore(ctx, *source, *dest)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Backup operation completed and verified.")
}
func usage() {
	fmt.Fprintln(os.Stderr, "Usage: library-backup create -data data -out NEW_BACKUP_DIR\n       library-backup verify -backup BACKUP_DIR\n       library-backup restore -backup BACKUP_DIR -out NEW_DATA_DIR")
	os.Exit(2)
}
