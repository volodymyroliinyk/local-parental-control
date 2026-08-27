package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeExecutableAcceptsELFAndRejectsScript(t *testing.T) {
	directory := t.TempDir()
	elf := filepath.Join(directory, "firefox")
	if err := os.WriteFile(elf, append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 12)...), 0700); err != nil {
		t.Fatal(err)
	}
	if resolved, ok := nativeExecutable(elf); !ok || resolved != elf {
		t.Fatalf("nativeExecutable(%q) = %q, %v", elf, resolved, ok)
	}
	script := filepath.Join(directory, "launcher")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, ok := nativeExecutable(script); ok {
		t.Fatal("script was reported as a native executable")
	}
}

func TestOutputLinesTrimsAndOmitsEmptyLines(t *testing.T) {
	lines := outputLines(" one \n\n two\tvalue \n")
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "two\tvalue" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
}

func TestNativeExecutableRejectsSnapLauncher(t *testing.T) {
	if _, err := os.Stat("/usr/bin/snap"); err != nil {
		t.Skipf("snap launcher is not installed: %v", err)
	}
	if _, ok := nativeExecutable("/usr/bin/snap"); ok {
		t.Fatal("shared Snap launcher was reported as supported")
	}
}

func TestDiscoverSnapPackageReturnsELFInsteadOfLauncher(t *testing.T) {
	packageRoot := t.TempDir()
	revisionRoot := filepath.Join(packageRoot, "100")
	actual := filepath.Join(revisionRoot, "usr", "lib", "firefox", "firefox")
	if err := os.MkdirAll(filepath.Dir(actual), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actual, append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 12)...), 0755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(revisionRoot, "firefox.launcher")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(packageRoot, "current")
	if err := os.Symlink("100", current); err != nil {
		t.Fatal(err)
	}
	var results []discoveredApplication
	discoverSnapPackage(current, "firefox", "firefox", func(result discoveredApplication) { results = append(results, result) })
	stable := filepath.Join(current, "usr", "lib", "firefox", "firefox")
	if len(results) != 1 || results[0].Executable != stable {
		t.Fatalf("unexpected Snap discovery results: %#v", results)
	}
	if _, valid := nativeExecutable(results[0].Executable); !valid {
		t.Fatalf("discovered path is not configuration-ready: %q", results[0].Executable)
	}
}
