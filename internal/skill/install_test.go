package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallStatusManagedStubLifecycle(t *testing.T) {
	target := testTarget(t)

	status := Status(target, "test")
	if status.State != "missing" || !status.Repairable || status.Installed {
		t.Fatalf("missing status = %#v", status)
	}

	status, err := Install(target, "test")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if status.State != "managed_stub" || !status.Managed || !status.Installed {
		t.Fatalf("installed status = %#v", status)
	}
	if data := readFile(t, target.SkillFilePath); !strings.Contains(data, "slacky skills get core") {
		t.Fatalf("stub should route to runtime guidance:\n%s", data)
	}

	if err := os.WriteFile(target.SkillFilePath, []byte("stale stub\n"), 0o600); err != nil {
		t.Fatalf("write stale stub: %v", err)
	}
	status = Status(target, "test")
	if status.State != "stale_managed_stub" || !status.Managed || !status.Repairable {
		t.Fatalf("stale status = %#v", status)
	}

	status, err = Install(target, "test")
	if err != nil {
		t.Fatalf("reinstall stale stub: %v", err)
	}
	if status.State != "managed_stub" {
		t.Fatalf("reinstalled status = %#v", status)
	}

	status, removed, err := Uninstall(target, "test")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !removed || status.State != "missing" {
		t.Fatalf("uninstall removed=%t status=%#v", removed, status)
	}

	status, removed, err = Uninstall(target, "test")
	if err != nil {
		t.Fatalf("uninstall missing: %v", err)
	}
	if removed || status.State != "missing" {
		t.Fatalf("missing uninstall removed=%t status=%#v", removed, status)
	}
}

func TestInstallRefusesForeignPaths(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, target Target)
		wantState string
	}{
		{
			name: "foreign file",
			setup: func(t *testing.T, target Target) {
				if err := os.MkdirAll(target.SkillDir, 0o700); err != nil {
					t.Fatalf("mkdir skill dir: %v", err)
				}
				if err := os.WriteFile(target.SkillPath, []byte("foreign file\n"), 0o600); err != nil {
					t.Fatalf("write foreign file: %v", err)
				}
			},
			wantState: "foreign_file",
		},
		{
			name: "foreign directory",
			setup: func(t *testing.T, target Target) {
				if err := os.MkdirAll(target.SkillPath, 0o700); err != nil {
					t.Fatalf("mkdir foreign dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(target.SkillPath, "SKILL.md"), []byte("foreign dir\n"), 0o600); err != nil {
					t.Fatalf("write foreign skill: %v", err)
				}
			},
			wantState: "foreign_directory",
		},
		{
			name: "foreign symlink",
			setup: func(t *testing.T, target Target) {
				if err := os.MkdirAll(target.SkillDir, 0o700); err != nil {
					t.Fatalf("mkdir skill dir: %v", err)
				}
				foreign := filepath.Join(t.TempDir(), "foreign")
				if err := os.MkdirAll(foreign, 0o700); err != nil {
					t.Fatalf("mkdir foreign target: %v", err)
				}
				if err := os.Symlink(foreign, target.SkillPath); err != nil {
					t.Fatalf("symlink foreign target: %v", err)
				}
			},
			wantState: "foreign_symlink",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := testTarget(t)
			test.setup(t, target)
			before := snapshotPath(t, target.SkillPath)

			status := Status(target, "test")
			if status.State != test.wantState || status.Repairable || status.Managed {
				t.Fatalf("foreign status = %#v", status)
			}
			if _, err := Install(target, "test"); err == nil {
				t.Fatalf("install should refuse %s", test.name)
			}
			if _, _, err := Uninstall(target, "test"); err == nil {
				t.Fatalf("uninstall should refuse %s", test.name)
			}
			after := snapshotPath(t, target.SkillPath)
			if before != after {
				t.Fatalf("foreign path changed\nbefore=%q\nafter=%q", before, after)
			}
		})
	}
}

func testTarget(t *testing.T) Target {
	t.Helper()
	target, err := ResolveTarget("", false, t.TempDir())
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	return target
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func snapshotPath(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		return "missing:" + err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("readlink %s: %v", path, err)
		}
		return "symlink:" + target
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatalf("readdir %s: %v", path, err)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return "dir:" + strings.Join(names, ",")
	}
	return "file:" + readFile(t, path)
}
