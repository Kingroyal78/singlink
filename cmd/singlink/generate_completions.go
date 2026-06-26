//go:build generate && generate_completions

package main

import "github.com/singlink/singlink/log"

func main() {
	err := generateCompletions()
	if err != nil {
		log.Fatal(err)
	}
}

func generateCompletions() error {
	for _, name := range []string{"sing-box", "singlink"} {
		if err := generateCompletion(name); err != nil {
			return err
		}
	}
	return nil
}

func generateCompletion(name string) error {
	mainCommand.Use = name
	err := mainCommand.GenBashCompletionFile("release/completions/" + name + ".bash")
	if err != nil {
		return err
	}
	err = mainCommand.GenFishCompletionFile("release/completions/"+name+".fish", true)
	if err != nil {
		return err
	}
	err = mainCommand.GenZshCompletionFile("release/completions/" + name + ".zsh")
	if err != nil {
		return err
	}
	return nil
}
