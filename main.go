// slackmoji manages custom Slack emoji using the signed-in Google Chrome session on macOS.
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

var (
	xoxcPattern      = regexp.MustCompile(`xoxc-[A-Za-z0-9-]+`)
	emojiNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,78}$`)
	userIDPattern    = regexp.MustCompile(`^[UW][A-Z0-9]+$`)
)

type config struct {
	workspace string
	profile   string
	yes       bool
	page      int
	count     int
	json      bool
	details   bool
	uploader  []string
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		color.New(color.FgRed, color.Bold).Fprint(os.Stderr, "error: ")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var cfg config
	root := &cobra.Command{
		Use:   "slackmoji",
		Short: color.New(color.Faint).Sprint("Manage custom Slack emoji through your signed-in Chrome session."),
		Long: cliHelp(`
slackmoji reads Chrome's Safe Storage secret from your Keychain, decrypts only
the selected Slack workspace cookies in memory, and discovers Slack's browser
request token from local storage. It never writes or displays credentials.
		`),
		Example: cliExample(`
slackmoji add party-parrot ./party-parrot.gif --workspace cloudexchange-inc  # Upload an emoji
slackmoji list party parrot                                                    # Search emoji
slackmoji delete party-parrot --workspace cloudexchange-inc --yes             # Permanently delete
		`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("invalid command: %q", args[0])
			}
			return cmd.Help()
		},
	}
	root.PersistentFlags().StringVarP(&cfg.workspace, "workspace", "w", "", "Slack subdomain, e.g. cloudexchange-inc")
	root.PersistentFlags().StringVarP(&cfg.profile, "profile", "p", "", "Chrome profile, e.g. 'Profile 1'")
	root.AddCommand(newAddCommand(&cfg), newDeleteCommand(&cfg), newListCommand(&cfg))
	return root
}

func newAddCommand(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:     "add <emoji-name> <image>",
		Aliases: []string{"upload"},
		Short:   "upload a custom emoji",
		Args:    cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if !emojiNamePattern.MatchString(args[0]) {
				return errors.New("emoji name must be 1-79 lowercase letters, numbers, hyphens, or underscores")
			}
			if info, err := os.Stat(args[1]); err != nil || info.IsDir() {
				return fmt.Errorf("image not found: %s", args[1])
			}
			client, hostname, cookies, token, err := connect(*cfg)
			if err != nil {
				return err
			}
			if err := uploadEmoji(client, hostname, cookies, token, args[0], args[1]); err != nil {
				return err
			}
			color.New(color.FgGreen, color.Bold).Printf("Uploaded :%s:", args[0])
			fmt.Printf(" to %s\n", hostname)
			return nil
		},
	}
}

func newDeleteCommand(cfg *config) *cobra.Command {
	command := &cobra.Command{
		Use:   "delete <emoji-name>",
		Short: "permanently delete a custom emoji",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !cfg.yes {
				return errors.New("refusing to delete without --yes")
			}
			if !emojiNamePattern.MatchString(args[0]) {
				return errors.New("emoji name must be 1-79 lowercase letters, numbers, hyphens, or underscores")
			}
			client, hostname, cookies, token, err := connect(*cfg)
			if err != nil {
				return err
			}
			if err := deleteEmoji(client, hostname, cookies, token, args[0]); err != nil {
				return err
			}
			color.New(color.FgYellow, color.Bold).Printf("Deleted :%s:", args[0])
			fmt.Printf(" from %s\n", hostname)
			return nil
		},
	}
	command.Flags().BoolVar(&cfg.yes, "yes", false, "confirm permanent deletion")
	return command
}

func newListCommand(cfg *config) *cobra.Command {
	command := &cobra.Command{
		Use:   "list [search-term...]",
		Short: "list custom emoji, optionally filtered by search terms",
		Long: cliHelp(`
List custom emoji in the selected Slack workspace. Search terms are passed to
Slack as individual queries; omit them to list all custom emoji.
		`),
		Example: cliExample(`
slackmoji list                         # List all custom emoji
slackmoji list party parrot            # Search using two terms
slackmoji list --page 2 --count 50     # Request another page
slackmoji list --uploader "Peter Downs" # Filter by uploader name
slackmoji list --json shellder          # Print Slack's complete response
		`),
		RunE: func(_ *cobra.Command, args []string) error {
			if cfg.page < 1 || cfg.count < 1 || cfg.count > 100 {
				return errors.New("--page must be positive and --count must be between 1 and 100")
			}
			client, hostname, cookies, token, err := connect(*cfg)
			if err != nil {
				return err
			}
			uploaderIDs, uploaderNames := splitUploaderFilters(cfg.uploader)
			payload, err := listEmoji(client, hostname, cookies, token, args, uploaderIDs, cfg.page, cfg.count)
			if err != nil {
				return err
			}
			if len(uploaderNames) > 0 {
				payload = filterEmojiByUploaderName(payload, uploaderNames)
			}
			return printEmojiList(payload, cfg.json, cfg.details)
		},
	}
	command.Flags().IntVar(&cfg.page, "page", 1, "results page")
	command.Flags().IntVar(&cfg.count, "count", 100, "results per page (1-100)")
	command.Flags().BoolVar(&cfg.json, "json", false, "print Slack's complete JSON response")
	command.Flags().BoolVar(&cfg.details, "details", false, "include image URLs and Slack IDs")
	command.Flags().StringSliceVar(&cfg.uploader, "uploader", nil, "filter by uploader name or Slack user ID (repeatable)")
	return command
}

func connect(cfg config) (*http.Client, string, map[string]string, string, error) {
	root, err := chromeRoot()
	if err != nil {
		return nil, "", nil, "", err
	}
	key, err := chromeCookieKey()
	if err != nil {
		return nil, "", nil, "", err
	}
	if cfg.workspace == "" {
		cfg.workspace, err = chooseWorkspace(root, cfg.profile)
		if err != nil {
			return nil, "", nil, "", err
		}
	}
	hostname, err := normalizeWorkspace(cfg.workspace)
	if err != nil {
		return nil, "", nil, "", err
	}
	profile, cookies, err := chooseProfile(root, cfg.profile, hostname, key)
	if err != nil {
		return nil, "", nil, "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	token, err := selectToken(client, hostname, cookies, profile)
	if err != nil {
		return nil, "", nil, "", err
	}
	return client, hostname, cookies, token, nil
}

func cliHelp(text string) string {
	return color.New(color.Faint).Sprint("Docs: https://github.com/peterldowns/slackmoji") + "\n\nHelp:\n" + cliExample(text)
}

func cliExample(text string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "#", 2)
		if len(parts) == 2 {
			lines = append(lines, fmt.Sprintf("  %s%s", strings.TrimRight(parts[0], " "), color.New(color.Faint).Sprintf(" # %s", strings.TrimSpace(parts[1]))))
		} else {
			lines = append(lines, "  "+parts[0])
		}
	}
	return strings.Join(lines, "\n")
}

func chromeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", fmt.Errorf("Chrome data directory not found: %s", root)
	}
	return root, nil
}

func chromeCookieKey() ([]byte, error) {
	output, err := exec.Command("security", "find-generic-password", "-w", "-a", "Chrome", "-s", "Chrome Safe Storage").Output()
	if err != nil || len(bytes.TrimSpace(output)) == 0 {
		return nil, errors.New("could not read Chrome Safe Storage from the macOS Keychain")
	}
	return pbkdf2SHA1(bytes.TrimSpace(output), []byte("saltysalt"), 1003, 16), nil
}

func pbkdf2SHA1(password, salt []byte, iterations, length int) []byte {
	result := make([]byte, 0, length)
	for block := 1; len(result) < length; block++ {
		mac := hmac.New(sha1.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for round := 1; round < iterations; round++ {
			mac = hmac.New(sha1.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for i := range t {
				t[i] ^= u[i]
			}
		}
		result = append(result, t...)
	}
	return result[:length]
}

func chromeProfiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			if info, err := os.Stat(filepath.Join(root, entry.Name(), "Cookies")); err == nil && !info.IsDir() {
				profiles = append(profiles, entry.Name())
			}
		}
	}
	sort.Strings(profiles)
	if len(profiles) == 0 {
		return nil, errors.New("no Chrome profiles with a Cookies database were found")
	}
	return profiles, nil
}

func copyCookieDB(profilePath string) (func(), string, error) {
	temp, err := os.MkdirTemp("", "slackmoji-cookies-")
	if err != nil {
		return nil, "", err
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	source := filepath.Join(profilePath, "Cookies")
	destination := filepath.Join(temp, "Cookies")
	if err := copyFile(source, destination); err != nil {
		cleanup()
		return nil, "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(source + suffix); err == nil {
			if err := copyFile(source+suffix, destination+suffix); err != nil {
				cleanup()
				return nil, "", err
			}
		}
	}
	return cleanup, destination, nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func cookiesForWorkspace(profilePath, hostname string, key []byte) (map[string]string, error) {
	cleanup, dbPath, err := copyCookieDB(profilePath)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT host_key, name, encrypted_value, path, LENGTH(path) FROM cookies WHERE host_key LIKE '%slack.com%' ORDER BY LENGTH(path) DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cookies := map[string]string{}
	for rows.Next() {
		var host, name, path string
		var encrypted []byte
		var pathLength int
		if err := rows.Scan(&host, &name, &encrypted, &path, &pathLength); err != nil {
			return nil, err
		}
		if matchesWorkspace(host, hostname) && cookies[name] == "" {
			value, err := decryptChromeCookie(encrypted, key)
			if err != nil {
				return nil, err
			}
			cookies[name] = value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if cookies["d"] == "" {
		return nil, fmt.Errorf("no signed-in Slack session for %s", hostname)
	}
	return cookies, nil
}

func decryptChromeCookie(encrypted, key []byte) (string, error) {
	if len(encrypted) == 0 {
		return "", nil
	}
	if !bytes.HasPrefix(encrypted, []byte("v10")) {
		return string(encrypted), nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	if len(encrypted[3:]) == 0 || len(encrypted[3:])%aes.BlockSize != 0 {
		return "", errors.New("invalid Chrome cookie ciphertext")
	}
	plain := make([]byte, len(encrypted)-3)
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte(" "), aes.BlockSize)).CryptBlocks(plain, encrypted[3:])
	if len(plain) == 0 || int(plain[len(plain)-1]) > len(plain) {
		return "", errors.New("invalid Chrome cookie padding")
	}
	padLength := int(plain[len(plain)-1])
	for _, b := range plain[len(plain)-padLength:] {
		if int(b) != padLength {
			return "", errors.New("invalid Chrome cookie padding")
		}
	}
	plain = plain[:len(plain)-padLength]
	if utf8ish(plain) {
		return string(plain), nil
	}
	if len(plain) >= 32 && utf8ish(plain[32:]) {
		return string(plain[32:]), nil
	}
	return "", errors.New("failed to decode a Chrome cookie value")
}

func utf8ish(value []byte) bool {
	return bytes.ToValidUTF8(value, []byte("?")) != nil && strings.IndexRune(string(value), '\uFFFD') == -1
}

func matchesWorkspace(cookieHost, hostname string) bool {
	domain := strings.TrimPrefix(cookieHost, ".")
	return hostname == domain || strings.HasSuffix(hostname, "."+domain)
}

func normalizeWorkspace(workspace string) (string, error) {
	hostname := strings.TrimSuffix(strings.TrimPrefix(workspace, "https://"), "/")
	if !strings.Contains(hostname, ".") {
		hostname += ".slack.com"
	}
	if !strings.HasSuffix(hostname, ".slack.com") {
		return "", errors.New("workspace must be a Slack subdomain, such as cloudexchange-inc")
	}
	return hostname, nil
}

func chooseWorkspace(root, requestedProfile string) (string, error) {
	profiles, err := chromeProfiles(root)
	if err != nil {
		return "", err
	}
	if requestedProfile != "" {
		profiles = []string{requestedProfile}
	}
	workspaces := map[string]bool{}
	for _, profile := range profiles {
		for _, host := range slackCookieHosts(filepath.Join(root, profile)) {
			workspaces[host] = true
		}
	}
	if len(workspaces) == 0 {
		return "", errors.New("no Slack workspaces were found in Chrome")
	}
	options := make([]string, 0, len(workspaces))
	for host := range workspaces {
		options = append(options, host)
	}
	sort.Strings(options)
	if len(options) == 1 {
		return options[0], nil
	}
	fmt.Fprintln(os.Stderr, "Select a Slack workspace:")
	for i, workspace := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, workspace)
	}
	fmt.Fprint(os.Stderr, "> ")
	var choice string
	if _, err := fmt.Fscanln(os.Stdin, &choice); err != nil {
		return "", errors.New("could not read workspace selection")
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(options) {
		return "", errors.New("invalid workspace selection")
	}
	return options[n-1], nil
}

func slackCookieHosts(profilePath string) []string {
	cleanup, dbPath, err := copyCookieDB(profilePath)
	if err != nil {
		return nil
	}
	defer cleanup()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	// Slack's authenticated `d` cookie is commonly scoped to .slack.com, so it
	// does not identify the workspace by itself. Other workspace cookies do.
	rows, err := db.Query(`SELECT DISTINCT host_key FROM cookies WHERE host_key LIKE '%.slack.com'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var hosts []string
	for rows.Next() {
		var host string
		if rows.Scan(&host) == nil {
			host = strings.TrimPrefix(host, ".")
			if host != "slack.com" {
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
}

func chooseProfile(root, requested, hostname string, key []byte) (string, map[string]string, error) {
	profiles, err := chromeProfiles(root)
	if err != nil {
		return "", nil, err
	}
	if requested != "" {
		found := false
		for _, profile := range profiles {
			if profile == requested {
				found = true
			}
		}
		if !found {
			return "", nil, fmt.Errorf("unknown Chrome profile %q", requested)
		}
		cookies, err := cookiesForWorkspace(filepath.Join(root, requested), hostname, key)
		return filepath.Join(root, requested), cookies, err
	}
	for _, profile := range profiles {
		path := filepath.Join(root, profile)
		cookies, err := cookiesForWorkspace(path, hostname, key)
		if err == nil {
			return path, cookies, nil
		}
	}
	return "", nil, fmt.Errorf("no signed-in Slack session for %s was found in Chrome", hostname)
}

func findTokens(profilePath string) ([]string, error) {
	files, err := os.ReadDir(filepath.Join(profilePath, "Local Storage", "leveldb"))
	if err != nil {
		return nil, errors.New("Slack local storage was not found in the selected Chrome profile")
	}
	seen := map[string]bool{}
	for _, file := range files {
		if file.IsDir() || (!strings.HasSuffix(file.Name(), ".ldb") && !strings.HasSuffix(file.Name(), ".log")) {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(profilePath, "Local Storage", "leveldb", file.Name()))
		if err != nil {
			continue
		}
		for _, token := range xoxcPattern.FindAllString(string(contents), -1) {
			seen[token] = true
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("could not find Slack's browser request token; open Slack in Chrome and retry")
	}
	tokens := make([]string, 0, len(seen))
	for token := range seen {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens, nil
}

func requestHeaders(hostname string, cookies map[string]string) http.Header {
	pairs := make([]string, 0, len(cookies))
	for name, value := range cookies {
		pairs = append(pairs, name+"="+value)
	}
	sort.Strings(pairs)
	headers := make(http.Header)
	headers.Set("Accept", "*/*")
	headers.Set("Origin", "https://"+hostname)
	headers.Set("Referer", "https://"+hostname+"/customize/emoji")
	headers.Set("User-Agent", userAgent)
	headers.Set("Cookie", strings.Join(pairs, "; "))
	return headers
}

func postForm(client *http.Client, hostname string, cookies map[string]string, endpoint string, fields map[string]string, imagePath string) (map[string]any, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if imagePath != "" {
		file, err := os.Open(imagePath)
		if err != nil {
			return nil, fmt.Errorf("image not found: %s", imagePath)
		}
		defer file.Close()
		part, err := writer.CreateFormFile("image", filepath.Base(imagePath))
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, "https://"+hostname+"/api/"+endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header = requestHeaders(hostname, cookies)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, errors.New("Slack rejected the Chrome session; refresh Slack in Chrome and retry")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("Slack returned HTTP %d", response.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, errors.New("Slack returned a non-JSON response")
	}
	return payload, nil
}

func selectToken(client *http.Client, hostname string, cookies map[string]string, profilePath string) (string, error) {
	tokens, err := findTokens(profilePath)
	if err != nil {
		return "", err
	}
	for _, token := range tokens {
		payload, err := postForm(client, hostname, cookies, "emoji.getInfo", map[string]string{"token": token, "name": "__slackmoji_probe__", "_x_mode": "online"}, "")
		if err != nil {
			continue
		}
		errorCode, _ := payload["error"].(string)
		if errorCode != "invalid_auth" && errorCode != "not_authed" && errorCode != "token_revoked" {
			return token, nil
		}
	}
	return "", errors.New("Chrome has Slack request tokens, but none match this workspace session; refresh Slack in Chrome and retry")
}

func uploadEmoji(client *http.Client, hostname string, cookies map[string]string, token, name, image string) error {
	payload, err := postForm(client, hostname, cookies, "emoji.add", map[string]string{"token": token, "name": name, "mode": "data", "search_args": "{}", "_x_reason": "add-custom-emoji-dialog-content", "_x_mode": "online"}, image)
	if err != nil {
		return err
	}
	return requireOK(payload, "upload")
}

func deleteEmoji(client *http.Client, hostname string, cookies map[string]string, token, name string) error {
	payload, err := postForm(client, hostname, cookies, "emoji.remove", map[string]string{"token": token, "name": name, "_x_reason": "customize-emoji-remove", "_x_mode": "online"}, "")
	if err != nil {
		return err
	}
	return requireOK(payload, "delete")
}

func listEmoji(client *http.Client, hostname string, cookies map[string]string, token string, queries, userIDs []string, page, count int) (map[string]any, error) {
	queryJSON, _ := json.Marshal(queries)
	userIDsJSON, _ := json.Marshal(userIDs)
	payload, err := postForm(client, hostname, cookies, "emoji.adminList", map[string]string{"token": token, "page": strconv.Itoa(page), "count": strconv.Itoa(count), "queries": string(queryJSON), "user_ids": string(userIDsJSON), "_x_reason": "customize-emoji-new-query", "_x_mode": "online"}, "")
	if err != nil {
		return nil, err
	}
	return payload, requireOK(payload, "list")
}

func splitUploaderFilters(filters []string) (userIDs, names []string) {
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if userIDPattern.MatchString(filter) {
			userIDs = append(userIDs, filter)
		} else {
			names = append(names, strings.ToLower(filter))
		}
	}
	return userIDs, names
}

func filterEmojiByUploaderName(payload map[string]any, filters []string) map[string]any {
	emojis, ok := payload["emoji"].([]any)
	if !ok {
		return payload
	}
	filtered := make([]any, 0, len(emojis))
	for _, item := range emojis {
		emoji, ok := item.(map[string]any)
		if !ok {
			continue
		}
		uploader := strings.ToLower(stringValue(emoji, "user_display_name") + " " + stringValue(emoji, "user_id"))
		matches := true
		for _, filter := range filters {
			if !strings.Contains(uploader, filter) {
				matches = false
				break
			}
		}
		if matches {
			filtered = append(filtered, item)
		}
	}
	copy := make(map[string]any, len(payload))
	for key, value := range payload {
		copy[key] = value
	}
	copy["emoji"] = filtered
	copy["custom_emoji_total_count"] = len(filtered)
	if paging, ok := payload["paging"].(map[string]any); ok {
		pagingCopy := make(map[string]any, len(paging))
		for key, value := range paging {
			pagingCopy[key] = value
		}
		pagingCopy["total"] = len(filtered)
		copy["paging"] = pagingCopy
	}
	return copy
}

func requireOK(payload map[string]any, action string) error {
	if ok, _ := payload["ok"].(bool); !ok {
		errorCode, _ := payload["error"].(string)
		if errorCode == "" {
			errorCode = "unknown error"
		}
		return fmt.Errorf("Slack could not %s emoji: %s", action, errorCode)
	}
	return nil
}

func printEmojiList(payload map[string]any, asJSON, details bool) error {
	if asJSON {
		encoded, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(encoded))
		return nil
	}
	emojis, ok := payload["emoji"].([]any)
	if !ok {
		return errors.New("Slack returned an unexpected emoji list format; retry with --json")
	}
	if len(emojis) == 0 {
		fmt.Fprintln(os.Stderr, color.New(color.Faint).Sprint("No matching emoji."))
		return nil
	}
	header := color.New(color.Faint, color.Bold)
	header.Printf("%-24s %-22s %-20s %s\n", "EMOJI", "UPLOADED BY", "CREATED", "DETAILS")
	for _, item := range emojis {
		emoji, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(emoji, "name")
		uploader := stringValue(emoji, "user_display_name")
		if uploader == "" {
			uploader = stringValue(emoji, "user_id")
		}
		created := createdAt(emoji)
		description := emojiDescription(emoji)
		color.New(color.FgCyan, color.Bold).Print(padRight(":"+name+":", 24))
		fmt.Printf(" %-22s %-20s %s\n", truncate(uploader, 22), created, description)
		if details {
			printEmojiDetails(emoji)
		}
	}
	if paging, ok := payload["paging"].(map[string]any); ok {
		if total, ok := paging["total"].(float64); ok {
			fmt.Fprintf(os.Stderr, "%.0f matching emoji\n", total)
		}
	}
	return nil
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func createdAt(emoji map[string]any) string {
	created, ok := emoji["created"].(float64)
	if !ok || created == 0 {
		return "unknown"
	}
	return time.Unix(int64(created), 0).Local().Format("2006-01-02 15:04")
}

func emojiDescription(emoji map[string]any) string {
	var parts []string
	if aliasFor := stringValue(emoji, "alias_for"); aliasFor != "" {
		parts = append(parts, "alias → :"+aliasFor+":")
	} else {
		parts = append(parts, "image")
	}
	if isTruthy(emoji["is_bad"]) {
		parts = append(parts, "bad")
	}
	if isTruthy(emoji["can_delete"]) {
		parts = append(parts, "deletable")
	}
	return strings.Join(parts, " · ")
}

func printEmojiDetails(emoji map[string]any) {
	metadata := []string{
		"user: " + stringValue(emoji, "user_id"),
		"team: " + stringValue(emoji, "team_id"),
		"url: " + stringValue(emoji, "url"),
	}
	if synonyms, ok := emoji["synonyms"].([]any); ok && len(synonyms) > 0 {
		var names []string
		for _, synonym := range synonyms {
			if value, ok := synonym.(string); ok {
				names = append(names, ":"+value+":")
			}
		}
		if len(names) > 0 {
			metadata = append(metadata, "synonyms: "+strings.Join(names, ", "))
		}
	}
	for _, detail := range metadata {
		if strings.TrimSpace(strings.TrimPrefix(detail, "url:")) != "" && !strings.HasSuffix(detail, ": ") {
			fmt.Fprintln(os.Stdout, color.New(color.Faint).Sprint("  "+detail))
		}
	}
}

func isTruthy(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case float64:
		return value != 0
	default:
		return false
	}
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return truncate(value, width)
	}
	return value + strings.Repeat(" ", width-len(value))
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "…"
}
