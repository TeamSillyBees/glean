package main

import (
	"strings"
	"testing"
)

func TestDestinationRejectsNonIsolatedDatabase(t *testing.T) {
	password := strings.Repeat("x", 32)
	valid := "postgres://postgres:test@127.0.0.1:5432/glean_browser_ci?sslmode=disable"
	if err := validateDestination("true", valid, password); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{
		"", "postgres://postgres:test@example.org/glean_browser_ci?sslmode=disable",
		"postgres://postgres:test@127.0.0.1/glean?sslmode=disable",
		"postgres://postgres:test@127.0.0.1/glean_browser_ci",
		"mysql://postgres:test@127.0.0.1/glean_browser_ci?sslmode=disable",
		valid + "&host=example.org",
		valid + "&dbname=glean",
		valid + "&sslmode=disable",
	} {
		if err := validateDestination("true", dsn, password); err == nil {
			t.Fatal("unsafe test destination was accepted")
		}
	}
	if validateDestination("false", valid, password) == nil || validateDestination("true", valid, "short") == nil {
		t.Fatal("non-CI or weak test password was accepted")
	}
}
