package releasecontroller

import (
	"testing"
)

func TestParseRpmChangelogOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: map[string]string{},
		},
		{
			name: "single package",
			input: `===RPM_CL_SEP:vim-minimal===
* Thu Mar 19 2026 Bryan Mason <bmason@redhat.com> - 2:8.2.2637-22.2
- CVE-2026-25749 vim: Heap Overflow in Vim
- CVE-2026-28417 vim: Arbitrary code execution`,
			expected: map[string]string{
				"vim-minimal": "* Thu Mar 19 2026 Bryan Mason <bmason@redhat.com> - 2:8.2.2637-22.2\n- CVE-2026-25749 vim: Heap Overflow in Vim\n- CVE-2026-28417 vim: Arbitrary code execution",
			},
		},
		{
			name: "multiple packages",
			input: `===RPM_CL_SEP:bash===
* Mon Jun 01 2026 Ondrej Valousek <ovalouse@redhat.com> - 5.2.37-1
- Update to bash-5.2 patch 37

* Mon May 25 2026 Ondrej Valousek <ovalouse@redhat.com> - 5.2.36-1
- Update to bash-5.2 patch 36
===RPM_CL_SEP:glibc===
* Fri May 22 2026 Florian Weimer <fweimer@redhat.com> - 2.39-124
- Rebuild with updated compiler`,
			expected: map[string]string{
				"bash":  "* Mon Jun 01 2026 Ondrej Valousek <ovalouse@redhat.com> - 5.2.37-1\n- Update to bash-5.2 patch 37\n\n* Mon May 25 2026 Ondrej Valousek <ovalouse@redhat.com> - 5.2.36-1\n- Update to bash-5.2 patch 36",
				"glibc": "* Fri May 22 2026 Florian Weimer <fweimer@redhat.com> - 2.39-124\n- Rebuild with updated compiler",
			},
		},
		{
			name: "trailing newlines",
			input: `===RPM_CL_SEP:openssl===
* Tue Jun 10 2026 Dmitry Belyavskiy <dbelyavs@redhat.com> - 1:3.5.5-4
- Fix CVE-2026-1234

`,
			expected: map[string]string{
				"openssl": "* Tue Jun 10 2026 Dmitry Belyavskiy <dbelyavs@redhat.com> - 1:3.5.5-4\n- Fix CVE-2026-1234",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseRpmChangelogOutput(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d packages, got %d: %v", len(tt.expected), len(result), result)
			}
			for pkg, expectedCL := range tt.expected {
				if got, ok := result[pkg]; !ok {
					t.Errorf("missing package %q in result", pkg)
				} else if got != expectedCL {
					t.Errorf("package %q:\n  expected: %q\n  got:      %q", pkg, expectedCL, got)
				}
			}
		})
	}
}

func TestFilterChangelogBetweenVersions(t *testing.T) {
	tests := []struct {
		name       string
		changelog  string
		oldVersion string
		expected   string
	}{
		{
			name:       "empty changelog",
			changelog:  "",
			oldVersion: "1.0-1",
			expected:   "",
		},
		{
			name:       "empty old version",
			changelog:  "* Mon Jun 01 2026 Author <a@b.c> - 2.0-1\n- Change",
			oldVersion: "",
			expected:   "* Mon Jun 01 2026 Author <a@b.c> - 2.0-1\n- Change",
		},
		{
			name: "filters to entries newer than old version",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-2
- Fix bug 2

* Fri May 22 2026 Author <a@b.c> - 2.0-1
- New feature

* Mon May 01 2026 Author <a@b.c> - 1.9-1
- Old change`,
			oldVersion: "1.9-1",
			expected: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-2
- Fix bug 2

* Fri May 22 2026 Author <a@b.c> - 2.0-1
- New feature`,
		},
		{
			name: "handles epoch in old version",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 32:9.18.33-15.el10_2.2
- Fix CVE

* Fri May 15 2026 Author <a@b.c> - 32:9.18.33-15.el10_2.1
- Previous fix`,
			oldVersion: "32:9.18.33-15.el10_2.1",
			expected: `* Mon Jun 01 2026 Author <a@b.c> - 32:9.18.33-15.el10_2.2
- Fix CVE`,
		},
		{
			name: "handles epoch mismatch between changelog and version",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 9.18.33-15.el10_2.2
- Fix CVE

* Fri May 15 2026 Author <a@b.c> - 9.18.33-15.el10_2.1
- Previous fix`,
			oldVersion: "32:9.18.33-15.el10_2.1",
			expected: `* Mon Jun 01 2026 Author <a@b.c> - 9.18.33-15.el10_2.2
- Fix CVE`,
		},
		{
			name: "no matching old version returns full changelog",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-2
- Fix bug 2

* Fri May 22 2026 Author <a@b.c> - 2.0-1
- New feature`,
			oldVersion: "1.0-1",
			expected: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-2
- Fix bug 2

* Fri May 22 2026 Author <a@b.c> - 2.0-1
- New feature`,
		},
		{
			name: "single entry matching old version",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 1.0-1
- Initial release`,
			oldVersion: "1.0-1",
			expected:   "",
		},
		{
			name: "multiline changelog entries",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-1
- Fix CVE-2026-1234 foo: buffer overflow
  Resolves: rhbz#12345
- Fix CVE-2026-5678 foo: use after free
  Resolves: rhbz#67890

* Fri May 01 2026 Author <a@b.c> - 1.0-1
- Initial release`,
			oldVersion: "1.0-1",
			expected: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-1
- Fix CVE-2026-1234 foo: buffer overflow
  Resolves: rhbz#12345
- Fix CVE-2026-5678 foo: use after free
  Resolves: rhbz#67890`,
		},
		{
			name: "old version with dist tag matches header without dist tag",
			changelog: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-2
- New fix

* Fri May 15 2026 Author <a@b.c> - 2.0-1
- Previous`,
			oldVersion: "2.0-1.el10_2",
			expected: `* Mon Jun 01 2026 Author <a@b.c> - 2.0-2
- New fix`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterChangelogBetweenVersions(tt.changelog, tt.oldVersion)
			if result != tt.expected {
				t.Errorf("expected:\n%s\n\ngot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.0-1", "1.0-1"},
		{"0:1.0-1", "1.0-1"},
		{"2:1.0-1", "1.0-1"},
		{"32:9.18.33-15", "9.18.33-15"},
		{"9.18.33-15", "9.18.33-15"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeVersion(tt.input); got != tt.expected {
				t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
