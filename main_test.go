package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidName(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"a", true},
		{"abc", true},
		{"A", true},
		{"a1", true},
		{"a-b", true},
		{"a_b", true},
		{"a.b", true},
		{"Agent01", true},
		{"", false},
		{"1abc", false},
		{"-abc", false},
		{"_abc", false},
		{".abc", false},
		{"a b", false},
		{"ab!", false},
		{"ab@cd", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := validName(tt.input, 32); got != tt.want {
				t.Errorf("validName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidNameLength(t *testing.T) {
	long63 := make([]byte, 32)
	for i := range long63 {
		long63[i] = 'a'
	}
	if !validName(string(long63), 32) {
		t.Error("validName should accept 32-char string")
	}

	long64 := make([]byte, 33)
	for i := range long64 {
		long64[i] = 'a'
	}
	if validName(string(long64), 32) {
		t.Error("validName should reject 33-char string")
	}
}

func TestResolveSSHKeys_StringOnly(t *testing.T) {
	cli := &CLI{
		SSHKeyString: []string{"ssh-rsa AAAA test@example.com"},
	}
	err := cli.resolveSSHKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cli.sshKeys) != 1 || cli.sshKeys[0] != "ssh-rsa AAAA test@example.com" {
		t.Errorf("sshKeys = %v, want [ssh-rsa AAAA test@example.com]", cli.sshKeys)
	}
}

func TestResolveSSHKeys_FileBased(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "id_rsa.pub")
	content := "ssh-ed2552 AAAA user@host\n"
	if err := os.WriteFile(keyFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cli := &CLI{
		SSHKey:       []string{keyFile},
		SSHKeyString: []string{},
	}
	err := cli.resolveSSHKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cli.sshKeys) != 1 || cli.sshKeys[0] != "ssh-ed2552 AAAA user@host" {
		t.Errorf("sshKeys = %v, want trimmed key content", cli.sshKeys)
	}
}

func TestResolveSSHKeys_FileNotFound(t *testing.T) {
	cli := &CLI{
		SSHKey:       []string{"/nonexistent/key.pub"},
		SSHKeyString: nil,
	}
	err := cli.resolveSSHKeys()
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestResolveSSHKeys_NoKeys(t *testing.T) {
	cli := &CLI{
		SSHKey:       nil,
		SSHKeyString: nil,
	}
	err := cli.resolveSSHKeys()
	if err == nil {
		t.Fatal("expected error when no keys provided")
	}
}

func TestResolveSSHKeys_Mixed(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.pub")
	if err := os.WriteFile(keyFile, []byte("ssh-rsa FILEKEY f@h\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cli := &CLI{
		SSHKey:       []string{keyFile},
		SSHKeyString: []string{"ssh-rsa STRINGKEY s@h"},
	}
	err := cli.resolveSSHKeys()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cli.sshKeys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(cli.sshKeys))
	}
	if cli.sshKeys[0] != "ssh-rsa FILEKEY f@h" {
		t.Errorf("first key = %q, want file key", cli.sshKeys[0])
	}
	if cli.sshKeys[1] != "ssh-rsa STRINGKEY s@h" {
		t.Errorf("second key = %q, want string key", cli.sshKeys[1])
	}
}

func TestValidate_UnsupportedAgent(t *testing.T) {
	dir := t.TempDir()
	cli := &CLI{
		Agent:        "nonexistent",
		Output:       filepath.Join(dir, "output"),
		User:         "agent",
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error for unsupported agent")
	}
}

func TestValidate_InvalidUser(t *testing.T) {
	cli := &CLI{
		Agent:        "codex",
		User:         "1badname",
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error for invalid username")
	}
}

func TestValidate_UserTooLong(t *testing.T) {
	longName := "a"
	for len(longName) <= 32 {
		longName += "b"
	}
	cli := &CLI{
		Agent:        "codex",
		User:         longName,
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error for too-long username")
	}
}

func TestValidate_MissingSecretsForCodex(t *testing.T) {
	dir := t.TempDir()
	cli := &CLI{
		Agent:        "codex",
		Output:       filepath.Join(dir, "output"),
		User:         "agent",
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error: codex requires secrets")
	}
}

func TestValidate_SecretsNotAcceptedForOpenCode(t *testing.T) {
	dir := t.TempDir()
	secFile := filepath.Join(dir, "secrets.json")
	if err := os.WriteFile(secFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	cli := &CLI{
		Agent:        "opencode",
		Output:       filepath.Join(dir, "output"),
		User:         "agent",
		SecretsFile:  secFile,
		ConfigFile:   cfgFile,
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error: opencode doesn't accept secrets")
	}
}

func TestValidate_MissingConfigForOpenCode(t *testing.T) {
	dir := t.TempDir()
	cli := &CLI{
		Agent:        "opencode",
		Output:       filepath.Join(dir, "output"),
		User:         "agent",
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error: opencode requires config")
	}
}

func TestValidate_MissingConfigForGoose(t *testing.T) {
	dir := t.TempDir()
	cli := &CLI{
		Agent:        "goose",
		Output:       filepath.Join(dir, "output"),
		User:         "agent",
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error: goose requires config")
	}
}

func TestValidate_OutputDirNotWritable(t *testing.T) {
	cli := &CLI{
		Agent:        "codex",
		Output:       "/nonexistent/dir/output.iso",
		User:         "agent",
		SSHKeyString: []string{"ssh-rsa AAAA t@t"},
	}
	err := cli.Validate()
	if err == nil {
		t.Fatal("expected error for non-writable output dir")
	}
}

func TestCLI_Accessors(t *testing.T) {
	cli := &CLI{
		sshKeys: []string{"key1"},
		config:  "cfgdata",
		secrets: "secdata",
	}
	if got := cli.SSHKeys(); len(got) != 1 || got[0] != "key1" {
		t.Errorf("SSHKeys() = %v, want [key1]", got)
	}
	if got := cli.Config(); got != "cfgdata" {
		t.Errorf("Config() = %q, want %q", got, "cfgdata")
	}
	if got := cli.Secrets(); got != "secdata" {
		t.Errorf("Secrets() = %q, want %q", got, "secdata")
	}
}
