package main

import "golang.org/x/term"

func readPassword(fd int) ([]byte, error) {
	if !term.IsTerminal(fd) {
		return nil, errNotTerminal
	}
	return term.ReadPassword(fd)
}

type notTerminal struct{}

func (notTerminal) Error() string { return "not a terminal" }

var errNotTerminal error = notTerminal{}
