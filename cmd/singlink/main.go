//go:build !generate

package main

import "github.com/singlink/singlink/log"

func main() {
	if err := mainCommand.Execute(); err != nil {
		log.Fatal(err)
	}
}
