package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/migration"
)

func main() {
	directory := flag.String("dir", "seeders", "seed SQL directory")
	file := flag.String("file", "", "one seed SQL file")
	flag.Parse()
	if *file != "" && *directory != "seeders" {
		fmt.Fprintln(os.Stderr, "file and dir cannot be used together")
		os.Exit(1)
	}
	if err := migration.Seed(*directory, *file); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("seeding completed")
}
