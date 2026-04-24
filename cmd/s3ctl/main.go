// Package main provides the s3ctl executable entrypoint.
package main

import (
	"os"

	"github.com/soakes/s3ctl/internal/s3ctl"
)

func main() {
	os.Exit(s3ctl.Main(os.Args[1:], os.Stdout, os.Stderr))
}
