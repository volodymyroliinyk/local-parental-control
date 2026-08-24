package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type discoveredApplication struct {
	Source, Package, Executable string
}

type packageManager struct {
	name       string
	listArgs   []string
	filesArgs  func(string) []string
	fileColumn int
}

var nativePackageManagers = []packageManager{
	{name: "dpkg-query", listArgs: []string{"-W", "-f=${binary:Package}\t${binary:Summary}\n"}, filesArgs: func(pkg string) []string { return []string{"-L", pkg} }},
	{name: "rpm", listArgs: []string{"-qa", "--qf", "%{NAME}\t%{SUMMARY}\n"}, filesArgs: func(pkg string) []string { return []string{"-ql", pkg} }},
	{name: "pacman", listArgs: []string{"-Q"}, filesArgs: func(pkg string) []string { return []string{"-Ql", pkg} }, fileColumn: 1},
	{name: "apk", listArgs: []string{"info"}, filesArgs: func(pkg string) []string { return []string{"info", "-L", pkg} }},
}

func discoverApplications(keyword string) ([]discoveredApplication, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return nil, fmt.Errorf("keyword must not be empty")
	}
	results := make(map[string]discoveredApplication)
	add := func(result discoveredApplication) {
		results[result.Source+"\x00"+result.Package+"\x00"+result.Executable] = result
	}
	discoverPATH(keyword, add)
	for _, manager := range nativePackageManagers {
		discoverNativePackages(keyword, manager, add)
	}
	discoverSnap(keyword, add)
	applications := make([]discoveredApplication, 0, len(results))
	for _, result := range results {
		applications = append(applications, result)
	}
	sort.Slice(applications, func(i, j int) bool {
		if applications[i].Source != applications[j].Source {
			return applications[i].Source < applications[j].Source
		}
		if applications[i].Package != applications[j].Package {
			return applications[i].Package < applications[j].Package
		}
		return applications[i].Executable < applications[j].Executable
	})
	return applications, nil
}

func discoverPATH(keyword string, add func(discoveredApplication)) {
	seen := make(map[string]bool)
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" || seen[directory] {
			continue
		}
		seen[directory] = true
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.Contains(strings.ToLower(entry.Name()), keyword) {
				continue
			}
			if path, ok := nativeExecutable(filepath.Join(directory, entry.Name())); ok {
				add(discoveredApplication{Source: "path", Package: "-", Executable: path})
			}
		}
	}
}

func discoverNativePackages(keyword string, manager packageManager, add func(discoveredApplication)) {
	output, ok := commandOutput(manager.name, manager.listArgs...)
	if !ok {
		return
	}
	for _, line := range outputLines(output) {
		if !strings.Contains(strings.ToLower(line), keyword) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg := fields[0]
		files, ok := commandOutput(manager.name, manager.filesArgs(pkg)...)
		if !ok {
			continue
		}
		for _, fileLine := range outputLines(files) {
			fileFields := strings.Fields(fileLine)
			if len(fileFields) <= manager.fileColumn {
				continue
			}
			if path, valid := nativeExecutable(fileFields[manager.fileColumn]); valid {
				add(discoveredApplication{Source: strings.TrimSuffix(manager.name, "-query"), Package: pkg, Executable: path})
			}
		}
	}
}

func discoverSnap(keyword string, add func(discoveredApplication)) {
	packages := make(map[string]bool)
	entries, _ := os.ReadDir("/snap/bin")
	for _, entry := range entries {
		if !strings.Contains(strings.ToLower(entry.Name()), keyword) {
			continue
		}
		pkg := strings.SplitN(entry.Name(), ".", 2)[0]
		packages[pkg] = true
	}
	output, ok := commandOutput("snap", "list")
	if ok {
		for index, line := range outputLines(output) {
			if index == 0 || !strings.Contains(strings.ToLower(line), keyword) {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) > 0 {
				packages[fields[0]] = true
			}
		}
	}
	for pkg := range packages {
		discoverSnapPackage(filepath.Join("/snap", pkg, "current"), pkg, keyword, add)
	}
}

func discoverSnapPackage(root, pkg, keyword string, add func(discoveredApplication)) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(resolvedRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if !strings.Contains(name, keyword) && !strings.Contains(name, strings.ToLower(pkg)) {
			return nil
		}
		if executable, valid := nativeExecutable(path); valid {
			add(discoveredApplication{Source: "snap", Package: pkg, Executable: executable})
		}
		return nil
	})
}

func nativeExecutable(path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	if filepath.Clean(resolved) == "/usr/bin/snap" {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", false
	}
	defer f.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil || !bytes.Equal(header, []byte{0x7f, 'E', 'L', 'F'}) {
		return "", false
	}
	return filepath.Clean(resolved), true
}

func commandOutput(name string, args ...string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).Output()
	return string(output), err == nil
}

func outputLines(output string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func printDiscovered(applications []discoveredApplication) {
	if len(applications) == 0 {
		fmt.Println("No installed applications found.")
		return
	}
	for _, application := range applications {
		fmt.Printf("%-8s  %-28s  supported             %s\n", application.Source, application.Package, application.Executable)
	}
}
