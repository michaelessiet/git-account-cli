package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1" // For ini file parsing
)

const (
	sshDir            = "~/.ssh"
	sshConfigPath     = "~/.ssh/config"
	globalGitConfig   = "~/.gitconfig"
	accountsConfigDir = "~/.git_accounts_config"
)

// Account represents a Git account configuration
type Account struct {
	Name       string
	Email      string
	Type       string // github, gitlab, bitbucket
	SSHKeyPath string
	HostAlias  string
	Hostname   string
	User       string
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error running command %s %s: %w\nOutput: %s", name, strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func generateSSHKey(name, email string) (string, error) {
	keyPath := expandPath(filepath.Join(sshDir, fmt.Sprintf("id_ed25519_%s", name)))

	if _, err := os.Stat(keyPath); err == nil {
		fmt.Printf("Warning: SSH key '%s' already exists. Skipping generation.\n", keyPath)
		return keyPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("error checking SSH key: %w", err)
	}

	fmt.Printf("Generating SSH key for %s (%s)...\n", name, email)
	// -N "" for no passphrase
	_, err := runCmd("ssh-keygen", "-t", "ed25519", "-C", email, "-f", keyPath, "-N", "")
	if err != nil {
		return "", fmt.Errorf("failed to generate SSH key: %w", err)
	}
	fmt.Printf("SSH key generated: %s\n", keyPath)
	return keyPath, nil
}

func addSSHConfigEntry(account *Account) error {
	sshConfigFilePath := expandPath(sshConfigPath)

	// Read existing config
	content, err := ioutil.ReadFile(sshConfigFilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read SSH config: %w", err)
	}

	// Remove existing entry for this host alias if it exists
	var newContent []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	skipLines := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == fmt.Sprintf("Host %s", account.HostAlias) {
			skipLines = true
		}
		if !skipLines {
			newContent = append(newContent, line)
		}
		if skipLines && strings.TrimSpace(line) == "" { // End of entry
			skipLines = false
		}
	}

	// Add new entry
	newContent = append(newContent,
		"", // Blank line for separation
		fmt.Sprintf("Host %s", account.HostAlias),
		fmt.Sprintf("  HostName %s", account.Hostname),
		fmt.Sprintf("  User %s", account.User),
		fmt.Sprintf("  IdentityFile %s", account.SSHKeyPath),
		"  IdentitiesOnly yes",
	)

	err = ioutil.WriteFile(sshConfigFilePath, []byte(strings.Join(newContent, "\n")), 0600)
	if err != nil {
		return fmt.Errorf("failed to write SSH config: %w", err)
	}
	fmt.Printf("SSH config updated for host: %s\n", account.HostAlias)
	return nil
}

func addKeyToSSHAgent(keyPath string) {
	_, err := runCmd("ssh-add", keyPath)
	if err != nil {
		fmt.Printf("Could not add %s to ssh-agent: %v. Please add it manually (`ssh-add %s`).\n", keyPath, err, keyPath)
	}
}

func saveAccountConfig(account *Account) error {
	configPath := expandPath(filepath.Join(accountsConfigDir, fmt.Sprintf("%s.ini", account.Name)))

	cfg := ini.Empty()
	sec, err := cfg.NewSection("account")
	if err != nil {
		return fmt.Errorf("failed to create ini section: %w", err)
	}

	sec.Key("name").SetValue(account.Name)
	sec.Key("email").SetValue(account.Email)
	sec.Key("type").SetValue(account.Type)
	sec.Key("ssh_key_path").SetValue(account.SSHKeyPath)
	sec.Key("host_alias").SetValue(account.HostAlias)
	sec.Key("hostname").SetValue(account.Hostname)
	sec.Key("user").SetValue(account.User)

	if err := cfg.SaveTo(configPath); err != nil {
		return fmt.Errorf("failed to save account config: %w", err)
	}
	fmt.Printf("Account configuration saved for '%s'.\n", account.Name)
	return nil
}

func loadAccountConfig(name string) (*Account, error) {
	configPath := expandPath(filepath.Join(accountsConfigDir, fmt.Sprintf("%s.ini", name)))

	cfg, err := ini.Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to load account config: %w", err)
	}

	acc := &Account{}
	sec := cfg.Section("account")
	acc.Name = sec.Key("name").String()
	acc.Email = sec.Key("email").String()
	acc.Type = sec.Key("type").String()
	acc.SSHKeyPath = sec.Key("ssh_key_path").String()
	acc.HostAlias = sec.Key("host_alias").String()
	acc.Hostname = sec.Key("hostname").String()
	acc.User = sec.Key("user").String()

	return acc, nil
}

func getAllAccountNames() ([]string, error) {
	dirPath := expandPath(accountsConfigDir)
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read accounts config directory: %w", err)
	}

	var names []string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".ini") {
			names = append(names, strings.TrimSuffix(f.Name(), ".ini"))
		}
	}
	return names, nil
}

func includeFilePath(name string) string {
	return expandPath(filepath.Join(accountsConfigDir, fmt.Sprintf("%s.gitconfig", name)))
}

func writeAccountIncludeFile(account *Account) error {
	path := includeFilePath(account.Name)
	content := fmt.Sprintf("[user]\n\tname = %s\n\temail = %s\n[core]\n\tsshCommand = ssh -i %s -o IdentitiesOnly=yes\n",
		account.Name, account.Email, account.SSHKeyPath)
	if err := ioutil.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write account include file: %w", err)
	}
	return nil
}

func absBindPath(p string) (string, error) {
	abs, err := filepath.Abs(expandPath(p))
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	if !strings.HasSuffix(abs, "/") {
		abs += "/"
	}
	return abs, nil
}

func includeIfKey(bindPath string) string {
	return fmt.Sprintf("includeIf.gitdir:%s.path", bindPath)
}

func getDirectoryBindings() (map[string]string, error) {
	output, err := runCmd("git", "config", "--global", "--get-regexp", `^includeif\.gitdir:.*\.path$`)
	if err != nil {
		// Exit code 1 means no matching keys, which is fine.
		return map[string]string{}, nil
	}
	bindings := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		const prefix = "includeif.gitdir:"
		const suffix = ".path"
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		boundPath := key[len(prefix) : len(key)-len(suffix)]
		name := strings.TrimSuffix(filepath.Base(value), ".gitconfig")
		bindings[boundPath] = name
	}
	return bindings, nil
}

func useAccountForDirectory(name, path string) {
	account, err := loadAccountConfig(name)
	if err != nil {
		fmt.Printf("Error loading account config: %v\n", err)
		return
	}
	if account == nil {
		fmt.Printf("Error: Account '%s' not found. Use 'git-account list' to see available accounts.\n", name)
		return
	}

	bindPath, err := absBindPath(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if info, err := os.Stat(strings.TrimSuffix(bindPath, "/")); err != nil {
		fmt.Printf("Error: directory %s does not exist.\n", bindPath)
		return
	} else if !info.IsDir() {
		fmt.Printf("Error: %s is not a directory.\n", bindPath)
		return
	}

	if err := writeAccountIncludeFile(account); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if _, err := runCmd("git", "config", "--global", "--replace-all", includeIfKey(bindPath), includeFilePath(account.Name)); err != nil {
		fmt.Printf("Error setting includeIf in global gitconfig: %v\n", err)
		return
	}

	fmt.Printf("Bound %s (and subdirectories) to account '%s'.\n", bindPath, name)
	fmt.Printf("Inside this tree, git will use:\n")
	fmt.Printf("  user.name  = %s\n", account.Name)
	fmt.Printf("  user.email = %s\n", account.Email)
	fmt.Printf("  ssh key    = %s\n", account.SSHKeyPath)
}

func unuseDirectory(path string) {
	bindPath, err := absBindPath(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	key := includeIfKey(bindPath)
	if _, err := runCmd("git", "config", "--global", "--get", key); err != nil {
		fmt.Printf("No binding found for %s.\n", bindPath)
		return
	}
	if _, err := runCmd("git", "config", "--global", "--unset-all", key); err != nil {
		fmt.Printf("Error removing binding: %v\n", err)
		return
	}
	fmt.Printf("Unbound %s.\n", bindPath)
}

func getPublicKeyContent(keyPath string) (string, error) {
	publicKeyPath := fmt.Sprintf("%s.pub", keyPath)
	content, err := ioutil.ReadFile(publicKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read public key %s: %w", publicKeyPath, err)
	}
	return strings.TrimSpace(string(content)), nil
}

func setupInstructions(account *Account) {
	fmt.Printf("\n--- Next Steps to Set Up Your %s Account (%s) ---\n", strings.ToUpper(account.Type[:1])+account.Type[1:], account.Name)

	publicKeyContent, err := getPublicKeyContent(account.SSHKeyPath)
	if err != nil {
		fmt.Printf("Error getting public key content: %v\n", err)
		fmt.Println("Please manually locate and copy the content of your public key:")
		fmt.Printf("  cat %s.pub\n", account.SSHKeyPath)
	} else {
		fmt.Println("\n1. Copy Your Public SSH Key:")
		fmt.Println("   Your public key content is below. Copy it to your clipboard:")
		fmt.Printf("   %s\n", publicKeyContent)
		fmt.Printf("   (You can also use: `cat %s.pub`)\n", account.SSHKeyPath)
	}

	fmt.Printf("\n2. Add the Public Key to Your %s Account:\n", strings.ToUpper(account.Type[:1])+account.Type[1:])
	switch account.Type {
	case "github":
		fmt.Println("   Go to GitHub:")
		fmt.Println("     - Log in to your GitHub account.")
		fmt.Println("     - Navigate to 'Settings' (your profile picture) -> 'SSH and GPG keys'.")
		fmt.Println("     - Click 'New SSH key' or 'Add SSH key'.")
		fmt.Println("     - Provide a descriptive 'Title' (e.g., 'My Laptop - personal_github').")
		fmt.Println("     - Paste the public key content you copied in the 'Key' field.")
		fmt.Println("     - Click 'Add SSH key'.")
		fmt.Println("   Reference: https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account")
	case "gitlab":
		fmt.Println("   Go to GitLab:")
		fmt.Println("     - Log in to your GitLab account.")
		fmt.Println("     - Click your profile avatar -> 'Preferences' -> 'SSH Keys'.")
		fmt.Println("     - In the 'Key' field, paste the public key content.")
		fmt.Println("     - Provide a 'Title' (e.g., 'My Laptop - client_gitlab').")
		fmt.Println("     - Optionally, set an 'Expiration date'.")
		fmt.Println("     - Click 'Add key'.")
		fmt.Println("   Reference: https://docs.gitlab.com/ee/user/ssh.html#add-an-ssh-key-to-your-gitlab-account")
	case "bitbucket":
		fmt.Println("   Go to Bitbucket:")
		fmt.Println("     - Log in to your Bitbucket account.")
		fmt.Println("     - Click your profile avatar -> 'Personal settings' -> 'SSH keys' (under Security).")
		fmt.Println("     - Click 'Add key'.")
		fmt.Println("     - Provide a 'Label' (e.g., 'My Laptop - work_bitbucket').")
		fmt.Println("     - Paste the public key content in the 'Key' field.")
		fmt.Println("     - Click 'Add key'.")
		fmt.Println("   Reference: https://support.atlassian.com/bitbucket-cloud/docs/set-up-an-ssh-key/")
	}

	fmt.Printf("\n3. Verify Your SSH Connection (Optional but Recommended):")
	fmt.Printf("\n   After adding the key to %s, test it in your terminal:\n", strings.ToUpper(account.Type[:1])+account.Type[1:])
	fmt.Printf("   ssh -T git@%s\n", account.HostAlias)
	fmt.Println("   You should receive a welcome message for your account.")

	fmt.Printf("\n4. You are ready to clone and use repositories:")
	fmt.Printf("\n   To clone: git clone git@%s:<owner>/<repo>.git\n", account.HostAlias)
	fmt.Printf("   To activate for commits: git-account switch %s\n", account.Name)
	fmt.Println("--------------------------------------------------")
}

func addAccount(name, email, serviceType string) {
	serviceType = strings.ToLower(serviceType)
	if serviceType != "github" && serviceType != "gitlab" && serviceType != "bitbucket" {
		fmt.Println("Error: service_type must be 'github', 'gitlab', or 'bitbucket'.")
		return
	}

	acc, err := loadAccountConfig(name)
	if err != nil {
		fmt.Printf("Error loading account config: %v\n", err)
		return
	}
	if acc != nil {
		fmt.Printf("Error: Account '%s' already exists.\n", name)
		return
	}

	sshKeyPath, err := generateSSHKey(name, email)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if sshKeyPath == "" {
		return // Key generation failed or skipped
	}

	hostname := ""
	if serviceType == "bitbucket" {
		hostname = "bitbucket.org"
	} else {
		hostname = fmt.Sprintf("%s.com", serviceType)
	}

	account := &Account{
		Name:       name,
		Email:      email,
		Type:       serviceType,
		SSHKeyPath: sshKeyPath,
		HostAlias:  fmt.Sprintf("%s-%s", strings.ReplaceAll(hostname, ".org", ".org"), name), // Handle bitbucket.org vs github.com
		Hostname:   hostname,
		User:       "git",
	}

	// Adjust host alias for bitbucket specifically to match previous logic (bitbucket.org-name)
	if serviceType == "bitbucket" {
		account.HostAlias = fmt.Sprintf("bitbucket.org-%s", name)
	} else {
		account.HostAlias = fmt.Sprintf("%s.com-%s", serviceType, name)
	}

	if err := saveAccountConfig(account); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if err := addSSHConfigEntry(account); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	addKeyToSSHAgent(sshKeyPath)

	fmt.Printf("\nAccount '%s' added successfully for %s.\n", name, serviceType)

	setupInstructions(account)
}

func removeAccount(name string) {
	account, err := loadAccountConfig(name)
	if err != nil {
		fmt.Printf("Error loading account config: %v\n", err)
		return
	}
	if account == nil {
		fmt.Printf("Error: Account '%s' not found.\n", name)
		return
	}

	sshConfigFilePath := expandPath(sshConfigPath)
	content, err := ioutil.ReadFile(sshConfigFilePath)
	if err != nil {
		fmt.Printf("Warning: Failed to read SSH config for cleanup: %v\n", err)
	} else {
		var newContent []string
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		skipLines := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == fmt.Sprintf("Host %s", account.HostAlias) {
				skipLines = true
			}
			if !skipLines {
				newContent = append(newContent, line)
			}
			if skipLines && strings.TrimSpace(line) == "" { // End of entry
				skipLines = false
			}
		}
		err = ioutil.WriteFile(sshConfigFilePath, []byte(strings.Join(newContent, "\n")), 0600)
		if err != nil {
			fmt.Printf("Warning: Failed to update SSH config for cleanup: %v\n", err)
		} else {
			fmt.Printf("Removed SSH config entry for %s\n", account.HostAlias)
		}
	}

	// Remove SSH key files
	sshKeyPath := account.SSHKeyPath
	if _, err := os.Stat(sshKeyPath); err == nil {
		os.Remove(sshKeyPath)
		fmt.Printf("Removed private key: %s\n", sshKeyPath)
	}
	if _, err := os.Stat(fmt.Sprintf("%s.pub", sshKeyPath)); err == nil {
		os.Remove(fmt.Sprintf("%s.pub", sshKeyPath))
		fmt.Printf("Removed public key: %s.pub\n", sshKeyPath)
	}

	// Remove account config file
	configFilePath := expandPath(filepath.Join(accountsConfigDir, fmt.Sprintf("%s.ini", name)))
	if _, err := os.Stat(configFilePath); err == nil {
		os.Remove(configFilePath)
		fmt.Printf("Removed account configuration for '%s'.\n", name)
	}

	// Remove per-account gitconfig include file
	incPath := includeFilePath(name)
	if _, err := os.Stat(incPath); err == nil {
		os.Remove(incPath)
		fmt.Printf("Removed gitconfig include: %s\n", incPath)
	}

	// Remove any directory bindings that pointed at this account
	bindings, _ := getDirectoryBindings()
	for boundPath, boundName := range bindings {
		if boundName == name {
			if _, err := runCmd("git", "config", "--global", "--unset-all", includeIfKey(boundPath)); err == nil {
				fmt.Printf("Removed directory binding: %s\n", boundPath)
			}
		}
	}

	fmt.Printf("Account '%s' removed successfully.\n", name)
}

func switchAccount(name string) {
	account, err := loadAccountConfig(name)
	if err != nil {
		fmt.Printf("Error loading account config: %v\n", err)
		return
	}
	if account == nil {
		fmt.Printf("Error: Account '%s' not found. Use 'git-account list' to see available accounts.\n", name)
		return
	}

	if _, err := runCmd("git", "config", "--global", "user.name", account.Name); err != nil {
		fmt.Printf("Error setting global git user.name: %v\n", err)
		return
	}
	fmt.Printf("Set global git user.name to: %s\n", account.Name)

	if _, err := runCmd("git", "config", "--global", "user.email", account.Email); err != nil {
		fmt.Printf("Error setting global git user.email: %v\n", err)
		return
	}
	fmt.Printf("Set global git user.email to: %s\n", account.Email)

	addKeyToSSHAgent(account.SSHKeyPath)

	fmt.Printf("\nSuccessfully switched to account '%s'.\n", name)
	fmt.Println("Your global git config (user.name and user.email) has been updated.")
	fmt.Println("SSH key for this account has been added to the SSH agent.")
	fmt.Printf("Remember to use 'git clone git@%s:<owner>/<repo>.git' for new repos.\n", account.HostAlias)
}

func renameAccount(oldName, newName string) {
	if oldName == newName {
		fmt.Println("Error: old and new names are the same.")
		return
	}
	if newName == "" || strings.ContainsAny(newName, "/\\ \t") {
		fmt.Println("Error: new name cannot be empty or contain whitespace or path separators.")
		return
	}

	oldAcc, err := loadAccountConfig(oldName)
	if err != nil {
		fmt.Printf("Error loading account config: %v\n", err)
		return
	}
	if oldAcc == nil {
		fmt.Printf("Error: Account '%s' not found.\n", oldName)
		return
	}

	if existing, err := loadAccountConfig(newName); err != nil {
		fmt.Printf("Error checking new name: %v\n", err)
		return
	} else if existing != nil {
		fmt.Printf("Error: Account '%s' already exists.\n", newName)
		return
	}

	newSSHKeyPath := expandPath(filepath.Join(sshDir, fmt.Sprintf("id_ed25519_%s", newName)))
	if _, err := os.Stat(newSSHKeyPath); err == nil {
		fmt.Printf("Error: SSH key '%s' already exists; refusing to overwrite.\n", newSSHKeyPath)
		return
	}

	newHostAlias := fmt.Sprintf("%s-%s", oldAcc.Hostname, newName)

	// Detect "currently switched to this account" so we can update global user.name afterward.
	wasSwitched := false
	if currentName, err := runCmd("git", "config", "--global", "user.name"); err == nil && currentName == oldAcc.Name {
		if currentEmail, err2 := runCmd("git", "config", "--global", "user.email"); err2 == nil && currentEmail == oldAcc.Email {
			wasSwitched = true
		}
	}

	oldKeyPath := oldAcc.SSHKeyPath
	if _, err := os.Stat(oldKeyPath); err == nil {
		if err := os.Rename(oldKeyPath, newSSHKeyPath); err != nil {
			fmt.Printf("Error renaming SSH private key: %v\n", err)
			return
		}
		fmt.Printf("Renamed SSH key: %s -> %s\n", oldKeyPath, newSSHKeyPath)
	}
	oldPub := oldKeyPath + ".pub"
	newPub := newSSHKeyPath + ".pub"
	if _, err := os.Stat(oldPub); err == nil {
		if err := os.Rename(oldPub, newPub); err != nil {
			fmt.Printf("Error renaming SSH public key: %v\n", err)
			return
		}
		fmt.Printf("Renamed SSH public key: %s -> %s\n", oldPub, newPub)
	}

	oldHostAlias := oldAcc.HostAlias
	sshConfigFilePath := expandPath(sshConfigPath)
	if content, err := ioutil.ReadFile(sshConfigFilePath); err == nil {
		var newContent []string
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		skipLines := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == fmt.Sprintf("Host %s", oldHostAlias) {
				skipLines = true
			}
			if !skipLines {
				newContent = append(newContent, line)
			}
			if skipLines && strings.TrimSpace(line) == "" {
				skipLines = false
			}
		}
		if err := ioutil.WriteFile(sshConfigFilePath, []byte(strings.Join(newContent, "\n")), 0600); err != nil {
			fmt.Printf("Warning: failed to rewrite SSH config: %v\n", err)
		}
	}

	newAcc := &Account{
		Name:       newName,
		Email:      oldAcc.Email,
		Type:       oldAcc.Type,
		SSHKeyPath: newSSHKeyPath,
		HostAlias:  newHostAlias,
		Hostname:   oldAcc.Hostname,
		User:       oldAcc.User,
	}
	if err := addSSHConfigEntry(newAcc); err != nil {
		fmt.Printf("Warning: failed to add new SSH config entry: %v\n", err)
	}

	if err := saveAccountConfig(newAcc); err != nil {
		fmt.Printf("Error saving new account config: %v\n", err)
		return
	}
	oldIniPath := expandPath(filepath.Join(accountsConfigDir, fmt.Sprintf("%s.ini", oldName)))
	if err := os.Remove(oldIniPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: failed to remove old account config: %v\n", err)
	}

	oldIncPath := includeFilePath(oldName)
	hadInclude := false
	if _, err := os.Stat(oldIncPath); err == nil {
		hadInclude = true
		os.Remove(oldIncPath)
	}
	if hadInclude {
		if err := writeAccountIncludeFile(newAcc); err != nil {
			fmt.Printf("Warning: failed to write new include file: %v\n", err)
		}
	}

	bindings, _ := getDirectoryBindings()
	for boundPath, boundName := range bindings {
		if boundName != oldName {
			continue
		}
		if !hadInclude {
			if err := writeAccountIncludeFile(newAcc); err != nil {
				fmt.Printf("Warning: failed to write include file for binding %s: %v\n", boundPath, err)
				continue
			}
			hadInclude = true
		}
		if _, err := runCmd("git", "config", "--global", "--replace-all", includeIfKey(boundPath), includeFilePath(newName)); err != nil {
			fmt.Printf("Warning: failed to update directory binding for %s: %v\n", boundPath, err)
		} else {
			fmt.Printf("Updated directory binding: %s -> %s\n", boundPath, newName)
		}
	}

	_, _ = runCmd("ssh-add", "-d", oldKeyPath)
	addKeyToSSHAgent(newSSHKeyPath)

	if wasSwitched {
		if _, err := runCmd("git", "config", "--global", "user.name", newName); err != nil {
			fmt.Printf("Warning: failed to update global user.name: %v\n", err)
		} else {
			fmt.Printf("Updated global git user.name to: %s\n", newName)
		}
	}

	fmt.Printf("\nAccount '%s' renamed to '%s'.\n", oldName, newName)
	fmt.Printf("Note: clone URLs using the old host alias 'git@%s:...' must be updated to 'git@%s:...'.\n", oldHostAlias, newHostAlias)
}

func listAccounts() {
	names, err := getAllAccountNames()
	if err != nil {
		fmt.Printf("Error listing accounts: %v\n", err)
		return
	}

	if len(names) == 0 {
		fmt.Println("No accounts configured yet.")
		fmt.Println("Use 'git-account add <name> <email> <type>' to add a new account.")
		return
	}

	fmt.Println("Configured Git Accounts:")
	for _, name := range names {
		account, err := loadAccountConfig(name)
		if err != nil {
			fmt.Printf("Error loading config for '%s': %v\n", name, err)
			continue
		}
		if account != nil {
			fmt.Printf("- Name: %s (%s)\n", account.Name, strings.ToUpper(account.Type[:1])+account.Type[1:])
			fmt.Printf("  Email: %s\n", account.Email)
			fmt.Printf("  SSH Key: %s\n", account.SSHKeyPath)
			fmt.Printf("  Host Alias: %s\n", account.HostAlias)
			fmt.Println("--------------------")
		}
	}

	bindings, err := getDirectoryBindings()
	if err == nil && len(bindings) > 0 {
		fmt.Println("\n--- Directory Bindings ---")
		for path, account := range bindings {
			fmt.Printf("- %s -> %s\n", path, account)
		}
	}

	fmt.Println("\n--- Current Global Git User ---")
	currentName, err := runCmd("git", "config", "--global", "user.name")
	if err != nil {
		currentName = "Not set or error"
	}
	currentEmail, err := runCmd("git", "config", "--global", "user.email")
	if err != nil {
		currentEmail = "Not set or error"
	}
	fmt.Printf("Name: %s\n", currentName)
	fmt.Printf("Email: %s\n", currentEmail)
}

func displayHelp() {
	fmt.Println("Usage: git-account <command> [args]")
	fmt.Println("\nCommands:")
	fmt.Println("  add <name> <email> <type>      Add a new Git account (type: github, gitlab, bitbucket)")
	fmt.Println("  remove <name>                  Remove an existing Git account")
	fmt.Println("  rename <old> <new>             Rename an existing account (keeps email and type)")
	fmt.Println("  switch <name>                  Switch to a configured Git account (sets global user.name/email)")
	fmt.Println("  use <name> [path]              Bind <path> (default: cwd) and its subdirectories to <name>")
	fmt.Println("  unuse [path]                   Remove the binding for <path> (default: cwd)")
	fmt.Println("  list                           List all configured Git accounts and directory bindings")
	fmt.Println("  help                           Display this help message")
	fmt.Println("\nExamples:")
	fmt.Println("  git-account add personal_github personal@example.com github")
	fmt.Println("  git-account add work_bitbucket work@example.com bitbucket")
	fmt.Println("  git-account switch personal_github")
	fmt.Println("  git-account use work_bitbucket ~/code/work")
	fmt.Println("  git-account unuse ~/code/work")
	fmt.Println("  git-account list")
	fmt.Println("\nNote: SSH keys are generated without a passphrase for convenience.")
	fmt.Println("      Remember to add the generated public keys to your Git service settings.")
	fmt.Println("      The 'add' command will guide you through this.")
}

func main() {
	// Create necessary directories
	os.MkdirAll(expandPath(sshDir), 0700)
	os.MkdirAll(expandPath(accountsConfigDir), 0700)

	// Ensure ~/.ssh/config exists with correct permissions
	sshConfigFilePath := expandPath(sshConfigPath)
	if _, err := os.Stat(sshConfigFilePath); os.IsNotExist(err) {
		if err := ioutil.WriteFile(sshConfigFilePath, []byte("# SSH configuration for git-account manager\n"), 0600); err != nil {
			fmt.Printf("Error creating SSH config file: %v\n", err)
			os.Exit(1)
		}
	} else if err != nil {
		fmt.Printf("Error checking SSH config file: %v\n", err)
		os.Exit(1)
	} else {
		// Ensure permissions are 0600
		if err := os.Chmod(sshConfigFilePath, 0600); err != nil {
			fmt.Printf("Error setting permissions for SSH config file: %v\n", err)
			os.Exit(1)
		}
	}

	if len(os.Args) < 2 {
		displayHelp()
		return
	}

	command := os.Args[1]

	switch command {
	case "add":
		if len(os.Args) == 5 {
			addAccount(os.Args[2], os.Args[3], os.Args[4])
		} else {
			fmt.Println("Usage: git-account add <name> <email> <type>")
		}
	case "remove":
		if len(os.Args) == 3 {
			removeAccount(os.Args[2])
		} else {
			fmt.Println("Usage: git-account remove <name>")
		}
	case "rename":
		if len(os.Args) == 4 {
			renameAccount(os.Args[2], os.Args[3])
		} else {
			fmt.Println("Usage: git-account rename <old> <new>")
		}
	case "switch":
		if len(os.Args) == 3 {
			switchAccount(os.Args[2])
		} else {
			fmt.Println("Usage: git-account switch <name>")
		}
	case "use":
		switch len(os.Args) {
		case 3:
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Printf("Error getting current directory: %v\n", err)
				return
			}
			useAccountForDirectory(os.Args[2], cwd)
		case 4:
			useAccountForDirectory(os.Args[2], os.Args[3])
		default:
			fmt.Println("Usage: git-account use <name> [path]")
		}
	case "unuse":
		switch len(os.Args) {
		case 2:
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Printf("Error getting current directory: %v\n", err)
				return
			}
			unuseDirectory(cwd)
		case 3:
			unuseDirectory(os.Args[2])
		default:
			fmt.Println("Usage: git-account unuse [path]")
		}
	case "list":
		listAccounts()
	case "help":
		displayHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		displayHelp()
	}
}
